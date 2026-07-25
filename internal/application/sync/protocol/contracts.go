package protocol

import synccontract "github.com/brianliu-sysu/uniswapv3/internal/application/sync/contract"

type Config = synccontract.Config
type RawLog = synccontract.RawLog
type BlockReader = synccontract.BlockReader
type BlockPreparer = synccontract.BlockPreparer
type PreparedBlock = synccontract.PreparedBlock
type PreparedReorg = synccontract.PreparedReorg

var DefaultConfig = synccontract.DefaultConfig
