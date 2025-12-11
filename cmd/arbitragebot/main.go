// Package main is the entry point for the arbitrage bot application.
// It parses command-line flags, loads configuration, and starts the bot.
//
// Usage:
//
//	arbitragebot --config configs/config.yaml
//	arbitragebot --config configs/config.yaml --dry-run
package main

import (
	"flag"
	"fmt"
	"os"

	"arbitragebot/pkg/config"
)

// Command-line flags.
var (
	// configPath is the path to the YAML configuration file.
	configPath string
	// dryRun enables paper trading mode (no real orders).
	dryRun bool
)

// init registers command-line flags.
func init() {
	flag.StringVar(&configPath, "config", "configs/config.yaml", "path to config file")
	flag.BoolVar(&dryRun, "dry-run", false, "run in dry-run mode (paper trading)")
}

// main is the application entry point.
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
