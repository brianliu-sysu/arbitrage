package contract_test

import (
	"math/big"
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/contract"
	"github.com/ethereum/go-ethereum/common"
)

func TestRequiredTokenApprovalsFromExactInputSingle(t *testing.T) {
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	router := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	data, err := contract.PackExactInputSingle(contract.ExactInputSingleParams{
		TokenIn:          usdc,
		TokenOut:         weth,
		Fee:              big.NewInt(500),
		Recipient:        common.HexToAddress("0x1"),
		AmountIn:         big.NewInt(0),
		AmountOutMinimum: big.NewInt(0),
	})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	got := contract.RequiredTokenApprovals(contract.ExecutionPlan{
		Routes: []contract.SwapRoute{{
			RouterAddress: router,
			Data:          data,
			FillSource:    contract.FillSourceERC20Balance,
			FillToken:     usdc,
			PatchAmount:   true,
			FillOffset:    contract.ExactInputSingleAmountInOffset,
		}},
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 approval, got %+v", got)
	}
	if got[0].Token != usdc || got[0].Spender != router {
		t.Fatalf("unexpected approval %+v", got[0])
	}
}

func TestMergeTokenApprovalsAddsMissingRouterAllowance(t *testing.T) {
	usdc := common.HexToAddress("0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48")
	weth := common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2")
	router := common.HexToAddress("0x68b3465833fb72A70ecDF485E0e4C7bD8665Fc45")
	vault := common.HexToAddress("0xBA12222222228d8Ba445958a75a0704d566BF2C8")

	data, err := contract.PackExactInputSingle(contract.ExactInputSingleParams{
		TokenIn:  usdc,
		TokenOut: weth,
		Fee:      big.NewInt(500),
		AmountIn: big.NewInt(1),
	})
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	plan := contract.ExecutionPlan{
		Routes: []contract.SwapRoute{{
			RouterAddress: router,
			Data:          data,
		}},
	}
	merged := contract.MergeTokenApprovals(
		[]contract.TokenApproval{{Token: weth, Spender: vault, Amount: big.NewInt(1)}},
		contract.RequiredTokenApprovals(plan),
	)
	if len(merged) != 2 {
		t.Fatalf("expected merged approvals to include USDC->router, got %+v", merged)
	}
	found := false
	for _, item := range merged {
		if item.Token == usdc && item.Spender == router {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing USDC->router approval in %+v", merged)
	}
}
