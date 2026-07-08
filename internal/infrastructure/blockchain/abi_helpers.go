package blockchain

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

func topicToInt24(topic common.Hash) (int32, error) {
	return abiInt24ToInt32(new(big.Int).SetBytes(topic.Bytes()))
}

func int32ToABIInt24(value int32) *big.Int {
	return big.NewInt(int64(value))
}

func abiUintToBigInt(value interface{}) (*big.Int, error) {
	switch v := value.(type) {
	case *big.Int:
		if v == nil {
			return nil, fmt.Errorf("nil big.Int")
		}
		return new(big.Int).Set(v), nil
	case uint64:
		return new(big.Int).SetUint64(v), nil
	case uint32:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint16:
		return new(big.Int).SetUint64(uint64(v)), nil
	case uint8:
		return new(big.Int).SetUint64(uint64(v)), nil
	default:
		return nil, fmt.Errorf("unsupported uint type %T", value)
	}
}

func abiUintToUint32(value interface{}) (uint32, error) {
	switch v := value.(type) {
	case *big.Int:
		if !v.IsUint64() || v.Uint64() > uint64(^uint32(0)) {
			return 0, fmt.Errorf("uint value %s overflows uint32", v.String())
		}
		return uint32(v.Uint64()), nil
	case uint64:
		if v > uint64(^uint32(0)) {
			return 0, fmt.Errorf("uint value %d overflows uint32", v)
		}
		return uint32(v), nil
	case uint32:
		return v, nil
	case uint16:
		return uint32(v), nil
	case uint8:
		return uint32(v), nil
	default:
		return 0, fmt.Errorf("unsupported uint type %T", value)
	}
}

func abiInt24ToInt32(value interface{}) (int32, error) {
	switch v := value.(type) {
	case int32:
		return v, nil
	case int64:
		return int32(v), nil
	case *big.Int:
		return bigIntToInt24(v)
	default:
		return 0, fmt.Errorf("unsupported int24 type %T", value)
	}
}

func bigIntToInt24(value *big.Int) (int32, error) {
	minInt24 := big.NewInt(-1 << 23)
	maxInt24 := big.NewInt(1<<23 - 1)

	working := new(big.Int).Set(value)
	if working.Cmp(minInt24) < 0 || working.Cmp(maxInt24) > 0 {
		two256 := new(big.Int).Lsh(big.NewInt(1), 256)
		if working.Bit(255) == 1 {
			working.Sub(working, two256)
		}
	}
	if working.Cmp(minInt24) < 0 || working.Cmp(maxInt24) > 0 {
		return 0, fmt.Errorf("value %s out of int24 range", value.String())
	}
	return int32(working.Int64()), nil
}
