package blockchain

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"

	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const arbitrageExecutorABI = `[
	{
		"type":"function",
		"name":"execute",
		"stateMutability":"nonpayable",
		"inputs":[
			{
				"name":"plan",
				"type":"tuple",
				"components":[
					{
						"name":"loan",
						"type":"tuple",
						"components":[
							{"name":"protocol","type":"uint8"},
							{"name":"lender","type":"address"},
							{"name":"token","type":"address"},
							{"name":"amount","type":"uint256"},
							{"name":"borrowToken0","type":"bool"}
						]
					},
					{
						"name":"routers",
						"type":"tuple[]",
						"components":[
							{"name":"routerAddress","type":"address"},
							{"name":"value","type":"uint256"},
							{"name":"data","type":"bytes"},
							{"name":"fillToken","type":"address"},
							{"name":"fillOffset","type":"uint256"}
						]
					},
					{"name":"settleCurrencies","type":"address[]"},
					{"name":"profitToken","type":"address"},
					{"name":"minProfit","type":"uint256"},
					{"name":"deadline","type":"uint256"}
				]
			}
		],
		"outputs":[{"name":"profit","type":"uint256"}]
	}
]`

type ContractExecutorBroadcaster struct {
	parsedABI abi.ABI
}

func NewContractExecutorBroadcaster() (*ContractExecutorBroadcaster, error) {
	parsedABI, err := abi.JSON(strings.NewReader(arbitrageExecutorABI))
	if err != nil {
		return nil, fmt.Errorf("parse arbitrage executor abi: %w", err)
	}
	return &ContractExecutorBroadcaster{parsedABI: parsedABI}, nil
}

func (b *ContractExecutorBroadcaster) BroadcastExecution(
	ctx context.Context,
	req domaincontract.BroadcastRequest,
) (domaincontract.BroadcastResponse, error) {
	if b == nil {
		return domaincontract.BroadcastResponse{}, errors.New("contract executor broadcaster is nil")
	}

	privateKey, err := parsePrivateKey(req.PrivateKey)
	if err != nil {
		return domaincontract.BroadcastResponse{}, err
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)

	client, err := ethclient.DialContext(ctx, req.RPCURL)
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()

	chainID, err := client.ChainID(ctx)
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("chain id: %w", err)
	}

	data, err := b.parsedABI.Pack("execute", toExecutionPlanABI(req.Plan))
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("pack execute calldata: %w", err)
	}

	nonce, err := resolveNonce(ctx, client, from, req.Nonce)
	if err != nil {
		return domaincontract.BroadcastResponse{}, err
	}

	gasLimit := req.GasLimit
	if gasLimit == 0 && !req.SkipEstimate {
		gasLimit, err = client.EstimateGas(ctx, ethereum.CallMsg{
			From: from,
			To:   &req.Executor,
			Data: data,
		})
		if err != nil {
			return domaincontract.BroadcastResponse{}, fmt.Errorf("estimate gas: %w", err)
		}
	}
	if gasLimit == 0 {
		return domaincontract.BroadcastResponse{}, errors.New("gasLimit is required when gas estimation is skipped")
	}

	gasPrice := req.GasPriceWei
	if gasPrice == nil {
		gasPrice, err = client.SuggestGasPrice(ctx)
		if err != nil {
			return domaincontract.BroadcastResponse{}, fmt.Errorf("suggest gas price: %w", err)
		}
	}

	tx := types.NewTransaction(nonce, req.Executor, new(big.Int), gasLimit, new(big.Int).Set(gasPrice), data)
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("sign tx: %w", err)
	}
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("send tx: %w", err)
	}

	return domaincontract.BroadcastResponse{TxHash: signedTx.Hash()}, nil
}

func parsePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	if err != nil {
		return nil, fmt.Errorf("parse privateKey: %w", err)
	}
	return privateKey, nil
}

func resolveNonce(ctx context.Context, client *ethclient.Client, from common.Address, nonce *uint64) (uint64, error) {
	if nonce != nil {
		return *nonce, nil
	}
	next, err := client.PendingNonceAt(ctx, from)
	if err != nil {
		return 0, fmt.Errorf("pending nonce: %w", err)
	}
	return next, nil
}

type flashLoanABI struct {
	Protocol     uint8
	Lender       common.Address
	Token        common.Address
	Amount       *big.Int
	BorrowToken0 bool
}

type swapRouteABI struct {
	RouterAddress common.Address
	Value         *big.Int
	Data          []byte
	FillToken     common.Address
	FillOffset    *big.Int
}

type executionPlanABI struct {
	Loan             flashLoanABI
	Routers          []swapRouteABI
	SettleCurrencies []common.Address
	ProfitToken      common.Address
	MinProfit        *big.Int
	Deadline         *big.Int
}

func toExecutionPlanABI(plan domaincontract.ExecutionPlan) executionPlanABI {
	routers := make([]swapRouteABI, 0, len(plan.Routes))
	for _, route := range plan.Routes {
		routers = append(routers, swapRouteABI{
			RouterAddress: route.RouterAddress,
			Value:         zeroIfNilBigInt(route.Value),
			Data:          append([]byte(nil), route.Data...),
			FillToken:     route.FillToken,
			FillOffset:    new(big.Int).SetUint64(route.FillOffset),
		})
	}

	settleCurrencies := append([]common.Address(nil), plan.SettleCurrencies...)
	if settleCurrencies == nil {
		settleCurrencies = []common.Address{}
	}

	return executionPlanABI{
		Loan: flashLoanABI{
			Protocol:     flashLoanProtocolABI(plan.Loan.Protocol),
			Lender:       plan.Loan.Lender,
			Token:        plan.Loan.Token,
			Amount:       zeroIfNilBigInt(plan.Loan.Amount),
			BorrowToken0: plan.Loan.BorrowToken0,
		},
		Routers:          routers,
		SettleCurrencies: settleCurrencies,
		ProfitToken:      plan.ProfitToken,
		MinProfit:        zeroIfNilBigInt(plan.MinProfit),
		Deadline:         zeroIfNilBigInt(plan.Deadline),
	}
}

func flashLoanProtocolABI(protocol domaincontract.FlashLoanProtocol) uint8 {
	switch protocol {
	case domaincontract.FlashLoanProtocolUniswapV3:
		return 1
	case domaincontract.FlashLoanProtocolUniswapV4:
		return 2
	default:
		return 0
	}
}

func zeroIfNilBigInt(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return new(big.Int).Set(v)
}
