package runner

import (
	synccontract "github.com/brianliu-sysu/uniswapv3/internal/application/sync/contract"
	syncprotocol "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
)

type Config = synccontract.Config
type RawLog = synccontract.RawLog
type HeadSubscriber = synccontract.HeadSubscriber
type BlockBatchReader = synccontract.BlockBatchReader
type CanonicalBlockReader = synccontract.CanonicalBlockReader
type BlockHandler = synccontract.BlockHandler
type HeadLogFetcher = synccontract.HeadLogFetcher
type NamedHeadHandler = synccontract.NamedHeadHandler
type PreparedBlock = synccontract.PreparedBlock
type PreparedReorg = synccontract.PreparedReorg
type BlockPreparer = synccontract.BlockPreparer
type ReorgPreparer = synccontract.ReorgPreparer
type SyncStartup = syncprotocol.SyncStartup

var DefaultConfig = synccontract.DefaultConfig
var ShouldSkipHeadNotification = syncprotocol.ShouldSkipHeadNotification
var NeedsHeadGapCatchup = syncprotocol.NeedsHeadGapCatchup
var RunStartupAt = syncprotocol.RunStartupAt
