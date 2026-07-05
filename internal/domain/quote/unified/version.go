package unified

// PoolVersion identifies which Uniswap protocol a pool hop uses.
type PoolVersion int

const (
	PoolVersionV3 PoolVersion = iota + 1
	PoolVersionPancakeV3
	PoolVersionV4
)

func (v PoolVersion) String() string {
	switch v {
	case PoolVersionV3:
		return "univ3"
	case PoolVersionPancakeV3:
		return "pancakev3"
	case PoolVersionV4:
		return "univ4"
	default:
		return "unknown"
	}
}
