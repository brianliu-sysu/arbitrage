package blockchain

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	topicInitialize  = crypto.Keccak256Hash([]byte("Initialize(uint160,int24)"))
	topicSwap        = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24)"))
	topicPancakeSwap = crypto.Keccak256Hash([]byte("Swap(address,address,int256,int256,uint160,uint128,int24,uint128,uint128)"))
	topicMint        = crypto.Keccak256Hash([]byte("Mint(address,address,int24,int24,uint128,uint256,uint256)"))
	topicBurn        = crypto.Keccak256Hash([]byte("Burn(address,int24,int24,uint128,uint256,uint256)"))
)

// ABIParser decodes Uniswap V3 pool logs into domain events.
type ABIParser = CLV3PoolParser

// PancakeABIParser decodes PancakeSwap V3 pool logs into domain events.
type PancakeABIParser = CLV3PoolParser

func NewABIParser() (*ABIParser, error) {
	parser, err := newCLV3PoolParser(poolABIJSON, topicSwap)
	if err != nil {
		return nil, fmt.Errorf("parse pool abi: %w", err)
	}
	return parser, nil
}

func NewPancakeABIParser() (*PancakeABIParser, error) {
	parser, err := newCLV3PoolParser(pancakePoolABIJSON, topicPancakeSwap)
	if err != nil {
		return nil, fmt.Errorf("parse pancake pool abi: %w", err)
	}
	return parser, nil
}

func PoolLogTopics() []common.Hash {
	return []common.Hash{topicInitialize, topicSwap, topicMint, topicBurn}
}

func PancakePoolLogTopics() []common.Hash {
	return []common.Hash{topicInitialize, topicPancakeSwap, topicMint, topicBurn}
}
