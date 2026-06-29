package main

import (
	"flag"
	"log"

	"github.com/brianliu-sysu/arbitrage/internal/app"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML config file")
	flag.Parse()

	log.Print("starting arbitrage service with Uber Fx")
	app.New(*configPath).Run()
}
