package balancer

// VaultVersion selects which Balancer Vault ABI to use for on-chain reads.
type VaultVersion int

const (
	VaultV2 VaultVersion = 2
	VaultV3 VaultVersion = 3
)

func (v VaultVersion) IsV3() bool {
	return v == VaultV3
}
