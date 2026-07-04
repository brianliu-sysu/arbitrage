package market

// Protocol identifies which AMM protocol a pool belongs to.
type Protocol uint8

const (
	ProtocolUnknown Protocol = iota
	ProtocolV3
	ProtocolV4
)

func (p Protocol) String() string {
	switch p {
	case ProtocolV3:
		return "v3"
	case ProtocolV4:
		return "v4"
	default:
		return "unknown"
	}
}
