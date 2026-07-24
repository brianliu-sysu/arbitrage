package clv3

import (
	"context"
	"math/big"
	"testing"

	quoteshared "github.com/brianliu-sysu/uniswapv3/internal/domain/quote/shared"
	"github.com/ethereum/go-ethereum/common"
)

type switchingViewReadiness struct {
	blockCalls int
	readyCalls int
}

func (r *switchingViewReadiness) BlockNumber() uint64 {
	r.blockCalls++
	if r.blockCalls == 1 {
		return 10
	}
	return 11
}

func (r *switchingViewReadiness) IsSystemReady() bool {
	r.readyCalls++
	return false
}

func (*switchingViewReadiness) IsPoolReady(common.Address) bool { return false }

func TestQuoteRetriesWhenViewRevisionChanges(t *testing.T) {
	readiness := &switchingViewReadiness{}
	service := NewAppService(nil, nil, nil, readiness, 3)
	_, err := service.Quote(context.Background(), Request{
		TokenIn:  common.HexToAddress("0x1"),
		TokenOut: common.HexToAddress("0x2"),
		Mode:     quoteshared.QuoteModeExactInput,
		AmountIn: big.NewInt(1),
	})
	if err == nil {
		t.Fatal("expected readiness error")
	}
	if readiness.readyCalls != 2 {
		t.Fatalf("expected quote to retry after view switch, readiness calls=%d", readiness.readyCalls)
	}
}
