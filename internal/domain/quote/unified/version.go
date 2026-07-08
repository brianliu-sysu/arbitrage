package unified

// PoolVersion identifies which Uniswap protocol a pool hop uses.
type PoolVersion int

const (
	PoolVersionV3 PoolVersion = iota + 1
	PoolVersionPancakeV3
	PoolVersionQuickSwapV3
	PoolVersionV4
	PoolVersionBalancer
	PoolVersionUnwrapWETH
	PoolVersionWrapWETH
)

func (v PoolVersion) String() string {
	switch v {
	case PoolVersionV3:
		return "univ3"
	case PoolVersionPancakeV3:
		return "pancakev3"
	case PoolVersionQuickSwapV3:
		return "quickswapv3"
	case PoolVersionV4:
		return "univ4"
	case PoolVersionBalancer:
		return "balancer"
	case PoolVersionUnwrapWETH:
		return "unwrap_weth"
	case PoolVersionWrapWETH:
		return "wrap_weth"
	default:
		return "unknown"
	}
}
