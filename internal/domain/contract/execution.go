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

type SwapRoute struct {
	RouterAddress common.Address
	Value         *big.Int
	Data          []byte
	// FillToken, if set, overwrites Data[FillOffset:FillOffset+32] with the live balance
	// when the 32-byte slot fits in Data. Use address(0) to disable.
	// Use 0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE for native ETH: the executor also
	// overrides call msg.value with its ETH balance (needed for WETH.deposit / native UR).
	FillToken  common.Address
	FillOffset uint64
}

type ExecutionPlan struct {
	Loan             FlashLoan
	Routes           []SwapRoute
	SettleCurrencies []common.Address // currencies that may hold open V4 PoolManager deltas
	ProfitToken      common.Address
	MinProfit        *big.Int
	Deadline         *big.Int
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
