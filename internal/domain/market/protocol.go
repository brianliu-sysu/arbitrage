package market

// Protocol identifies which AMM protocol a pool belongs to.
type Protocol uint8

const (
	ProtocolUnknown Protocol = iota
	ProtocolUniswapV3
	ProtocolV4
	ProtocolPancakeV3
	ProtocolBalancer
	ProtocolQuickSwapV3
)

// ProtocolV3 is kept for backward compatibility with Uniswap V3 pools.
const ProtocolV3 = ProtocolUniswapV3

func (p Protocol) String() string {
	switch p {
	case ProtocolUniswapV3:
		return "univ3"
	case ProtocolV4:
		return "univ4"
	case ProtocolPancakeV3:
		return "pancakev3"
	case ProtocolBalancer:
		return "balancer"
	case ProtocolQuickSwapV3:
		return "quickswapv3"
	default:
		return "unknown"
	}
}
