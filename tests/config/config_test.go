package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"arbitragebot/pkg/config"
)

func createTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp config: %v", err)
	}
	return path
}

// setExchangeEnvVars sets API credentials for exchanges in tests.
// Returns a cleanup function that restores original values.
func setExchangeEnvVars(t *testing.T, exchanges ...string) func() {
	t.Helper()
	original := make(map[string]string)

	for _, ex := range exchanges {
		keyVar := ex + "_API_KEY"
		secretVar := ex + "_API_SECRET"

		original[keyVar] = os.Getenv(keyVar)
		original[secretVar] = os.Getenv(secretVar)

		os.Setenv(keyVar, "test-api-key")
		os.Setenv(secretVar, "test-api-secret")
	}

	return func() {
		for k, v := range original {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}
}

func TestLoad_FullConfig(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: testbot
  env: development
  log_level: debug

exchanges:
  binance:
    enabled: true
    testnet: false
    fee_maker: "0.0002"
    fee_taker: "0.0004"
    rate_limit: 1200
    websocket:
      enabled: true
      ping_interval: 30s
      reconnect_delay: 5s

orderbook:
  max_depth: 20
  max_age: 100ms
  redis:
    enabled: true
    addr: localhost:6379
    db: 0
    pool_size: 10

arbitrage:
  cross_exchange:
    enabled: true
    min_profit_threshold: "0.003"
    max_slippage: "0.01"
    min_volume_usd: "100"
  triangular:
    enabled: true
    min_profit_threshold: "0.002"
    max_path_length: 3
    currencies:
      - BTC
      - ETH
      - USDT

execution:
  timeout: 5s
  retry:
    max_attempts: 3
    initial_delay: 100ms
    max_delay: 1s
    multiplier: 2.0

risk:
  max_position_per_exchange: "0.20"
  daily_loss_limit: "0.05"
  kill_switch_drawdown: "0.05"
  max_open_orders: 10

pairs:
  - BTC/USDT
  - ETH/USDT

notification:
  telegram:
    enabled: true
    notify_opportunities: false
    notify_executions: true
    notify_errors: true
    notify_daily_summary: true

metrics:
  prometheus:
    enabled: true
    port: 9090
    path: /metrics

server:
  http:
    port: 8080
    read_timeout: 10s
    write_timeout: 10s
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// App
	if cfg.App.Name != "testbot" {
		t.Errorf("app.name = %q, want %q", cfg.App.Name, "testbot")
	}
	if cfg.App.Env != "development" {
		t.Errorf("app.env = %q, want %q", cfg.App.Env, "development")
	}
	if cfg.App.LogLevel != "debug" {
		t.Errorf("app.log_level = %q, want %q", cfg.App.LogLevel, "debug")
	}

	// Exchanges
	if len(cfg.Exchanges) != 1 {
		t.Errorf("exchanges count = %d, want 1", len(cfg.Exchanges))
	}
	binance, ok := cfg.Exchanges["binance"]
	if !ok {
		t.Fatal("binance exchange not found")
	}
	if !binance.Enabled {
		t.Error("binance.enabled = false, want true")
	}
	if binance.FeeMaker != "0.0002" {
		t.Errorf("binance.fee_maker = %q, want %q", binance.FeeMaker, "0.0002")
	}
	if binance.RateLimit != 1200 {
		t.Errorf("binance.rate_limit = %d, want 1200", binance.RateLimit)
	}
	if !binance.WebSocket.Enabled {
		t.Error("binance.websocket.enabled = false, want true")
	}

	// Orderbook
	if cfg.Orderbook == nil {
		t.Fatal("orderbook is nil")
	}
	if cfg.Orderbook.MaxDepth != 20 {
		t.Errorf("orderbook.max_depth = %d, want 20", cfg.Orderbook.MaxDepth)
	}
	if !cfg.Orderbook.Redis.Enabled {
		t.Error("orderbook.redis.enabled = false, want true")
	}

	// Arbitrage
	if cfg.Arbitrage == nil {
		t.Fatal("arbitrage is nil")
	}
	if cfg.Arbitrage.CrossExchange == nil {
		t.Fatal("arbitrage.cross_exchange is nil")
	}
	if !cfg.Arbitrage.CrossExchange.Enabled {
		t.Error("arbitrage.cross_exchange.enabled = false, want true")
	}
	if cfg.Arbitrage.CrossExchange.MinProfitThreshold != "0.003" {
		t.Errorf("arbitrage.cross_exchange.min_profit_threshold = %q, want %q",
			cfg.Arbitrage.CrossExchange.MinProfitThreshold, "0.003")
	}
	if cfg.Arbitrage.Triangular == nil {
		t.Fatal("arbitrage.triangular is nil")
	}
	if len(cfg.Arbitrage.Triangular.Currencies) != 3 {
		t.Errorf("arbitrage.triangular.currencies count = %d, want 3",
			len(cfg.Arbitrage.Triangular.Currencies))
	}

	// Execution
	if cfg.Execution == nil {
		t.Fatal("execution is nil")
	}
	if cfg.Execution.Retry.MaxAttempts != 3 {
		t.Errorf("execution.retry.max_attempts = %d, want 3", cfg.Execution.Retry.MaxAttempts)
	}
	if cfg.Execution.Retry.Multiplier != 2.0 {
		t.Errorf("execution.retry.multiplier = %f, want 2.0", cfg.Execution.Retry.Multiplier)
	}

	// Risk
	if cfg.Risk == nil {
		t.Fatal("risk is nil")
	}
	if cfg.Risk.MaxOpenOrders != 10 {
		t.Errorf("risk.max_open_orders = %d, want 10", cfg.Risk.MaxOpenOrders)
	}

	// Pairs
	if len(cfg.Pairs) != 2 {
		t.Errorf("pairs count = %d, want 2", len(cfg.Pairs))
	}

	// Notification
	if cfg.Notification == nil {
		t.Fatal("notification is nil")
	}
	if !cfg.Notification.Telegram.Enabled {
		t.Error("notification.telegram.enabled = false, want true")
	}

	// Metrics
	if cfg.Metrics == nil {
		t.Fatal("metrics is nil")
	}
	if cfg.Metrics.Prometheus.Port != 9090 {
		t.Errorf("metrics.prometheus.port = %d, want 9090", cfg.Metrics.Prometheus.Port)
	}

	// Server
	if cfg.Server == nil {
		t.Fatal("server is nil")
	}
	if cfg.Server.HTTP.Port != 8080 {
		t.Errorf("server.http.port = %d, want 8080", cfg.Server.HTTP.Port)
	}
}

func TestLoad_MinimalConfig(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: minimalbot
  env: production

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.App.Name != "minimalbot" {
		t.Errorf("app.name = %q, want %q", cfg.App.Name, "minimalbot")
	}

	// Optional sections should be nil
	if cfg.Orderbook != nil {
		t.Error("orderbook should be nil")
	}
	if cfg.Arbitrage != nil {
		t.Error("arbitrage should be nil")
	}
	if cfg.Execution != nil {
		t.Error("execution should be nil")
	}
	if cfg.Risk != nil {
		t.Error("risk should be nil")
	}
	if cfg.Notification != nil {
		t.Error("notification should be nil")
	}
	if cfg.Metrics != nil {
		t.Error("metrics should be nil")
	}
	if cfg.Server != nil {
		t.Error("server should be nil")
	}
}

func TestLoad_PartialArbitrage(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: partialbot
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

arbitrage:
  cross_exchange:
    enabled: true
    min_profit_threshold: "0.003"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Arbitrage == nil {
		t.Fatal("arbitrage should not be nil")
	}
	if cfg.Arbitrage.CrossExchange == nil {
		t.Fatal("arbitrage.cross_exchange should not be nil")
	}
	if cfg.Arbitrage.Triangular != nil {
		t.Error("arbitrage.triangular should be nil")
	}
}

func TestLoad_PartialOrderbook(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: orderbookbot
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

orderbook:
  max_depth: 50
  max_age: 200ms

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.Orderbook == nil {
		t.Fatal("orderbook should not be nil")
	}
	if cfg.Orderbook.MaxDepth != 50 {
		t.Errorf("orderbook.max_depth = %d, want 50", cfg.Orderbook.MaxDepth)
	}
	// Redis.Enabled defaults to false (zero value)
	assert.False(t, cfg.Orderbook.Redis.Enabled)
}

func TestLoad_ValidationError_MissingAppName(t *testing.T) {
	t.Parallel()

	content := `
app:
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing app.name")
	}
}

func TestLoad_ValidationError_NoPairs(t *testing.T) {
	t.Parallel()

	content := `
app:
  name: nopairstest
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs: []
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for empty pairs")
	}
}

func TestLoad_ValidationError_NoEnabledExchanges(t *testing.T) {
	t.Parallel()

	content := `
app:
  name: noexchangetest
  env: development

exchanges:
  binance:
    enabled: false
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for no enabled exchanges")
	}
}

func TestLoad_ValidationError_MissingFees(t *testing.T) {
	t.Parallel()

	content := `
app:
  name: nofeetest
  env: development

exchanges:
  binance:
    enabled: true
    rate_limit: 1200

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for missing fees")
	}
}

func TestLoad_ValidationError_InvalidRiskMaxOpenOrders(t *testing.T) {
	t.Parallel()

	content := `
app:
  name: risktest
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

risk:
  max_open_orders: 0

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid risk.max_open_orders")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	t.Parallel()

	content := `
app:
  name: [invalid yaml
  env: development
`

	path := createTempConfig(t, content)
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestConfig_IsDevelopment(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: devtest
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if !cfg.IsDevelopment() {
		t.Error("IsDevelopment() = false, want true")
	}
	if cfg.IsProduction() {
		t.Error("IsProduction() = true, want false")
	}
}

func TestConfig_IsProduction(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: prodtest
  env: production

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if cfg.IsDevelopment() {
		t.Error("IsDevelopment() = true, want false")
	}
	if !cfg.IsProduction() {
		t.Error("IsProduction() = false, want true")
	}
}

func TestConfig_EnabledExchanges(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE", "BYBIT")
	defer cleanup()

	content := `
app:
  name: exchangetest
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"
  bybit:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"
  kraken:
    enabled: false
    fee_maker: "0.001"
    fee_taker: "0.001"

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	enabled := cfg.EnabledExchanges()
	if len(enabled) != 2 {
		t.Errorf("EnabledExchanges() count = %d, want 2", len(enabled))
	}

	enabledMap := make(map[string]bool)
	for _, e := range enabled {
		enabledMap[e] = true
	}

	if !enabledMap["binance"] {
		t.Error("binance should be in enabled exchanges")
	}
	if !enabledMap["bybit"] {
		t.Error("bybit should be in enabled exchanges")
	}
	if enabledMap["kraken"] {
		t.Error("kraken should not be in enabled exchanges")
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE")
	defer cleanup()

	content := `
app:
  name: durationtest
  env: development

exchanges:
  binance:
    enabled: true
    fee_maker: "0.001"
    fee_taker: "0.001"
    websocket:
      enabled: true
      ping_interval: 45s
      reconnect_delay: 10s

orderbook:
  max_depth: 20
  max_age: 150ms

execution:
  timeout: 3s
  retry:
    max_attempts: 5
    initial_delay: 50ms
    max_delay: 2s
    multiplier: 1.5

server:
  http:
    port: 8080
    read_timeout: 15s
    write_timeout: 20s

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// WebSocket durations
	binance := cfg.Exchanges["binance"]
	if binance.WebSocket.PingInterval.Seconds() != 45 {
		t.Errorf("websocket.ping_interval = %v, want 45s", binance.WebSocket.PingInterval)
	}
	if binance.WebSocket.ReconnectDelay.Seconds() != 10 {
		t.Errorf("websocket.reconnect_delay = %v, want 10s", binance.WebSocket.ReconnectDelay)
	}

	// Orderbook duration
	if cfg.Orderbook.MaxAge.Milliseconds() != 150 {
		t.Errorf("orderbook.max_age = %v, want 150ms", cfg.Orderbook.MaxAge)
	}

	// Execution durations
	if cfg.Execution.Timeout.Seconds() != 3 {
		t.Errorf("execution.timeout = %v, want 3s", cfg.Execution.Timeout)
	}
	if cfg.Execution.Retry.InitialDelay.Milliseconds() != 50 {
		t.Errorf("execution.retry.initial_delay = %v, want 50ms", cfg.Execution.Retry.InitialDelay)
	}

	// Server durations
	if cfg.Server.HTTP.ReadTimeout.Seconds() != 15 {
		t.Errorf("server.http.read_timeout = %v, want 15s", cfg.Server.HTTP.ReadTimeout)
	}
	if cfg.Server.HTTP.WriteTimeout.Seconds() != 20 {
		t.Errorf("server.http.write_timeout = %v, want 20s", cfg.Server.HTTP.WriteTimeout)
	}
}

func TestLoad_MultipleExchanges(t *testing.T) {
	cleanup := setExchangeEnvVars(t, "BINANCE", "BYBIT")
	defer cleanup()

	content := `
app:
  name: multiexchange
  env: development

exchanges:
  binance:
    enabled: true
    testnet: false
    fee_maker: "0.0002"
    fee_taker: "0.0004"
    rate_limit: 1200
  bybit:
    enabled: true
    testnet: true
    fee_maker: "0.0001"
    fee_taker: "0.0006"
    rate_limit: 600
  kraken:
    enabled: false
    fee_maker: "0.0016"
    fee_taker: "0.0026"
    rate_limit: 300

pairs:
  - BTC/USDT
`

	path := createTempConfig(t, content)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if len(cfg.Exchanges) != 3 {
		t.Errorf("exchanges count = %d, want 3", len(cfg.Exchanges))
	}

	binance := cfg.Exchanges["binance"]
	if binance.Testnet {
		t.Error("binance.testnet = true, want false")
	}
	if binance.FeeMaker != "0.0002" {
		t.Errorf("binance.fee_maker = %q, want %q", binance.FeeMaker, "0.0002")
	}

	bybit := cfg.Exchanges["bybit"]
	if !bybit.Testnet {
		t.Error("bybit.testnet = false, want true")
	}
	if bybit.RateLimit != 600 {
		t.Errorf("bybit.rate_limit = %d, want 600", bybit.RateLimit)
	}

	kraken := cfg.Exchanges["kraken"]
	if kraken.Enabled {
		t.Error("kraken.enabled = true, want false")
	}
}