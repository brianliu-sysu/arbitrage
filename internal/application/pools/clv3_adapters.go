package poolsapp

import (
	"context"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

func univ3CLV3Source(s *AppService) clv3PoolSource {
	src := clv3PoolSource{
		poolType:  PoolTypeUniv3,
		listLabel: "univ3",
		readLabel: "univ3",
		registry:  s.univ3Registry,
		pools: clv3PoolRepoFunc(func(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
			pool, err := s.univ3Pools.Get(ctx, address)
			if pool == nil {
				return nil, err
			}
			return pool.Pool.Clone(), nil
		}),
	}
	if s.chain != nil {
		src.reader = s.chain.V3
	}
	return src
}

func pancakeCLV3Source(s *AppService) clv3PoolSource {
	src := clv3PoolSource{
		poolType:  PoolTypePancakeV3,
		listLabel: "pancakev3",
		readLabel: "pancakev3",
		registry:  s.pancakeRegistry,
		pools: clv3PoolRepoFunc(func(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
			pool, err := s.pancakePools.Get(ctx, address)
			if pool == nil {
				return nil, err
			}
			return pool.Pool.Clone(), nil
		}),
	}
	if s.chain != nil {
		src.reader = s.chain.Pancake
	}
	return src
}

func univ3CLV3Pools(s *AppService) CLV3PoolRepository {
	return clv3PoolRepoFunc(func(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
		pool, err := s.univ3Pools.Get(ctx, address)
		if pool == nil {
			return nil, err
		}
		return pool.Pool.Clone(), nil
	})
}

func pancakeCLV3Pools(s *AppService) CLV3PoolRepository {
	return clv3PoolRepoFunc(func(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
		pool, err := s.pancakePools.Get(ctx, address)
		if pool == nil {
			return nil, err
		}
		return pool.Pool.Clone(), nil
	})
}
