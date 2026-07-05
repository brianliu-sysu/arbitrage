package unified

// PoolVersion identifies which Uniswap protocol a pool hop uses.
type PoolVersion int

const (
	PoolVersionV3 PoolVersion = iota + 1
	PoolVersionV4
)

func (v PoolVersion) String() string {
	switch v {
	case PoolVersionV3:
		return "v3"
	case PoolVersionV4:
		return "v4"
	default:
		return "unknown"
	}
}
