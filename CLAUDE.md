# ArbitrageBot - Project Context for Claude Code

## Project Overview

**Name:** ArbitrageBot
**Type:** High-frequency cryptocurrency arbitrage bot
**Language:** Go 1.21+
**Purpose:** Automated detection and execution of arbitrage opportunities across cryptocurrency exchanges

### Arbitrage Types

1. **Cross-exchange arbitrage** - Buy on one exchange, sell on another
2. **Triangular arbitrage** - Cyclic trades within a single exchange (BTC→ETH→USDT→BTC)

### Business Goals

- Detect arbitrage opportunities with latency < 50ms
- Execute trades atomically with rollback capability
- Maintain strict risk management with automatic kill-switch
- Achieve consistent profits with minimal drawdown

---

## Technology Stack

### Core Dependencies

```go
require (
    github.com/thrasher-corp/gocryptotrader v0.0.0  // Universal exchange library
    github.com/shopspring/decimal v1.3.1             // Precise decimal arithmetic
    github.com/go-redis/redis/v9 v9.0.0              // Caching and shared state
    golang.org/x/sync v0.0.0                         // errgroup for parallel execution
    github.com/prometheus/client_golang v1.16.0     // Metrics
    go.uber.org/zap v1.26.0                          // Structured logging
    github.com/spf13/viper v1.17.0                   // Configuration
)
```

### Why These Choices

- **gocryptotrader**: Battle-tested library with unified API for 30+ exchanges
- **decimal**: Avoids floating-point errors critical for financial calculations
- **Redis**: Sub-millisecond access for orderbook sharing between processes
- **errgroup**: Clean parallel execution with error propagation

---

## Project Architecture

```
arbitragebot/
├── cmd/
│   └── arbitrage/
│       └── main.go                    # Application entry point
├── internal/
│   ├── domain/                        # Domain models and business rules
│   │   ├── order.go                   # Order, Trade entities
│   │   ├── orderbook.go               # Orderbook model
│   │   ├── opportunity.go             # Arbitrage opportunity model
│   │   └── balance.go                 # Balance model
│   ├── exchange/                      # Exchange adapters layer
│   │   ├── interface.go               # Exchange interface definition
│   │   ├── manager.go                 # MultiExchangeManager
│   │   ├── binance/
│   │   │   ├── adapter.go             # Binance implementation
│   │   │   └── websocket.go           # Binance WebSocket handler
│   │   └── bybit/
│   │       ├── adapter.go             # Bybit implementation
│   │       └── websocket.go           # Bybit WebSocket handler
│   ├── orderbook/                     # Orderbook management
│   │   ├── cache.go                   # Thread-safe orderbook cache
│   │   ├── hub.go                     # WebSocket hub for streams
│   │   └── validator.go               # Data freshness validation
│   ├── detector/                      # Arbitrage detection
│   │   ├── interface.go               # Detector interface
│   │   ├── cross_exchange.go          # Cross-exchange detector
│   │   ├── triangular.go              # Triangular detector (Bellman-Ford)
│   │   └── calculator.go              # Profit calculation with fees
│   ├── executor/                      # Trade execution
│   │   ├── engine.go                  # Parallel execution engine
│   │   ├── rollback.go                # Rollback strategy
│   │   └── retry.go                   # Retry with exponential backoff
│   ├── risk/                          # Risk management
│   │   ├── manager.go                 # Risk manager implementation
│   │   ├── limits.go                  # Position and loss limits
│   │   └── killswitch.go              # Emergency stop mechanism
│   └── balance/                       # Balance management
│       ├── tracker.go                 # Real-time balance tracking
│       └── rebalancer.go              # Cross-exchange rebalancing
├── pkg/                               # Shared packages (can be imported externally)
│   ├── notification/
│   │   └── telegram.go                # Telegram alerts
│   ├── logger/
│   │   └── logger.go                  # Structured logging setup
│   ├── metrics/
│   │   └── prometheus.go              # Prometheus metrics registry
│   └── config/
│       └── config.go                  # Configuration loader
├── configs/
│   ├── config.yaml                    # Main configuration
│   └── config.example.yaml            # Example configuration
├── scripts/
│   ├── setup.sh                       # Environment setup
│   └── deploy.sh                      # Deployment script
├── tests/                             # All tests (external to packages)
│   ├── config/                        # Config package tests
│   ├── integration/                   # Integration tests
│   ├── e2e/                           # End-to-end tests
│   └── mocks/                         # Generated mocks
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── docker-compose.yaml
└── CLAUDE.md
```

---

## Component Details

### 1. Exchange Manager (`internal/exchange/manager.go`)

Manages connections to multiple exchanges with unified interface.

**Responsibilities:**
- Initialize and maintain exchange connections
- Health checks with automatic reconnection
- Rate limiting per exchange API
- Credential management from environment

**Key Interface:**

```go
// Exchange defines the unified interface for all exchange adapters
type Exchange interface {
    // Connection management
    Connect(ctx context.Context) error
    Disconnect() error
    IsConnected() bool

    // Market data
    GetOrderbook(ctx context.Context, pair string) (*domain.Orderbook, error)
    SubscribeOrderbook(ctx context.Context, pairs []string) (<-chan *domain.Orderbook, error)

    // Trading
    PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Trade, error)
    CancelOrder(ctx context.Context, orderID string) error
    GetOrder(ctx context.Context, orderID string) (*domain.Order, error)

    // Account
    GetBalances(ctx context.Context) (map[string]decimal.Decimal, error)
    GetFees(pair string) (maker, taker decimal.Decimal)

    // Metadata
    Name() string
    SupportedPairs() []string
}

// Manager coordinates multiple exchanges
type Manager struct {
    exchanges map[string]Exchange
    mu        sync.RWMutex
    logger    *zap.Logger
}
```

### 2. Orderbook Cache (`internal/orderbook/cache.go`)

Real-time synchronized orderbook storage with staleness detection.

**Key Features:**
- Thread-safe access via sync.RWMutex
- Automatic staleness detection (reject if > 100ms old)
- Redis backend for multi-process sharing
- Memory-efficient storage with depth limits

**Example Implementation Pattern:**

```go
type Cache struct {
    local  map[string]*TimestampedOrderbook // exchange:pair -> orderbook
    redis  *redis.Client
    mu     sync.RWMutex
    maxAge time.Duration
    logger *zap.Logger
}

type TimestampedOrderbook struct {
    Orderbook *domain.Orderbook
    UpdatedAt time.Time
}

func (c *Cache) Get(exchange, pair string) (*domain.Orderbook, error) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    key := fmt.Sprintf("%s:%s", exchange, pair)
    ts, ok := c.local[key]
    if !ok {
        return nil, ErrOrderbookNotFound
    }

    if time.Since(ts.UpdatedAt) > c.maxAge {
        return nil, ErrOrderbookStale
    }

    return ts.Orderbook, nil
}
```

### 3. Arbitrage Detectors (`internal/detector/`)

#### Cross-Exchange Detector

Compares best bid/ask across exchanges accounting for all fees.

```go
type CrossExchangeDetector struct {
    cache      *orderbook.Cache
    calculator *ProfitCalculator
    minProfit  decimal.Decimal
    logger     *zap.Logger
}

func (d *CrossExchangeDetector) Detect(ctx context.Context, pair string) ([]*domain.Opportunity, error) {
    // Get orderbooks from all exchanges
    // Compare best bid on exchange A vs best ask on exchange B
    // Calculate net profit after fees (maker/taker + withdrawal)
    // Filter by minimum profit threshold
    // Return sorted opportunities
}
```

#### Triangular Detector (Bellman-Ford)

Finds negative cycles in exchange rate graph.

```go
type TriangularDetector struct {
    cache     *orderbook.Cache
    exchange  exchange.Exchange
    minProfit decimal.Decimal
    maxHops   int
    logger    *zap.Logger
}

// Graph representation:
// Vertices: currencies (BTC, ETH, USDT, BNB, etc.)
// Edges: trading pairs with weight = -log(rate * (1 - fee))
// Negative cycle = profitable arbitrage path

func (d *TriangularDetector) buildGraph(orderbooks map[string]*domain.Orderbook) *Graph {
    // For each trading pair A/B:
    // - Add edge A->B with weight -log(bid * (1 - takerFee))
    // - Add edge B->A with weight -log(1/ask * (1 - takerFee))
}

func (d *TriangularDetector) findNegativeCycles(g *Graph) [][]string {
    // Bellman-Ford algorithm
    // Returns paths where sum of weights < 0 (profit > 0)
}
```

### 4. Execution Engine (`internal/executor/engine.go`)

Parallel order execution with atomic semantics.

**Key Features:**
- Parallel order placement via errgroup
- Atomic execution: all orders succeed or all are cancelled
- Automatic rollback on partial failures
- Context-based timeout and cancellation

```go
type Engine struct {
    exchanges exchange.Manager
    risk      *risk.Manager
    retry     *RetryPolicy
    logger    *zap.Logger
}

func (e *Engine) Execute(ctx context.Context, opp *domain.Opportunity) (*ExecutionResult, error) {
    // 1. Validate opportunity is still profitable
    // 2. Check risk limits
    // 3. Reserve balances
    // 4. Place orders in parallel
    // 5. Monitor fills
    // 6. Rollback if any order fails
    // 7. Record execution for audit
}

// Parallel execution pattern
func (e *Engine) placeOrdersParallel(ctx context.Context, orders []*domain.Order) ([]*domain.Trade, error) {
    g, ctx := errgroup.WithContext(ctx)
    trades := make([]*domain.Trade, len(orders))

    for i, order := range orders {
        i, order := i, order // capture loop variables
        g.Go(func() error {
            trade, err := e.exchanges.Get(order.Exchange).PlaceOrder(ctx, order)
            if err != nil {
                return fmt.Errorf("place order on %s: %w", order.Exchange, err)
            }
            trades[i] = trade
            return nil
        })
    }

    if err := g.Wait(); err != nil {
        return nil, err
    }
    return trades, nil
}
```

### 5. Risk Manager (`internal/risk/manager.go`)

Enforces trading limits and safety controls.

**Risk Controls:**
- Position limits per exchange (max 20% of capital)
- Minimum profit threshold (0.2-0.5% after fees)
- Maximum slippage tolerance (1%)
- Daily loss limit (5%)
- Kill-switch on excessive drawdown

```go
type Manager struct {
    config     *Config
    balances   *balance.Tracker
    positions  map[string]decimal.Decimal
    dailyPnL   decimal.Decimal
    isKilled   atomic.Bool
    mu         sync.RWMutex
    logger     *zap.Logger
}

func (m *Manager) ValidateOpportunity(opp *domain.Opportunity) error {
    if m.isKilled.Load() {
        return ErrKillSwitchActive
    }

    if opp.ExpectedProfit.LessThan(m.config.MinProfitThreshold) {
        return ErrProfitBelowThreshold
    }

    if opp.EstimatedSlippage.GreaterThan(m.config.MaxSlippage) {
        return ErrSlippageTooHigh
    }

    // Check position limits...
    // Check daily loss limit...

    return nil
}

func (m *Manager) TriggerKillSwitch(reason string) {
    m.isKilled.Store(true)
    m.logger.Error("kill switch triggered", zap.String("reason", reason))
    // Send alert via Telegram
    // Cancel all open orders
    // Close positions if configured
}
```

---

## Domain Models (`internal/domain/`)

### Order

```go
type OrderSide string

const (
    OrderSideBuy  OrderSide = "buy"
    OrderSideSell OrderSide = "sell"
)

type OrderType string

const (
    OrderTypeLimit  OrderType = "limit"
    OrderTypeMarket OrderType = "market"
)

type OrderStatus string

const (
    OrderStatusPending   OrderStatus = "pending"
    OrderStatusOpen      OrderStatus = "open"
    OrderStatusFilled    OrderStatus = "filled"
    OrderStatusCancelled OrderStatus = "cancelled"
    OrderStatusFailed    OrderStatus = "failed"
)

type Order struct {
    ID        string
    Exchange  string
    Pair      string
    Side      OrderSide
    Type      OrderType
    Price     decimal.Decimal
    Quantity  decimal.Decimal
    Status    OrderStatus
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

### Orderbook

```go
type PriceLevel struct {
    Price    decimal.Decimal
    Quantity decimal.Decimal
}

type Orderbook struct {
    Exchange  string
    Pair      string
    Bids      []PriceLevel // Sorted descending by price
    Asks      []PriceLevel // Sorted ascending by price
    Timestamp time.Time
    Sequence  int64
}

func (o *Orderbook) BestBid() PriceLevel { return o.Bids[0] }
func (o *Orderbook) BestAsk() PriceLevel { return o.Asks[0] }
func (o *Orderbook) Spread() decimal.Decimal {
    return o.BestAsk().Price.Sub(o.BestBid().Price)
}
```

### Opportunity

```go
type OpportunityType string

const (
    OpportunityTypeCrossExchange OpportunityType = "cross_exchange"
    OpportunityTypeTriangular    OpportunityType = "triangular"
)

type Opportunity struct {
    ID             string
    Type           OpportunityType
    Orders         []*Order           // Orders to execute
    ExpectedProfit decimal.Decimal    // Profit after all fees
    ProfitPercent  decimal.Decimal    // Profit as percentage
    EstimatedSlippage decimal.Decimal
    DetectedAt     time.Time
    ExpiresAt      time.Time          // Opportunity staleness
    Metadata       map[string]string  // Additional context
}
```

---

## Configuration

### config.yaml

```yaml
app:
  name: arbitragebot
  env: production  # development, staging, production
  log_level: info

exchanges:
  binance:
    enabled: true
    testnet: false
    fee_maker: "0.0002"  # 0.02%
    fee_taker: "0.0004"  # 0.04%
    rate_limit: 1200     # requests per minute
    websocket:
      enabled: true
      ping_interval: 30s
      reconnect_delay: 5s
  bybit:
    enabled: true
    testnet: false
    fee_maker: "0.0001"
    fee_taker: "0.0006"
    rate_limit: 600
    websocket:
      enabled: true
      ping_interval: 20s
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
    min_profit_threshold: "0.003"  # 0.3%
    max_slippage: "0.01"           # 1%
    min_volume_usd: "100"
  triangular:
    enabled: true
    min_profit_threshold: "0.002"  # 0.2%
    max_path_length: 3
    currencies:
      - BTC
      - ETH
      - USDT
      - BNB

execution:
  timeout: 5s
  retry:
    max_attempts: 3
    initial_delay: 100ms
    max_delay: 1s
    multiplier: 2.0

risk:
  max_position_per_exchange: "0.20"  # 20%
  daily_loss_limit: "0.05"           # 5%
  kill_switch_drawdown: "0.05"       # 5%
  max_open_orders: 10

pairs:
  - BTC/USDT
  - ETH/USDT
  - BNB/USDT
  - SOL/USDT
  - ETH/BTC

notification:
  telegram:
    enabled: true
    notify_opportunities: false  # Too noisy in production
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
```

### Environment Variables

```bash
# Exchange credentials (NEVER commit these)
BINANCE_API_KEY=your_api_key
BINANCE_API_SECRET=your_api_secret
BYBIT_API_KEY=your_api_key
BYBIT_API_SECRET=your_api_secret

# Telegram notifications
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id

# Redis (if not using default)
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=

# Application
APP_ENV=production
LOG_LEVEL=info
```

---

## Coding Guidelines

### Error Handling

Always wrap errors with context:

```go
// Good
if err != nil {
    return fmt.Errorf("failed to fetch orderbook for %s on %s: %w", pair, exchange, err)
}

// Bad
if err != nil {
    return err
}
```

### Context Usage

Always propagate context for cancellation and timeouts:

```go
func (s *Service) DoSomething(ctx context.Context) error {
    ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()

    select {
    case <-ctx.Done():
        return ctx.Err()
    case result := <-s.process(ctx):
        return result
    }
}
```

### Decimal Arithmetic

Never use float64 for prices or quantities:

```go
// Good
price := decimal.NewFromString("0.00001234")
quantity := decimal.NewFromFloat(1.5)
total := price.Mul(quantity)

// Bad
price := 0.00001234
quantity := 1.5
total := price * quantity // Floating point errors!
```

### Interface-First Design

Define interfaces where they are used, not where they are implemented:

```go
// In detector/cross_exchange.go (consumer)
type OrderbookProvider interface {
    Get(exchange, pair string) (*domain.Orderbook, error)
}

// In orderbook/cache.go (implementer)
type Cache struct { ... }
func (c *Cache) Get(exchange, pair string) (*domain.Orderbook, error) { ... }
```

### Goroutine Safety

Always use proper synchronization:

```go
// Using mutex for shared state
type Cache struct {
    data map[string]interface{}
    mu   sync.RWMutex
}

func (c *Cache) Get(key string) interface{} {
    c.mu.RLock()
    defer c.mu.RUnlock()
    return c.data[key]
}

// Using channels for communication
type Worker struct {
    jobs    chan Job
    results chan Result
    done    chan struct{}
}
```

---

## Testing Strategy

### Test Location

All tests are placed in the `tests/` directory, organized by package:

```
tests/
├── config/           # pkg/config tests
├── integration/      # Integration tests (require external services)
├── e2e/              # End-to-end tests
└── mocks/            # Generated mocks
```

**Convention:** Test files use `_test` package suffix for black-box testing:
```go
package config_test  // tests pkg/config from external perspective

import "arbitragebot/pkg/config"
```

Run all tests:
```bash
go test ./...                           # All tests
go test ./tests/config/...              # Config tests only
go test -tags=integration ./tests/...   # Integration tests
```

### Unit Tests

Table-driven tests with parallel execution:

```go
func TestProfitCalculator_Calculate(t *testing.T) {
    t.Parallel()

    tests := []struct {
        name       string
        buyPrice   string
        sellPrice  string
        quantity   string
        makerFee   string
        takerFee   string
        wantProfit string
        wantErr    bool
    }{
        {
            name:       "profitable trade",
            buyPrice:   "100.00",
            sellPrice:  "101.00",
            quantity:   "1.0",
            makerFee:   "0.001",
            takerFee:   "0.001",
            wantProfit: "0.798", // 1% - 0.2% fees
        },
        {
            name:       "unprofitable after fees",
            buyPrice:   "100.00",
            sellPrice:  "100.10",
            quantity:   "1.0",
            makerFee:   "0.001",
            takerFee:   "0.001",
            wantProfit: "-0.102",
        },
    }

    for _, tt := range tests {
        tt := tt // capture range variable
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()

            calc := NewProfitCalculator()
            profit, err := calc.Calculate(
                decimal.RequireFromString(tt.buyPrice),
                decimal.RequireFromString(tt.sellPrice),
                decimal.RequireFromString(tt.quantity),
                decimal.RequireFromString(tt.makerFee),
                decimal.RequireFromString(tt.takerFee),
            )

            if tt.wantErr {
                require.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, tt.wantProfit, profit.StringFixed(3))
        })
    }
}
```

### Integration Tests

Use test containers or testnet:

```go
func TestBinanceAdapter_Integration(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    adapter := binance.NewAdapter(binance.Config{
        Testnet: true,
        APIKey:  os.Getenv("BINANCE_TESTNET_API_KEY"),
        Secret:  os.Getenv("BINANCE_TESTNET_SECRET"),
    })

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    err := adapter.Connect(ctx)
    require.NoError(t, err)
    defer adapter.Disconnect()

    orderbook, err := adapter.GetOrderbook(ctx, "BTC/USDT")
    require.NoError(t, err)
    assert.NotEmpty(t, orderbook.Bids)
    assert.NotEmpty(t, orderbook.Asks)
}
```

### Mocks

Generate mocks using mockgen:

```go
//go:generate mockgen -source=interface.go -destination=../tests/mocks/exchange_mock.go -package=mocks

type Exchange interface {
    // ...
}
```

---

## Prometheus Metrics

```go
var (
    opportunitiesDetected = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "arbitrage_opportunities_detected_total",
            Help: "Total number of arbitrage opportunities detected",
        },
        []string{"type", "pair"},
    )

    executionsTotal = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "arbitrage_executions_total",
            Help: "Total number of arbitrage executions",
        },
        []string{"type", "status"},
    )

    profitUSD = promauto.NewGauge(
        prometheus.GaugeOpts{
            Name: "arbitrage_profit_usd",
            Help: "Total profit in USD",
        },
    )

    orderbookStaleness = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "orderbook_staleness_seconds",
            Help:    "Age of orderbook data in seconds",
            Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0},
        },
        []string{"exchange", "pair"},
    )

    exchangeLatency = promauto.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "exchange_api_latency_seconds",
            Help:    "Exchange API request latency",
            Buckets: prometheus.DefBuckets,
        },
        []string{"exchange", "operation"},
    )

    websocketReconnects = promauto.NewCounterVec(
        prometheus.CounterOpts{
            Name: "websocket_reconnects_total",
            Help: "Total WebSocket reconnections",
        },
        []string{"exchange"},
    )
)
```

---

## CLI Usage

```bash
# Production mode
./arbitragebot --config configs/config.yaml

# Dry-run (paper trading)
./arbitragebot --config configs/config.yaml --dry-run

# With specific log level
./arbitragebot --config configs/config.yaml --log-level debug

# Backtest mode (if implemented)
./arbitragebot backtest \
    --config configs/config.yaml \
    --data ./historical_data/ \
    --from 2024-01-01 \
    --to 2024-12-31
```

---

## Troubleshooting Guide

### Common Issues

#### 1. "orderbook stale" errors
**Cause:** WebSocket disconnection or network latency
**Solution:**
- Check network connectivity
- Verify WebSocket heartbeat configuration
- Increase `orderbook.max_age` if network is slow (not recommended for production)

#### 2. "rate limit exceeded" from exchange
**Cause:** Too many API requests
**Solution:**
- Reduce `rate_limit` in config
- Implement request batching
- Use WebSocket instead of REST for market data

#### 3. "insufficient balance" errors
**Cause:** Balance not updated or reserved by pending orders
**Solution:**
- Check balance synchronization
- Verify no stuck pending orders
- Add safety margin to balance checks

#### 4. High latency in order execution
**Cause:** Network latency or exchange congestion
**Solution:**
- Deploy closer to exchange servers (AWS Tokyo for Asian exchanges)
- Use co-located servers if available
- Optimize order size to improve fill rate

#### 5. Kill switch triggered unexpectedly
**Cause:** Rapid losses or calculation errors
**Solution:**
- Review execution logs for failed trades
- Check for price feed anomalies
- Verify fee calculations are accurate

### Debug Mode

Enable detailed logging:

```yaml
app:
  log_level: debug

# Or via environment
LOG_LEVEL=debug ./arbitragebot --config configs/config.yaml
```

### Health Checks

```bash
# Check application health
curl http://localhost:8080/health

# Check metrics
curl http://localhost:9090/metrics | grep arbitrage
```

---

## Performance Optimization

### Memory Management

```go
// Use sync.Pool for frequently allocated objects
var orderbookPool = sync.Pool{
    New: func() interface{} {
        return &domain.Orderbook{
            Bids: make([]domain.PriceLevel, 0, 100),
            Asks: make([]domain.PriceLevel, 0, 100),
        }
    },
}

func getOrderbook() *domain.Orderbook {
    return orderbookPool.Get().(*domain.Orderbook)
}

func putOrderbook(ob *domain.Orderbook) {
    ob.Bids = ob.Bids[:0]
    ob.Asks = ob.Asks[:0]
    orderbookPool.Put(ob)
}
```

### GC Tuning

```bash
# Reduce GC frequency for latency-sensitive applications
GOGC=200 ./arbitragebot --config configs/config.yaml
```

### Benchmarking

```go
func BenchmarkDetector_Detect(b *testing.B) {
    detector := setupDetector()
    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = detector.Detect(ctx, "BTC/USDT")
    }
}
```

Run benchmarks:

```bash
go test -bench=. -benchmem ./internal/detector/...
```

---

## Deployment

### Docker

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o arbitragebot ./cmd/arbitrage

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/arbitragebot .
COPY --from=builder /app/configs ./configs
EXPOSE 8080 9090
CMD ["./arbitragebot", "--config", "configs/config.yaml"]
```

### AWS Deployment Considerations

- **Region:** ap-northeast-1 (Tokyo) or ap-southeast-1 (Singapore) for Asian exchanges
- **Instance:** c6i.large or better for low-latency networking
- **Network:** Enable enhanced networking (ENA)
- **Storage:** Use gp3 EBS for logs, instance store for temp data

---

## Security Checklist

- [ ] API keys stored in environment variables or secrets manager
- [ ] API keys have only read+trade permissions (NO withdrawal)
- [ ] All external inputs validated
- [ ] Rate limiting implemented per exchange
- [ ] TLS enabled for all external connections
- [ ] Audit logging for all trades and decisions
- [ ] Kill-switch tested and working
- [ ] Credentials rotated regularly

---

## Development Workflow

### Setup

```bash
# Clone repository
git clone <repo-url>
cd arbitragebot

# Install dependencies
go mod download

# Copy example config
cp configs/config.example.yaml configs/config.yaml

# Set environment variables
export BINANCE_API_KEY=your_key
export BINANCE_API_SECRET=your_secret

# Run tests
go test ./...

# Run linter
golangci-lint run

# Build
go build -o arbitragebot ./cmd/arbitrage
```

### Makefile Targets

```makefile
.PHONY: build test lint run

build:
	go build -o bin/arbitragebot ./cmd/arbitrage

test:
	go test -race -cover ./...

test-integration:
	go test -race -tags=integration ./tests/integration/...

lint:
	golangci-lint run

run:
	go run ./cmd/arbitrage --config configs/config.yaml

run-dry:
	go run ./cmd/arbitrage --config configs/config.yaml --dry-run

docker-build:
	docker build -t arbitragebot:latest .

docker-run:
	docker-compose up -d
```

---

## Go Best Practices Summary

1. **Readability over cleverness** - Write clear, obvious code
2. **Handle all errors** - Never ignore errors, always wrap with context
3. **Use interfaces for abstraction** - Define interfaces where consumed
4. **Prefer composition** - Small, focused types composed together
5. **Context everywhere** - Propagate context for cancellation/timeouts
6. **No globals** - Use dependency injection
7. **Test everything** - Table-driven tests, high coverage
8. **Profile before optimizing** - Don't guess, measure
9. **Document public APIs** - GoDoc comments on exported functions
10. **Keep dependencies minimal** - Prefer stdlib when possible