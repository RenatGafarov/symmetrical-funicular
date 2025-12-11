package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	App          AppConfig                 `mapstructure:"app"`
	Exchanges    map[string]ExchangeConfig `mapstructure:"exchanges"`
	Orderbook    *OrderbookConfig          `mapstructure:"orderbook"`
	Arbitrage    *ArbitrageConfig          `mapstructure:"arbitrage"`
	Execution    *ExecutionConfig          `mapstructure:"execution"`
	Risk         *RiskConfig               `mapstructure:"risk"`
	Pairs        []string                  `mapstructure:"pairs"`
	Notification *NotificationConfig       `mapstructure:"notification"`
	Metrics      *MetricsConfig            `mapstructure:"metrics"`
	Server       *ServerConfig             `mapstructure:"server"`
}

type AppConfig struct {
	Name     string `mapstructure:"name"`
	Env      string `mapstructure:"env"`
	LogLevel string `mapstructure:"log_level"`
}

type ExchangeConfig struct {
	Enabled   bool            `mapstructure:"enabled"`
	Testnet   bool            `mapstructure:"testnet"`
	FeeMaker  string          `mapstructure:"fee_maker"`
	FeeTaker  string          `mapstructure:"fee_taker"`
	RateLimit int             `mapstructure:"rate_limit"`
	WebSocket WebSocketConfig `mapstructure:"websocket"`
}

type WebSocketConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	PingInterval   time.Duration `mapstructure:"ping_interval"`
	ReconnectDelay time.Duration `mapstructure:"reconnect_delay"`
}

type OrderbookConfig struct {
	MaxDepth int           `mapstructure:"max_depth"`
	MaxAge   time.Duration `mapstructure:"max_age"`
	Redis    RedisConfig   `mapstructure:"redis"`
}

type RedisConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

type ArbitrageConfig struct {
	CrossExchange *CrossExchangeConfig `mapstructure:"cross_exchange"`
	Triangular    *TriangularConfig    `mapstructure:"triangular"`
}

type CrossExchangeConfig struct {
	Enabled            bool   `mapstructure:"enabled"`
	MinProfitThreshold string `mapstructure:"min_profit_threshold"`
	MaxSlippage        string `mapstructure:"max_slippage"`
	MinVolumeUSD       string `mapstructure:"min_volume_usd"`
}

type TriangularConfig struct {
	Enabled            bool     `mapstructure:"enabled"`
	MinProfitThreshold string   `mapstructure:"min_profit_threshold"`
	MaxPathLength      int      `mapstructure:"max_path_length"`
	Currencies         []string `mapstructure:"currencies"`
}

type ExecutionConfig struct {
	Timeout time.Duration `mapstructure:"timeout"`
	Retry   RetryConfig   `mapstructure:"retry"`
}

type RetryConfig struct {
	MaxAttempts  int           `mapstructure:"max_attempts"`
	InitialDelay time.Duration `mapstructure:"initial_delay"`
	MaxDelay     time.Duration `mapstructure:"max_delay"`
	Multiplier   float64       `mapstructure:"multiplier"`
}

type RiskConfig struct {
	MaxPositionPerExchange string `mapstructure:"max_position_per_exchange"`
	DailyLossLimit         string `mapstructure:"daily_loss_limit"`
	KillSwitchDrawdown     string `mapstructure:"kill_switch_drawdown"`
	MaxOpenOrders          int    `mapstructure:"max_open_orders"`
}

type NotificationConfig struct {
	Telegram TelegramConfig `mapstructure:"telegram"`
}

type TelegramConfig struct {
	Enabled             bool `mapstructure:"enabled"`
	NotifyOpportunities bool `mapstructure:"notify_opportunities"`
	NotifyExecutions    bool `mapstructure:"notify_executions"`
	NotifyErrors        bool `mapstructure:"notify_errors"`
	NotifyDailySummary  bool `mapstructure:"notify_daily_summary"`
}

type MetricsConfig struct {
	Prometheus PrometheusConfig `mapstructure:"prometheus"`
}

type PrometheusConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

type ServerConfig struct {
	HTTP HTTPConfig `mapstructure:"http"`
}

type HTTPConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

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

func (c *Config) IsDevelopment() bool {
	return c.App.Env == "development"
}

func (c *Config) IsProduction() bool {
	return c.App.Env == "production"
}

func (c *Config) EnabledExchanges() []string {
	var exchanges []string
	for name, ex := range c.Exchanges {
		if ex.Enabled {
			exchanges = append(exchanges, name)
		}
	}
	return exchanges
}