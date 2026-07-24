package syncapp

import synccontract "github.com/brianliu-sysu/uniswapv3/internal/application/sync/contract"

// Config holds sync-related runtime settings shared by V3 and V4.
type Config = synccontract.Config

var DefaultConfig = synccontract.DefaultConfig

// RawLog is a decoded-free log entry fetched from the chain.
type RawLog = synccontract.RawLog

// HeadSubscriber delivers new canonical block headers.
type HeadSubscriber = synccontract.HeadSubscriber

// BlockReader reads block headers for catchup and reorg recovery.
type BlockReader = synccontract.BlockReader
