package runtime

import "testing"

type readyProtocol struct {
	name  string
	ready bool
}

func (p *readyProtocol) Name() string  { return p.name }
func (p *readyProtocol) IsReady() bool { return p.ready }

func TestNewlyReadyProtocolsDoesNotMutateCommittedState(t *testing.T) {
	committed := map[string]bool{}
	ready := newlyReadyProtocols([]protocolReadiness{&readyProtocol{
		name:  "univ3",
		ready: true,
	}}, committed)

	if len(ready) != 1 || ready[0] != "univ3" {
		t.Fatalf("unexpected newly ready protocols: %v", ready)
	}
	if committed["univ3"] {
		t.Fatal("readiness must not be committed before route refresh succeeds")
	}
}
