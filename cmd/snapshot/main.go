// snapshot 一次性池子快照工具：从配置读取 pools / auto_discover，链上全量同步 tick 地图并写入 pool_states。
//
// 用法:
//
//	go run ./cmd/snapshot/ -config config.yaml
//	go run ./cmd/snapshot/ -config config.yaml -chain ethereum
package main

import (
	"flag"
	"log"

	snapshotapp "github.com/brianliu-sysu/arbitrage/internal/app/snapshot"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	chainFilter := flag.String("chain", "", "only snapshot this chain name (optional)")
	flag.Parse()

	log.Print("starting snapshot with Uber Fx")
	snapshotapp.New(*configPath, *chainFilter).Run()
}
