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
	"log"
	"os"

	"arbitragebot/pkg/config"

	"github.com/joho/godotenv"
)

// Command-line flags.
var (
	// configPath is the path to the YAML configuration file.
	configPath string
	// dryRun enables paper trading mode (no real orders).
	dryRun bool
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Fatalln("⚠️  No .env file found, using system environment variables")
	} else {
		log.Println("✅ .env file loaded successfully")
	}
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
