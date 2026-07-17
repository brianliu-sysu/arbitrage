package blockchain

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log"
	"math/big"
	"regexp"
	"strings"

	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const arbitrageExecutorABI = `[
	{
		"type":"error",
		"name":"SwapCallFailed",
		"inputs":[
			{"name":"index","type":"uint256"},
			{"name":"router","type":"address"},
			{"name":"reason","type":"bytes"}
		]
	},
	{
		"type":"error",
		"name":"InsufficientRepayBalance",
		"inputs":[
			{"name":"token","type":"address"},
			{"name":"balance","type":"uint256"},
			{"name":"required","type":"uint256"}
		]
	},
	{
		"type":"function",
		"name":"approveToken",
		"stateMutability":"nonpayable",
		"inputs":[
			{"name":"token","type":"address"},
			{"name":"spender","type":"address"},
			{"name":"amount","type":"uint256"}
		],
		"outputs":[]
	},
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
							{"name":"fillSource","type":"uint8"},
							{"name":"fillToken","type":"address"},
							{"name":"patchAmount","type":"bool"},
							{"name":"amountAsCallValue","type":"bool"},
							{"name":"fillOffset","type":"uint256"}
						]
					},
					{"name":"settleCurrencies","type":"address[]"},
					{"name":"profitToken","type":"address"},
					{"name":"minProfit","type":"uint256"},
					{"name":"deadline","type":"uint256"}
				]
			},
			{"name":"coinbasePaymentBps","type":"uint16"},
			{"name":"wrappedNativeToken","type":"address"}
		],
		"outputs":[{"name":"profit","type":"uint256"}]
	}
]`

const erc20AllowanceABI = `[
	{
		"type":"function",
		"name":"allowance",
		"stateMutability":"view",
		"inputs":[
			{"name":"owner","type":"address"},
			{"name":"spender","type":"address"}
		],
		"outputs":[{"name":"","type":"uint256"}]
	}
]`

type ContractExecutorBroadcaster struct {
	parsedABI abi.ABI
	erc20ABI  abi.ABI
}

func NewContractExecutorBroadcaster() (*ContractExecutorBroadcaster, error) {
	parsedABI, err := abi.JSON(strings.NewReader(arbitrageExecutorABI))
	if err != nil {
		return nil, fmt.Errorf("parse arbitrage executor abi: %w", err)
	}
	erc20ABI, err := abi.JSON(strings.NewReader(erc20AllowanceABI))
	if err != nil {
		return nil, fmt.Errorf("parse erc20 abi: %w", err)
	}
	return &ContractExecutorBroadcaster{parsedABI: parsedABI, erc20ABI: erc20ABI}, nil
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

	data, err := b.parsedABI.Pack("execute", toExecutionPlanABI(req.Plan), req.Plan.CoinbasePaymentBPS, req.Plan.WrappedNativeToken)
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("pack execute calldata: %w", err)
	}
	logExecuteCalldata("broadcast", from, req.Executor, chainID, data)

	txHash, err := b.sendTransaction(ctx, client, req, from, privateKey, chainID, data)
	if err != nil {
		return domaincontract.BroadcastResponse{}, err
	}
	return domaincontract.BroadcastResponse{TxHash: txHash}, nil
}

func (b *ContractExecutorBroadcaster) SimulateExecution(
	ctx context.Context,
	req domaincontract.BroadcastRequest,
) error {
	if b == nil {
		return errors.New("contract executor broadcaster is nil")
	}

	privateKey, err := parsePrivateKey(req.PrivateKey)
	if err != nil {
		return err
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)

	client, err := ethclient.DialContext(ctx, req.RPCURL)
	if err != nil {
		return fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()

	data, err := b.parsedABI.Pack("execute", toExecutionPlanABI(req.Plan), req.Plan.CoinbasePaymentBPS, req.Plan.WrappedNativeToken)
	if err != nil {
		return fmt.Errorf("pack execute calldata: %w", err)
	}
	logExecuteCalldata("simulate", from, req.Executor, nil, data)
	_, err = client.CallContract(ctx, ethereum.CallMsg{
		From: from,
		To:   &req.Executor,
		Data: data,
	}, nil)
	if err != nil {
		return fmt.Errorf("simulate execute: %w%s", err, b.decodeRevertError(err))
	}
	return nil
}

func logExecuteCalldata(stage string, from common.Address, executor common.Address, chainID *big.Int, data []byte) {
	chainIDText := ""
	if chainID != nil {
		chainIDText = chainID.String()
	}
	log.Printf(
		"contract executor %s calldata from=%s executor=%s chain_id=%s data=%s",
		stage,
		from.Hex(),
		executor.Hex(),
		chainIDText,
		hexutil.Encode(data),
	)
}

func (b *ContractExecutorBroadcaster) decodeRevertError(err error) string {
	if err == nil {
		return ""
	}
	raw, ok := ethclient.RevertErrorData(err)
	if !ok {
		raw = extractHexRevertData(err.Error())
	}
	if len(raw) < 4 {
		return ""
	}
	decoded := b.decodeABIError(raw)
	if decoded == "" {
		return ""
	}
	return ": " + decoded
}

var hexDataPattern = regexp.MustCompile(`0x[0-9a-fA-F]{8,}`)

func extractHexRevertData(message string) []byte {
	matches := hexDataPattern.FindAllString(message, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		raw, err := hexutil.Decode(matches[i])
		if err == nil && len(raw) >= 4 {
			return raw
		}
	}
	return nil
}

func (b *ContractExecutorBroadcaster) decodeABIError(raw []byte) string {
	if b == nil || len(raw) < 4 {
		return ""
	}
	for name, abiError := range b.parsedABI.Errors {
		if len(abiError.ID) >= 4 && string(abiError.ID[:4]) == string(raw[:4]) {
			values, err := abiError.Inputs.Unpack(raw[4:])
			if err != nil {
				return fmt.Sprintf("%s(unpack failed: %v)", name, err)
			}
			return formatABIError(name, values)
		}
	}
	return fmt.Sprintf("custom error 0x%x", raw[:4])
}

func formatABIError(name string, values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		switch typed := value.(type) {
		case *big.Int:
			parts = append(parts, typed.String())
		case common.Address:
			parts = append(parts, typed.Hex())
		case []byte:
			parts = append(parts, "0x"+common.Bytes2Hex(typed))
		default:
			parts = append(parts, fmt.Sprintf("%v", typed))
		}
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(parts, ", "))
}

func (b *ContractExecutorBroadcaster) Allowance(
	ctx context.Context,
	rpcURL string,
	token common.Address,
	owner common.Address,
	spender common.Address,
) (*big.Int, error) {
	if b == nil {
		return nil, errors.New("contract executor broadcaster is nil")
	}
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()

	data, err := b.erc20ABI.Pack("allowance", owner, spender)
	if err != nil {
		return nil, fmt.Errorf("pack allowance calldata: %w", err)
	}
	output, err := client.CallContract(ctx, ethereum.CallMsg{
		To:   &token,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("call allowance: %w", err)
	}
	values, err := b.erc20ABI.Unpack("allowance", output)
	if err != nil {
		return nil, fmt.Errorf("unpack allowance: %w", err)
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("unexpected allowance output count %d", len(values))
	}
	allowance, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected allowance output type %T", values[0])
	}
	return new(big.Int).Set(allowance), nil
}

func (b *ContractExecutorBroadcaster) BroadcastApprove(
	ctx context.Context,
	req domaincontract.BroadcastRequest,
	approval domaincontract.TokenApproval,
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

	data, err := b.parsedABI.Pack("approveToken", approval.Token, approval.Spender, maxUint256Big())
	if err != nil {
		return domaincontract.BroadcastResponse{}, fmt.Errorf("pack approveToken calldata: %w", err)
	}

	txHash, err := b.sendTransaction(ctx, client, req, from, privateKey, chainID, data)
	if err != nil {
		return domaincontract.BroadcastResponse{}, err
	}
	return domaincontract.BroadcastResponse{TxHash: txHash}, nil
}

func (b *ContractExecutorBroadcaster) sendTransaction(
	ctx context.Context,
	client *ethclient.Client,
	req domaincontract.BroadcastRequest,
	from common.Address,
	privateKey *ecdsa.PrivateKey,
	chainID *big.Int,
	data []byte,
) (common.Hash, error) {
	nonce, err := resolveNonce(ctx, client, from, req.Nonce)
	if err != nil {
		return common.Hash{}, err
	}

	gasLimit := req.GasLimit
	if gasLimit == 0 && !req.SkipEstimate {
		estimatedGas, estimateErr := client.EstimateGas(ctx, ethereum.CallMsg{
			From: from,
			To:   &req.Executor,
			Data: data,
		})
		if estimateErr != nil {
			return common.Hash{}, fmt.Errorf("estimate gas: %w", estimateErr)
		}
		gasLimit = estimatedGas
	}
	if gasLimit == 0 {
		return common.Hash{}, errors.New("gasLimit is required when gas estimation is skipped")
	}

	submitRPCURL := strings.TrimSpace(req.SubmitRPCURL)
	var tx *types.Transaction
	var targetBlock uint64
	if submitRPCURL != "" {
		header, headerErr := client.HeaderByNumber(ctx, nil)
		if headerErr != nil {
			return common.Hash{}, fmt.Errorf("latest header: %w", headerErr)
		}
		if header.BaseFee == nil {
			return common.Hash{}, errors.New("latest block baseFee is required for flashbots bundle")
		}
		tipCap := req.GasPriceWei
		if tipCap == nil {
			tipCap, err = client.SuggestGasTipCap(ctx)
			if err != nil {
				return common.Hash{}, fmt.Errorf("suggest gas tip cap: %w", err)
			}
		}
		feeCap := new(big.Int).Add(new(big.Int).Mul(header.BaseFee, big.NewInt(2)), tipCap)
		targetBlock = header.Number.Uint64() + 1
		tx = types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: tipCap,
			GasFeeCap: feeCap,
			Gas:       gasLimit,
			To:        &req.Executor,
			Data:      data,
		})
	} else {
		gasPrice := req.GasPriceWei
		if gasPrice == nil {
			gasPrice, err = client.SuggestGasPrice(ctx)
			if err != nil {
				return common.Hash{}, fmt.Errorf("suggest gas price: %w", err)
			}
		}
		tx = types.NewTransaction(nonce, req.Executor, new(big.Int), gasLimit, new(big.Int).Set(gasPrice), data)
	}

	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign tx: %w", err)
	}
	if submitRPCURL != "" {
		if err := submitFlashbotsBundles(ctx, submitRPCURL, privateKey, signedTx, targetBlock, 3); err != nil {
			return common.Hash{}, err
		}
	} else if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, fmt.Errorf("send tx: %w", err)
	}

	return signedTx.Hash(), nil
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
	RouterAddress     common.Address
	Value             *big.Int
	Data              []byte
	FillSource        uint8
	FillToken         common.Address
	PatchAmount       bool
	AmountAsCallValue bool
	FillOffset        *big.Int
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
			RouterAddress:     route.RouterAddress,
			Value:             zeroIfNilBigInt(route.Value),
			Data:              append([]byte(nil), route.Data...),
			FillSource:        uint8(route.FillSource),
			FillToken:         route.FillToken,
			PatchAmount:       route.PatchAmount,
			AmountAsCallValue: route.AmountAsCallValue,
			FillOffset:        new(big.Int).SetUint64(route.FillOffset),
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

func maxUint256Big() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
}
