package contract

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type FlashLoanProtocol string

const (
	FlashLoanProtocolBalancer  FlashLoanProtocol = "balancer"
	FlashLoanProtocolUniswapV3 FlashLoanProtocol = "uniswapV3"
	FlashLoanProtocolUniswapV4 FlashLoanProtocol = "uniswapV4"
)

type FlashLoan struct {
	Protocol     FlashLoanProtocol
	Lender       common.Address
	Token        common.Address
	Amount       *big.Int
	BorrowToken0 bool
}

type FillSource uint8

const (
	FillSourceNone FillSource = iota
	FillSourceERC20Balance
	FillSourceNativeBalance
)

type SwapRoute struct {
	RouterAddress common.Address
	Value         *big.Int
	Data          []byte
	// FillSource controls how the executor computes a dynamic amount for this route.
	// ERC20Balance requires FillToken; NativeBalance uses native ETH via address(0) in the contract.
	FillSource        FillSource
	FillToken         common.Address
	PatchAmount       bool
	AmountAsCallValue bool
	FillOffset        uint64
}

type ExecutionPlan struct {
	Loan               FlashLoan
	Routes             []SwapRoute
	SettleCurrencies   []common.Address // currencies that may hold open V4 PoolManager deltas
	ProfitToken        common.Address
	MinProfit          *big.Int
	Deadline           *big.Int
	CoinbasePaymentBPS uint16
	WrappedNativeToken common.Address
}

type BroadcastRequest struct {
	RPCURL       string
	PrivateKey   string
	Executor     common.Address
	Plan         ExecutionPlan
	GasLimit     uint64
	GasPriceWei  *big.Int
	Nonce        *uint64
	SkipEstimate bool
	SubmitRPCURL string
}

type BroadcastResponse struct {
	TxHash common.Hash
}

type TokenApproval struct {
	Token   common.Address
	Spender common.Address
	Amount  *big.Int
}

type EnsureApprovalsRequest struct {
	RPCURL       string
	PrivateKey   string
	Executor     common.Address
	Approvals    []TokenApproval
	GasLimit     uint64
	GasPriceWei  *big.Int
	SkipEstimate bool
	SubmitRPCURL string
}

type EnsureApprovalsResponse struct {
	TxHashes  []common.Hash
	Broadcast bool
}
