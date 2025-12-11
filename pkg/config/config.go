// Package config provides configuration loading and validation for the arbitrage bot.
// It uses Viper to load YAML configuration files with support for environment variable overrides.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config is the root configuration structure for the arbitrage bot.
// Required sections: App, Exchanges, Pairs.
// Optional sections (nil if not specified): Orderbook, Arbitrage, Execution, Risk, Notification, Metrics, Server.
type Config struct {
	// App contains application-level settings like name and environment.
	App AppConfig `mapstructure:"app"`
	// Exchanges maps exchange names to their configurations.
	Exchanges map[string]ExchangeConfig `mapstructure:"exchanges"`
	// Orderbook configures orderbook caching and staleness (optional).
	Orderbook *OrderbookConfig `mapstructure:"orderbook"`
	// Arbitrage configures arbitrage detection strategies (optional).
	Arbitrage *ArbitrageConfig `mapstructure:"arbitrage"`
	// Execution configures order execution timeouts and retries (optional).
	Execution *ExecutionConfig `mapstructure:"execution"`
	// Risk configures risk management limits (optional).
	Risk *RiskConfig `mapstructure:"risk"`
	// Pairs is the list of trading pairs to monitor (e.g., "BTC/USDT").
	Pairs []string `mapstructure:"pairs"`
	// Notification configures alert channels like Telegram (optional).
	Notification *NotificationConfig `mapstructure:"notification"`
	// Metrics configures Prometheus metrics endpoint (optional).
	Metrics *MetricsConfig `mapstructure:"metrics"`
	// Server configures the HTTP server (optional).
	Server *ServerConfig `mapstructure:"server"`
}

// AppConfig contains application-level settings.
type AppConfig struct {
	// Name is the application name used in logs and metrics.
	Name string `mapstructure:"name"`
	// Env is the environment: "development", "staging", or "production".
	Env string `mapstructure:"env"`
	// LogLevel sets logging verbosity: "debug", "info", "warn", "error".
	LogLevel string `mapstructure:"log_level"`
}

// ExchangeConfig contains settings for a single exchange.
type ExchangeConfig struct {
	// Enabled determines if this exchange should be used.
	Enabled bool `mapstructure:"enabled"`
	// Testnet enables testnet/sandbox mode for the exchange.
	Testnet bool `mapstructure:"testnet"`
	// FeeMaker is the maker fee as a decimal string (e.g., "0.001" for 0.1%).
	FeeMaker string `mapstructure:"fee_maker"`
	// FeeTaker is the taker fee as a decimal string (e.g., "0.001" for 0.1%).
	FeeTaker string `mapstructure:"fee_taker"`
	// RateLimit is the maximum API requests per minute.
	RateLimit int `mapstructure:"rate_limit"`
	// WebSocket configures WebSocket connection settings.
	WebSocket WebSocketConfig `mapstructure:"websocket"`
}

// WebSocketConfig contains WebSocket connection settings.
type WebSocketConfig struct {
	// Enabled determines if WebSocket should be used for real-time data.
	Enabled bool `mapstructure:"enabled"`
	// PingInterval is the interval between ping messages to keep connection alive.
	PingInterval time.Duration `mapstructure:"ping_interval"`
	// ReconnectDelay is the delay before attempting to reconnect after disconnection.
	ReconnectDelay time.Duration `mapstructure:"reconnect_delay"`
}

// OrderbookConfig contains orderbook caching settings.
type OrderbookConfig struct {
	// MaxDepth is the maximum number of price levels to store per side.
	MaxDepth int `mapstructure:"max_depth"`
	// MaxAge is the maximum age of orderbook data before it's considered stale.
	MaxAge time.Duration `mapstructure:"max_age"`
	// Redis configures Redis for shared orderbook caching.
	Redis RedisConfig `mapstructure:"redis"`
}

// RedisConfig contains Redis connection settings.
type RedisConfig struct {
	// Enabled determines if Redis should be used for caching.
	Enabled bool `mapstructure:"enabled"`
	// Addr is the Redis server address (e.g., "localhost:6379").
	Addr string `mapstructure:"addr"`
	// DB is the Redis database number (0-15).
	DB int `mapstructure:"db"`
	// PoolSize is the maximum number of connections in the pool.
	PoolSize int `mapstructure:"pool_size"`
}

// ArbitrageConfig contains arbitrage detection settings.
type ArbitrageConfig struct {
	// CrossExchange configures cross-exchange arbitrage detection (optional).
	CrossExchange *CrossExchangeConfig `mapstructure:"cross_exchange"`
	// Triangular configures triangular arbitrage detection (optional).
	Triangular *TriangularConfig `mapstructure:"triangular"`
}

// CrossExchangeConfig contains cross-exchange arbitrage settings.
type CrossExchangeConfig struct {
	// Enabled determines if cross-exchange arbitrage detection is active.
	Enabled bool `mapstructure:"enabled"`
	// MinProfitThreshold is the minimum profit percentage to trigger (e.g., "0.003" for 0.3%).
	MinProfitThreshold string `mapstructure:"min_profit_threshold"`
	// MaxSlippage is the maximum allowed slippage (e.g., "0.01" for 1%).
	MaxSlippage string `mapstructure:"max_slippage"`
	// MinVolumeUSD is the minimum trade volume in USD.
	MinVolumeUSD string `mapstructure:"min_volume_usd"`
}

// TriangularConfig contains triangular arbitrage settings.
type TriangularConfig struct {
	// Enabled determines if triangular arbitrage detection is active.
	Enabled bool `mapstructure:"enabled"`
	// MinProfitThreshold is the minimum profit percentage to trigger (e.g., "0.002" for 0.2%).
	MinProfitThreshold string `mapstructure:"min_profit_threshold"`
	// MaxPathLength is the maximum number of trades in a cycle (typically 3).
	MaxPathLength int `mapstructure:"max_path_length"`
	// Currencies is the list of currencies to include in triangular detection.
	Currencies []string `mapstructure:"currencies"`
}

// ExecutionConfig contains order execution settings.
type ExecutionConfig struct {
	// Timeout is the maximum time to wait for order execution.
	Timeout time.Duration `mapstructure:"timeout"`
	// Retry configures retry behavior for failed orders.
	Retry RetryConfig `mapstructure:"retry"`
}

// RetryConfig contains retry settings for failed operations.
type RetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts.
	MaxAttempts int `mapstructure:"max_attempts"`
	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration `mapstructure:"max_delay"`
	// Multiplier is the factor by which delay increases after each retry.
	Multiplier float64 `mapstructure:"multiplier"`
}

// RiskConfig contains risk management settings.
type RiskConfig struct {
	// MaxPositionPerExchange is the maximum position size per exchange as a decimal (e.g., "0.20" for 20%).
	MaxPositionPerExchange string `mapstructure:"max_position_per_exchange"`
	// DailyLossLimit is the maximum daily loss before stopping trading (e.g., "0.05" for 5%).
	DailyLossLimit string `mapstructure:"daily_loss_limit"`
	// KillSwitchDrawdown is the drawdown threshold that triggers emergency stop (e.g., "0.05" for 5%).
	KillSwitchDrawdown string `mapstructure:"kill_switch_drawdown"`
	// MaxOpenOrders is the maximum number of open orders allowed.
	MaxOpenOrders int `mapstructure:"max_open_orders"`
}

// NotificationConfig contains notification settings.
type NotificationConfig struct {
	// Telegram configures Telegram bot notifications.
	Telegram TelegramConfig `mapstructure:"telegram"`
}

// TelegramConfig contains Telegram notification settings.
type TelegramConfig struct {
	// Enabled determines if Telegram notifications are active.
	Enabled bool `mapstructure:"enabled"`
	// NotifyOpportunities sends alerts when arbitrage opportunities are detected.
	NotifyOpportunities bool `mapstructure:"notify_opportunities"`
	// NotifyExecutions sends alerts when trades are executed.
	NotifyExecutions bool `mapstructure:"notify_executions"`
	// NotifyErrors sends alerts when errors occur.
	NotifyErrors bool `mapstructure:"notify_errors"`
	// NotifyDailySummary sends a daily summary of trading activity.
	NotifyDailySummary bool `mapstructure:"notify_daily_summary"`
}

// MetricsConfig contains metrics settings.
type MetricsConfig struct {
	// Prometheus configures Prometheus metrics endpoint.
	Prometheus PrometheusConfig `mapstructure:"prometheus"`
}

// PrometheusConfig contains Prometheus metrics settings.
type PrometheusConfig struct {
	// Enabled determines if Prometheus metrics endpoint is active.
	Enabled bool `mapstructure:"enabled"`
	// Port is the port to expose metrics on.
	Port int `mapstructure:"port"`
	// Path is the HTTP path for metrics (e.g., "/metrics").
	Path string `mapstructure:"path"`
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	// HTTP configures the HTTP server.
	HTTP HTTPConfig `mapstructure:"http"`
}

// HTTPConfig contains HTTP server settings.
type HTTPConfig struct {
	// Port is the port to listen on.
	Port int `mapstructure:"port"`
	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// Load reads configuration from a YAML file at the given path.
// It also supports environment variable overrides with the ARBITRAGE_ prefix.
// Returns an error if the file cannot be read, parsed, or fails validation.
func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	v.SetEnvPrefix("ARBITRAGE")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// Validate checks that the configuration is valid.
// Returns an error if required fields are missing or have invalid values.
func (c *Config) Validate() error {
	if c.App.Name == "" {
		return fmt.Errorf("app.name is required")
	}

	if len(c.Pairs) == 0 {
		return fmt.Errorf("at least one trading pair is required")
	}

	enabledExchanges := 0
	for name, ex := range c.Exchanges {
		if ex.Enabled {
			enabledExchanges++
			if ex.FeeMaker == "" || ex.FeeTaker == "" {
				return fmt.Errorf("exchange %s: fee_maker and fee_taker are required", name)
			}
		}
	}

	if enabledExchanges == 0 {
		return fmt.Errorf("at least one exchange must be enabled")
	}

	if c.Risk != nil && c.Risk.MaxOpenOrders <= 0 {
		return fmt.Errorf("risk.max_open_orders must be positive")
	}

	return nil
}

// IsDevelopment returns true if the environment is "development".
func (c *Config) IsDevelopment() bool {
	return c.App.Env == "development"
}

// IsProduction returns true if the environment is "production".
func (c *Config) IsProduction() bool {
	return c.App.Env == "production"
}

// EnabledExchanges returns a list of exchange names that are enabled.
func (c *Config) EnabledExchanges() []string {
	var exchanges []string
	for name, ex := range c.Exchanges {
		if ex.Enabled {
			exchanges = append(exchanges, name)
		}
	}
	return exchanges
}
