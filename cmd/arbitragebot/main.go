package main

import (
	"flag"
	"fmt"
	"os"

	"arbitragebot/pkg/config"
)

var (
	configPath string
	dryRun     bool
)

func init() {
	flag.StringVar(&configPath, "config", "configs/config.yaml", "path to config file")
	flag.BoolVar(&dryRun, "dry-run", false, "run in dry-run mode (paper trading)")
}

func main() {
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Starting %s (env: %s)\n", cfg.App.Name, cfg.App.Env)
	fmt.Printf("Enabled exchanges: %v\n", cfg.EnabledExchanges())
	fmt.Printf("Trading pairs: %v\n", cfg.Pairs)

	if dryRun {
		fmt.Println("Running in dry-run mode (paper trading)")
	}
}
