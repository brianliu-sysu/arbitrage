package market

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestPoolRefFromV3(t *testing.T) {
	address := common.HexToAddress("0x88e6A0c2dDD26FEEb64F039a2c41296Fb728693B")
	ref := PoolRefFromV3(address)
	if ref.Protocol != ProtocolV3 {
		t.Fatalf("expected v3, got %s", ref.Protocol)
	}
	if ref.Address != address {
		t.Fatalf("expected address %s, got %s", address, ref.Address)
	}
	if ref.IsZero() {
		t.Fatal("expected non-zero ref")
	}
}

func TestPoolRefFromV4(t *testing.T) {
	poolID := common.HexToHash("0x1234567890123456789012345678901234567890123456789012345678901234")
	ref := PoolRefFromV4(poolID)
	if ref.Protocol != ProtocolV4 {
		t.Fatalf("expected v4, got %s", ref.Protocol)
	}
	if ref.PoolID != poolID {
		t.Fatalf("expected pool id %s, got %s", poolID, ref.PoolID)
	}
	if ref.String() != "v4:"+poolID.Hex() {
		t.Fatalf("unexpected string %s", ref.String())
	}
}
