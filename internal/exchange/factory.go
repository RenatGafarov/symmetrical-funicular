package exchange

import (
	"fmt"
	"os"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"arbitragebot/internal/exchange/binance"
	"arbitragebot/internal/exchange/bybit"
	"arbitragebot/pkg/config"
)

// binanceWrapper wraps the Binance adapter to implement the Exchange interface.
type binanceWrapper struct {
	*binance.Adapter
}

func (w *binanceWrapper) GetFees(pair string) Fees {
	f := w.Adapter.GetFees(pair)
	return Fees{Maker: f.Maker, Taker: f.Taker}
}

// bybitWrapper wraps the Bybit adapter to implement the Exchange interface.
type bybitWrapper struct {
	*bybit.Adapter
}

func (w *bybitWrapper) GetFees(pair string) Fees {
	f := w.Adapter.GetFees(pair)
	return Fees{Maker: f.Maker, Taker: f.Taker}
}

// Ensure wrappers implement Exchange interface
var _ Exchange = (*binanceWrapper)(nil)
var _ Exchange = (*bybitWrapper)(nil)

// NewExchange creates an exchange adapter based on the exchange name and configuration.
// It reads API credentials from environment variables:
// - BINANCE_API_KEY, BINANCE_API_SECRET for Binance
// - BYBIT_API_KEY, BYBIT_API_SECRET for Bybit
func NewExchange(name string, cfg *config.ExchangeConfig, pairs []string, logger *zap.Logger) (Exchange, error) {
	switch name {
	case "binance":
		return newBinanceAdapter(cfg, pairs, logger)
	case "bybit":
		return newBybitAdapter(cfg, pairs, logger)
	default:
		return nil, fmt.Errorf("unknown exchange: %s", name)
	}
}

// newBinanceAdapter creates a Binance adapter from configuration.
func newBinanceAdapter(cfg *config.ExchangeConfig, pairs []string, logger *zap.Logger) (Exchange, error) {
	feeMaker, err := decimal.NewFromString(cfg.FeeMaker)
	if err != nil {
		return nil, fmt.Errorf("parse fee_maker: %w", err)
	}
	feeTaker, err := decimal.NewFromString(cfg.FeeTaker)
	if err != nil {
		return nil, fmt.Errorf("parse fee_taker: %w", err)
	}

	adapter := binance.NewAdapter(binance.Config{
		APIKey:           os.Getenv("BINANCE_API_KEY"),
		APISecret:        os.Getenv("BINANCE_API_SECRET"),
		Testnet:          cfg.Testnet,
		FeeMaker:         feeMaker,
		FeeTaker:         feeTaker,
		RateLimit:        cfg.RateLimit,
		Pairs:            pairs,
		WebSocketEnabled: cfg.WebSocket.Enabled,
		PingInterval:     cfg.WebSocket.PingInterval,
		ReconnectDelay:   cfg.WebSocket.ReconnectDelay,
		Logger:           logger,
	})

	return &binanceWrapper{Adapter: adapter}, nil
}

// newBybitAdapter creates a Bybit adapter from configuration.
func newBybitAdapter(cfg *config.ExchangeConfig, pairs []string, logger *zap.Logger) (Exchange, error) {
	feeMaker, err := decimal.NewFromString(cfg.FeeMaker)
	if err != nil {
		return nil, fmt.Errorf("parse fee_maker: %w", err)
	}
	feeTaker, err := decimal.NewFromString(cfg.FeeTaker)
	if err != nil {
		return nil, fmt.Errorf("parse fee_taker: %w", err)
	}

	adapter := bybit.NewAdapter(bybit.Config{
		APIKey:           os.Getenv("BYBIT_API_KEY"),
		APISecret:        os.Getenv("BYBIT_API_SECRET"),
		Testnet:          cfg.Testnet,
		FeeMaker:         feeMaker,
		FeeTaker:         feeTaker,
		RateLimit:        cfg.RateLimit,
		Pairs:            pairs,
		WebSocketEnabled: cfg.WebSocket.Enabled,
		PingInterval:     cfg.WebSocket.PingInterval,
		ReconnectDelay:   cfg.WebSocket.ReconnectDelay,
		Logger:           logger,
	})

	return &bybitWrapper{Adapter: adapter}, nil
}

// CreateExchangesFromConfig creates and registers all enabled exchanges from configuration.
func CreateExchangesFromConfig(cfg *config.Config, logger *zap.Logger) (*Manager, error) {
	manager := NewManager(logger)

	for name, exCfg := range cfg.Exchanges {
		if !exCfg.Enabled {
			logger.Info("exchange disabled, skipping", zap.String("exchange", name))
			continue
		}

		ex, err := NewExchange(name, &exCfg, cfg.Pairs, logger)
		if err != nil {
			return nil, fmt.Errorf("create exchange %s: %w", name, err)
		}

		if err := manager.Register(ex); err != nil {
			return nil, fmt.Errorf("register exchange %s: %w", name, err)
		}

		logger.Info("exchange created", zap.String("exchange", name))
	}

	return manager, nil
}