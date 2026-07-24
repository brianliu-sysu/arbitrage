package runtime

import (
	"testing"

	"github.com/brianliu-sysu/uniswapv3/internal/config"
)

func TestNewProtocolServicesSkipsDisabledProtocols(t *testing.T) {
	services, err := newProtocolServices(config.ChainConfig{}, nil, nil, protocolResources{})
	if err != nil {
		t.Fatalf("build disabled protocols: %v", err)
	}
	if len(services.modules) != 0 {
		t.Fatalf("expected no disabled protocol modules, got %d", len(services.modules))
	}
}

func TestNewProtocolResourcesSkipsDisabledProtocols(t *testing.T) {
	services, err := newProtocolResources(config.ChainConfig{}, nil)
	if err != nil {
		t.Fatalf("build disabled protocol infrastructure: %v", err)
	}
	if services.headLogFetcher != nil ||
		services.univ3 != nil ||
		services.pancakeV3 != nil ||
		services.quickSwapV3 != nil ||
		services.univ4 != nil ||
		services.balancer != nil {
		t.Fatal("expected disabled protocol infrastructure to remain nil")
	}
}
