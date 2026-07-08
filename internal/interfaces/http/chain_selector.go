package httpapi

import (
	"fmt"
	"strconv"
	"strings"
)

// ChainInfo describes one API-selectable chain.
type ChainInfo struct {
	Name    string
	ChainID uint64
	Primary bool
}

type chainSelector struct {
	primary string
	aliases map[string]string
}

func newChainSelector(chains []ChainInfo) chainSelector {
	selector := chainSelector{aliases: make(map[string]string)}
	for _, chain := range chains {
		name := strings.TrimSpace(chain.Name)
		if name == "" {
			name = fmt.Sprintf("chain-%d", chain.ChainID)
		}
		key := normalizeChainKey(name)
		if selector.primary == "" || chain.Primary {
			selector.primary = key
		}
		selector.aliases[key] = key
		if chain.ChainID != 0 {
			selector.aliases[normalizeChainKey(strconv.FormatUint(chain.ChainID, 10))] = key
		}
	}
	return selector
}

func (s chainSelector) selectKey(chain string) (string, bool) {
	key := normalizeChainKey(chain)
	if key == "" {
		key = s.primary
	}
	if key == "" {
		return "", false
	}
	resolved, ok := s.aliases[key]
	return resolved, ok
}

func normalizeChainKey(chain string) string {
	return strings.ToLower(strings.TrimSpace(chain))
}

func chainNotFoundMessage(chain string) string {
	chain = strings.TrimSpace(chain)
	if chain == "" {
		return "default chain is not configured"
	}
	return fmt.Sprintf("chain %q is not configured", chain)
}
