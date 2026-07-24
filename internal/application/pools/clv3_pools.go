package poolsapp

import (
	"context"
	"fmt"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type clv3PoolRegistry interface {
	List(ctx context.Context) ([]common.Address, error)
}

type clv3PoolRepository interface {
	Get(ctx context.Context, address common.Address) (*marketclv3.Pool, error)
}

type clv3PoolSource struct {
	poolType  string
	listLabel string
	registry  clv3PoolRegistry
	pools     clv3PoolRepository
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
	src clv3PoolSource,
	head uint64,
	resolve TokenMetadataResolver,
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
		return fmt.Errorf("read %s chain states: %w", src.listLabel, err)
	}

	tokenMeta, err := resolve(ctx, tokenAddresses...)
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

func diagnosticsCLV3(
	ctx context.Context,
	src clv3PoolSource,
	poolAddress common.Address,
	head uint64,
	resolve TokenMetadataResolver,
) (*DiagnosticsResponse, error) {
	if poolAddress == (common.Address{}) {
		return nil, fmt.Errorf("poolAddress is required for %s diagnostics", src.listLabel)
	}
	if src.pools == nil {
		return nil, fmt.Errorf("%s pool repository is not configured", src.listLabel)
	}
	if src.reader == nil {
		return nil, fmt.Errorf("%s chain reader is not configured", src.listLabel)
	}

	pool, err := src.pools.Get(ctx, poolAddress)
	if err != nil {
		return nil, fmt.Errorf("load %s pool: %w", src.listLabel, err)
	}
	if pool == nil {
		return nil, fmt.Errorf("%s pool %s not found", src.listLabel, poolAddress.Hex())
	}

	tokenMeta, err := resolve(ctx, pool.Token0, pool.Token1)
	if err != nil {
		return nil, err
	}
	token0 := enrichToken(tokenInfoFromAddress(pool.Token0), tokenMeta)
	token1 := enrichToken(tokenInfoFromAddress(pool.Token1), tokenMeta)
	chainState, err := src.reader.ReadV3BaseState(ctx, poolAddress, head)
	if err != nil {
		return nil, fmt.Errorf("read chain v3 state: %w", err)
	}
	return buildV3DiagnosticsResponse(src.poolType, poolAddress, token0, token1, pool.Fee,
		pool.State.SqrtPriceX96, pool.State.Tick, pool.State.Liquidity,
		pool.LastBlockNumber, string(pool.Status), head, chainState), nil
}
