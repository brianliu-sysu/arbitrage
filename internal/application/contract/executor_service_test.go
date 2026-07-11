package contract_test

import (
	"context"
	"math/big"
	"testing"

	contractapp "github.com/brianliu-sysu/uniswapv3/internal/application/contract"
	domaincontract "github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
)

func TestEnsureApprovalsBroadcastsAndCachesMaxApproval(t *testing.T) {
	broadcaster := &fakeBroadcaster{allowance: big.NewInt(0)}
	service := contractapp.NewAppService(broadcaster)

	req := domaincontract.EnsureApprovalsRequest{
		RPCURL:     "http://127.0.0.1:8545",
		PrivateKey: "0xabc",
		Executor:   common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		Approvals: []domaincontract.TokenApproval{{
			Token:   common.HexToAddress("0x00000000000000000000000000000000000000bb"),
			Spender: common.HexToAddress("0x00000000000000000000000000000000000000cc"),
			Amount:  big.NewInt(100),
		}},
		GasLimit:     100_000,
		SkipEstimate: true,
	}

	resp, err := service.EnsureApprovals(context.Background(), req)
	if err != nil {
		t.Fatalf("ensure approvals: %v", err)
	}
	if !resp.Broadcast || broadcaster.approveCalls != 1 {
		t.Fatalf("expected approval broadcast, resp=%+v calls=%d", resp, broadcaster.approveCalls)
	}

	resp, err = service.EnsureApprovals(context.Background(), req)
	if err != nil {
		t.Fatalf("ensure approvals cached: %v", err)
	}
	if resp.Broadcast {
		t.Fatalf("expected cached approval to skip broadcast")
	}
	if broadcaster.allowanceCalls != 1 {
		t.Fatalf("expected one on-chain allowance check, got %d", broadcaster.allowanceCalls)
	}
}

func TestValidateBroadcastRequestAllowsNativeValueOnlyFill(t *testing.T) {
	req := domaincontract.BroadcastRequest{
		RPCURL:     "http://127.0.0.1:8545",
		PrivateKey: "0xabc",
		Executor:   common.HexToAddress("0x00000000000000000000000000000000000000aa"),
		Plan: domaincontract.ExecutionPlan{
			Loan: domaincontract.FlashLoan{
				Protocol: domaincontract.FlashLoanProtocolBalancer,
				Lender:   common.HexToAddress("0x00000000000000000000000000000000000000bb"),
				Token:    common.HexToAddress("0x00000000000000000000000000000000000000cc"),
				Amount:   big.NewInt(1),
			},
			Routes: []domaincontract.SwapRoute{{
				RouterAddress:     common.HexToAddress("0x00000000000000000000000000000000000000dd"),
				Data:              make([]byte, 128),
				FillSource:        domaincontract.FillSourceNativeBalance,
				AmountAsCallValue: true,
			}},
			ProfitToken: common.HexToAddress("0x00000000000000000000000000000000000000cc"),
		},
	}

	if err := contractapp.ValidateBroadcastRequest(req); err != nil {
		t.Fatalf("validate broadcast request: %v", err)
	}
}

type fakeBroadcaster struct {
	allowance      *big.Int
	allowanceCalls int
	approveCalls   int
}

func (f *fakeBroadcaster) BroadcastExecution(context.Context, domaincontract.BroadcastRequest) (domaincontract.BroadcastResponse, error) {
	return domaincontract.BroadcastResponse{TxHash: common.HexToHash("0x1")}, nil
}

func (f *fakeBroadcaster) SimulateExecution(context.Context, domaincontract.BroadcastRequest) error {
	return nil
}

func (f *fakeBroadcaster) Allowance(context.Context, string, common.Address, common.Address, common.Address) (*big.Int, error) {
	f.allowanceCalls++
	return new(big.Int).Set(f.allowance), nil
}

func (f *fakeBroadcaster) BroadcastApprove(context.Context, domaincontract.BroadcastRequest, domaincontract.TokenApproval) (domaincontract.BroadcastResponse, error) {
	f.approveCalls++
	return domaincontract.BroadcastResponse{TxHash: common.HexToHash("0x2")}, nil
}
