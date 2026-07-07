package poolsapp

import (
	"context"
	"fmt"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

// CLV3PoolRegistry lists tracked concentrated-liquidity V3 pool addresses.
type CLV3PoolRegistry interface {
	List(ctx context.Context) ([]common.Address, error)
}

// CLV3PoolRepository loads concentrated-liquidity V3 pool state.
type CLV3PoolRepository interface {
	Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error)
}

type clv3PoolRegistryFunc func(context.Context) ([]common.Address, error)

func (f clv3PoolRegistryFunc) List(ctx context.Context) ([]common.Address, error) {
	return f(ctx)
}

type clv3PoolRepoFunc func(context.Context, common.Address) (*marketclv3.Pool, error)

func (f clv3PoolRepoFunc) Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error) {
	return f(ctx, address)
}

type clv3PoolSource struct {
	poolType  string
	listLabel string
	readLabel string
	registry  CLV3PoolRegistry
	pools     CLV3PoolRepository
	reader    V3BaseStateReader
}

func appendCLV3PoolInfos(ctx context.Context, src clv3PoolSource, out *[]PoolInfo) error {
	if src.registry == nil || src.pools == nil {
		return nil
	}
	addresses, err := src.registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", src.listLabel, err)
	}
	for _, address := range addresses {
		pool, err := src.pools.Get(ctx, address)
		if err != nil {
			return fmt.Errorf("load %s pool %s: %w", src.listLabel, address.Hex(), err)
		}
		if pool == nil {
			continue
		}
		*out = append(*out, PoolInfo{
			PoolAddress: address.Hex(),
			PoolType:    src.poolType,
			Token0:      tokenInfoFromAddress(pool.Token0),
			Token1:      tokenInfoFromAddress(pool.Token1),
			Fee:         pool.Fee,
		})
	}
	return nil
}

func appendMismatchingCLV3Pools(
	ctx context.Context,
	s *AppService,
	src clv3PoolSource,
	head uint64,
	items *[]DiagnosticsResponse,
) error {
	if src.registry == nil || src.pools == nil || src.reader == nil {
		return nil
	}
	addresses, err := src.registry.List(ctx)
	if err != nil {
		return fmt.Errorf("list %s pools: %w", src.listLabel, err)
	}

	type localPool struct {
		address common.Address
		pool    *marketclv3.Pool
	}
	locals := make([]localPool, 0, len(addresses))
	atHead := make([]common.Address, 0, len(addresses))
	tokenAddresses := make([]common.Address, 0, len(addresses)*2)

	for _, address := range addresses {
		pool, err := src.pools.Get(ctx, address)
		if err != nil || pool == nil {
			continue
		}
		locals = append(locals, localPool{address: address, pool: pool})
		if pool.LastBlockNumber != head {
			continue
		}
		atHead = append(atHead, address)
		tokenAddresses = append(tokenAddresses, pool.Token0, pool.Token1)
	}

	chainStates, err := readManyV3BaseStates(ctx, src.reader, atHead, head)
	if err != nil {
		return fmt.Errorf("read %s chain states: %w", src.readLabel, err)
	}

	tokenMeta, err := s.resolveTokenMetadata(ctx, tokenAddresses...)
	if err != nil {
		return err
	}

	for _, local := range locals {
		if local.pool.LastBlockNumber != head {
			continue
		}
		chainState := chainStates[local.address]
		if chainState == nil {
			continue
		}
		token0 := enrichToken(tokenInfoFromAddress(local.pool.Token0), tokenMeta)
		token1 := enrichToken(tokenInfoFromAddress(local.pool.Token1), tokenMeta)
		resp := buildV3DiagnosticsResponse(
			src.poolType,
			local.address,
			token0,
			token1,
			local.pool.Fee,
			local.pool.State.SqrtPriceX96,
			local.pool.State.Tick,
			local.pool.State.Liquidity,
			local.pool.LastBlockNumber,
			string(local.pool.Status),
			head,
			chainState,
		)
		if isMismatchingAtHead(resp) {
			*items = append(*items, *resp)
		}
	}
	return nil
}

func (s *AppService) diagnosticsCLV3(
	ctx context.Context,
	poolType string,
	label string,
	poolAddress common.Address,
	head uint64,
	pools CLV3PoolRepository,
	reader V3BaseStateReader,
) (*DiagnosticsResponse, error) {
	if poolAddress == (common.Address{}) {
		return nil, fmt.Errorf("poolAddress is required for %s diagnostics", label)
	}
	if pools == nil {
		return nil, fmt.Errorf("%s pool repository is not configured", label)
	}
	if reader == nil {
		return nil, fmt.Errorf("%s chain reader is not configured", label)
	}

	pool, err := pools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load %s pool: %w", label, err)
	}
	if pool == nil {
		return nil, fmt.Errorf("%s pool %s not found", label, poolAddress.Hex())
	}

	return s.buildV3Diagnostics(
		ctx,
		poolType,
		poolAddress,
		pool.Token0,
		pool.Token1,
		pool.Fee,
		pool.State.SqrtPriceX96,
		pool.State.Tick,
		pool.State.Liquidity,
		pool.LastBlockNumber,
		string(pool.Status),
		head,
		reader,
	)
}
