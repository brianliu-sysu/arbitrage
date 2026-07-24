package syncapp

import syncrunner "github.com/brianliu-sysu/uniswapv3/internal/application/sync/runner"

type PreparedBlock = syncrunner.PreparedBlock
type BlockPreparer = syncrunner.BlockPreparer
type BlockHandler = syncrunner.BlockHandler
type HeadLogFetcher = syncrunner.HeadLogFetcher
type HeadCoordinator = syncrunner.HeadCoordinator
type BlockBatchReader = syncrunner.BlockBatchReader
type CanonicalBlockReader = syncrunner.CanonicalBlockReader
type NamedHeadHandler = syncrunner.NamedHeadHandler
type SharedHeadRunner = syncrunner.SharedHeadRunner
type SharedHeadDependencies = syncrunner.SharedHeadDependencies
type MarketBlockProcessor = syncrunner.MarketBlockProcessor

var NewSharedHeadRunner = syncrunner.NewSharedHeadRunner
var NewMarketBlockProcessor = syncrunner.NewMarketBlockProcessor
