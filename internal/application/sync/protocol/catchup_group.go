package protocol

// CatchupIndexGroup is a batch of catchup tasks sharing a similar start block.
type CatchupIndexGroup struct {
	MinFromBlock uint64
	Indices      []int
}

// GroupCatchupFromBlocks groups sorted fromBlock values for batched catchup.
// fromBlocks must be sorted in ascending order.
func GroupCatchupFromBlocks(fromBlocks []uint64, maxPools, maxBlockSpan uint64) []CatchupIndexGroup {
	if len(fromBlocks) == 0 {
		return nil
	}
	if maxPools == 0 {
		maxPools = 100
	}
	if maxBlockSpan == 0 {
		maxBlockSpan = 100
	}

	groups := make([]CatchupIndexGroup, 0, len(fromBlocks))
	current := CatchupIndexGroup{
		MinFromBlock: fromBlocks[0],
		Indices:      []int{0},
	}

	for i := 1; i < len(fromBlocks); i++ {
		fromBlock := fromBlocks[i]
		span := fromBlock - current.MinFromBlock
		if uint64(len(current.Indices)) >= maxPools || span > maxBlockSpan {
			groups = append(groups, current)
			current = CatchupIndexGroup{
				MinFromBlock: fromBlock,
				Indices:      []int{i},
			}
			continue
		}
		current.Indices = append(current.Indices, i)
	}
	groups = append(groups, current)
	return groups
}
