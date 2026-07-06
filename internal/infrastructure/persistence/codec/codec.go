package codec

import (
	"database/sql"
	"math/big"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

func BigIntToNullString(v *big.Int) sql.NullString {
	if v == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: v.String(), Valid: true}
}

func NullStringToBigInt(v sql.NullString) *big.Int {
	if !v.Valid || v.String == "" {
		return big.NewInt(0)
	}
	out, ok := new(big.Int).SetString(v.String, 10)
	if !ok {
		return big.NewInt(0)
	}
	return out
}

func AddressToBytes(address common.Address) []byte {
	return address.Bytes()
}

func BytesToAddress(raw []byte) common.Address {
	if len(raw) == 0 {
		return common.Address{}
	}
	return common.BytesToAddress(raw)
}

func PoolStateFromRow(
	sqrtPrice sql.NullString,
	tick int32,
	liquidity sql.NullString,
	feeGrowth0 sql.NullString,
	feeGrowth1 sql.NullString,
) market.PoolState {
	return market.PoolState{
		SqrtPriceX96:         NullStringToBigInt(sqrtPrice),
		Tick:                 tick,
		Liquidity:            NullStringToBigInt(liquidity),
		FeeGrowthGlobal0X128: NullStringToBigInt(feeGrowth0),
		FeeGrowthGlobal1X128: NullStringToBigInt(feeGrowth1),
	}
}

func ClonePool(pool *marketv3.Pool) *marketv3.Pool {
	if pool == nil {
		return nil
	}
	return pool.Clone()
}

func CloneSnapshot(snapshot *marketv3.Snapshot) *marketv3.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.State = snapshot.State.Clone()
	cloned.Ticks = snapshot.Ticks.Clone()
	cloned.Bitmap = snapshot.Bitmap.Clone()
	return &cloned
}

func ClonePancakePool(pool *marketpancake.Pool) *marketpancake.Pool {
	if pool == nil {
		return nil
	}
	return pool.Clone()
}

func ClonePancakeSnapshot(snapshot *marketpancake.Snapshot) *marketpancake.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.State = snapshot.State.Clone()
	cloned.Ticks = snapshot.Ticks.Clone()
	cloned.Bitmap = snapshot.Bitmap.Clone()
	return &cloned
}

func PoolIDToBytes(id marketv4.PoolID) []byte {
	return id.Hash().Bytes()
}

func BytesToPoolID(raw []byte) marketv4.PoolID {
	if len(raw) == 0 {
		return marketv4.PoolID{}
	}
	return marketv4.PoolID(common.BytesToHash(raw))
}

func CloneV4Pool(pool *marketv4.Pool) *marketv4.Pool {
	if pool == nil {
		return nil
	}
	return pool.Clone()
}

func CloneV4Snapshot(snapshot *marketv4.Snapshot) *marketv4.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.State = snapshot.State.Clone()
	cloned.Ticks = snapshot.Ticks.Clone()
	cloned.Bitmap = snapshot.Bitmap.Clone()
	return &cloned
}
