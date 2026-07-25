package marketpipeline

import (
	"context"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	marketuniv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/marketchange"
	"github.com/ethereum/go-ethereum/common"
)

type ReportReceiver interface {
	ReportApplied(context.Context, ProtocolBlockReport) error
}

type Univ3PoolListener struct{ Receiver ReportReceiver }

func (l *Univ3PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return report(ctx, l.Receiver, ProtocolBlockReport{
		Protocol: ProtocolUniv3, BlockNumber: blockNumber, Changes: marketchange.Changes{Univ3: pools},
	})
}

type PancakeV3PoolListener struct{ Receiver ReportReceiver }

func (l *PancakeV3PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return report(ctx, l.Receiver, ProtocolBlockReport{
		Protocol: ProtocolPancakeV3, BlockNumber: blockNumber, Changes: marketchange.Changes{PancakeV3: pools},
	})
}

type QuickSwapV3PoolListener struct{ Receiver ReportReceiver }

func (l *QuickSwapV3PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []common.Address) error {
	return report(ctx, l.Receiver, ProtocolBlockReport{
		Protocol: ProtocolQuickSwapV3, BlockNumber: blockNumber, Changes: marketchange.Changes{QuickSwapV3: pools},
	})
}

type Univ4PoolListener struct{ Receiver ReportReceiver }

func (l *Univ4PoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketuniv4.PoolID) error {
	return report(ctx, l.Receiver, ProtocolBlockReport{
		Protocol: ProtocolUniv4, BlockNumber: blockNumber, Changes: marketchange.Changes{Univ4: pools},
	})
}

type BalancerPoolListener struct{ Receiver ReportReceiver }

func (l *BalancerPoolListener) OnPoolsChanged(ctx context.Context, blockNumber uint64, pools []marketbalancer.PoolID) error {
	return report(ctx, l.Receiver, ProtocolBlockReport{
		Protocol: ProtocolBalancer, BlockNumber: blockNumber, Changes: marketchange.Changes{Balancer: pools},
	})
}

func report(ctx context.Context, receiver ReportReceiver, block ProtocolBlockReport) error {
	if receiver == nil {
		return nil
	}
	return receiver.ReportApplied(ctx, block)
}
