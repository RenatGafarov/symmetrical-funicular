package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"arbitragebot/internal/domain"
)

// Sentinel errors for exchange operations.
var (
	ErrNotConnected     = errors.New("exchange not connected")
	ErrPairNotSupported = errors.New("trading pair not supported")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrOrderNotFound    = errors.New("order not found")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// Fees represents maker and taker fees.
type Fees struct {
	Maker decimal.Decimal
	Taker decimal.Decimal
}

// Adapter implements the exchange.Exchange interface for Binance Spot.
type Adapter struct {
	client    *Client
	wsManager *WebSocketManager
	config    Config
	fees      Fees
	pairs     []string
	connected atomic.Bool
	logger    *zap.Logger
}

// Config holds configuration for the Binance adapter.
type Config struct {
	// APIKey is the Binance API key.
	APIKey string
	// APISecret is the Binance API secret.
	APISecret string
	// Testnet enables testnet mode.
	Testnet bool
	// FeeMaker is the maker fee as a decimal.
	FeeMaker decimal.Decimal
	// FeeTaker is the taker fee as a decimal.
	FeeTaker decimal.Decimal
	// RateLimit is the maximum API requests per minute.
	RateLimit int
	// Pairs is the list of trading pairs to support.
	Pairs []string
	// WebSocketEnabled enables WebSocket for real-time data.
	WebSocketEnabled bool
	// PingInterval is the WebSocket ping interval.
	PingInterval time.Duration
	// ReconnectDelay is the WebSocket reconnect delay.
	ReconnectDelay time.Duration
	// Logger is the logger instance.
	Logger *zap.Logger
}

// NewAdapter creates a new Binance adapter.
func NewAdapter(cfg Config) *Adapter {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	client := NewClient(ClientConfig{
		APIKey:    cfg.APIKey,
		APISecret: cfg.APISecret,
		Testnet:   cfg.Testnet,
		RateLimit: cfg.RateLimit,
		Logger:    logger,
	})

	return &Adapter{
		client: client,
		config: cfg,
		fees: Fees{
			Maker: cfg.FeeMaker,
			Taker: cfg.FeeTaker,
		},
		pairs:  cfg.Pairs,
		logger: logger,
	}
}

// NewAdapterWithBaseURL creates a new Binance adapter with a custom base URL.
// This is primarily used for testing with mock servers.
func NewAdapterWithBaseURL(baseURL string, cfg Config) *Adapter {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	client := NewClientWithBaseURL(baseURL, ClientConfig{
		APIKey:    cfg.APIKey,
		APISecret: cfg.APISecret,
		RateLimit: cfg.RateLimit,
		Logger:    logger,
	})

	return &Adapter{
		client: client,
		config: cfg,
		fees: Fees{
			Maker: cfg.FeeMaker,
			Taker: cfg.FeeTaker,
		},
		pairs:  cfg.Pairs,
		logger: logger,
	}
}

// Name returns the exchange identifier.
func (a *Adapter) Name() string {
	return "binance"
}

// Connect establishes connection to Binance API.
func (a *Adapter) Connect(ctx context.Context) error {
	// Verify connectivity by fetching server time
	serverTime, err := a.client.GetServerTime(ctx)
	if err != nil {
		return fmt.Errorf("connect to binance: %w", err)
	}

	localTime := time.Now()
	drift := localTime.Sub(serverTime)
	if drift < 0 {
		drift = -drift
	}

	a.logger.Info("connected to binance",
		zap.Time("server_time", serverTime),
		zap.Duration("clock_drift", drift))

	if drift > 5*time.Second {
		a.logger.Warn("significant clock drift detected", zap.Duration("drift", drift))
	}

	a.connected.Store(true)
	return nil
}

// Disconnect closes all connections to Binance.
func (a *Adapter) Disconnect() error {
	a.connected.Store(false)

	if a.wsManager != nil {
		a.wsManager.Close()
	}

	a.logger.Info("disconnected from binance")
	return nil
}

// IsConnected returns true if connected to Binance.
func (a *Adapter) IsConnected() bool {
	return a.connected.Load()
}

// GetOrderbook fetches the current orderbook for a trading pair.
func (a *Adapter) GetOrderbook(ctx context.Context, pair string) (*domain.Orderbook, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	symbol := pairToSymbol(pair)
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("limit", "20")

	body, err := a.client.Request(ctx, http.MethodGet, "/api/v3/depth", params, false)
	if err != nil {
		return nil, fmt.Errorf("get orderbook for %s: %w", pair, err)
	}

	var resp orderbookResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse orderbook: %w", err)
	}

	return resp.toDomain(pair), nil
}

// SubscribeOrderbook opens a real-time orderbook stream for the given pairs.
func (a *Adapter) SubscribeOrderbook(ctx context.Context, pairs []string) (<-chan *domain.Orderbook, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	if !a.config.WebSocketEnabled {
		return nil, fmt.Errorf("websocket not enabled")
	}

	wsURL := "wss://stream.binance.com:9443/ws"
	if a.config.Testnet {
		wsURL = "wss://stream.testnet.binance.vision/ws"
	}

	a.wsManager = NewWebSocketManager(WebSocketConfig{
		URL:            wsURL,
		Pairs:          pairs,
		PingInterval:   a.config.PingInterval,
		ReconnectDelay: a.config.ReconnectDelay,
		Logger:         a.logger,
	})

	return a.wsManager.Subscribe(ctx)
}

// PlaceOrder submits a new order to Binance.
func (a *Adapter) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Trade, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	symbol := pairToSymbol(order.Pair)
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", strings.ToUpper(string(order.Side)))
	params.Set("type", strings.ToUpper(string(order.Type)))
	params.Set("quantity", order.Quantity.String())

	if order.Type == domain.OrderTypeLimit {
		params.Set("price", order.Price.String())
		params.Set("timeInForce", "GTC")
	}

	body, err := a.client.Request(ctx, http.MethodPost, "/api/v3/order", params, true)
	if err != nil {
		return nil, a.mapError(err, order.Pair)
	}

	var resp orderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse order response: %w", err)
	}

	return resp.toTrade(order.Pair), nil
}

// CancelOrder cancels an open order by its ID.
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	if !a.IsConnected() {
		return ErrNotConnected
	}

	// orderID format: "symbol:orderId"
	parts := strings.SplitN(orderID, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid order ID format: %s", orderID)
	}

	symbol, id := parts[0], parts[1]
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", id)

	_, err := a.client.Request(ctx, http.MethodDelete, "/api/v3/order", params, true)
	if err != nil {
		return a.mapError(err, symbolToPair(symbol))
	}

	return nil
}

// GetOrder retrieves the current state of an order by its ID.
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*domain.Order, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	// orderID format: "symbol:orderId"
	parts := strings.SplitN(orderID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid order ID format: %s", orderID)
	}

	symbol, id := parts[0], parts[1]
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", id)

	body, err := a.client.Request(ctx, http.MethodGet, "/api/v3/order", params, true)
	if err != nil {
		return nil, a.mapError(err, symbolToPair(symbol))
	}

	var resp orderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse order: %w", err)
	}

	return resp.toOrder(symbolToPair(symbol)), nil
}

// GetBalances returns available balances for all assets.
func (a *Adapter) GetBalances(ctx context.Context) (map[string]decimal.Decimal, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	body, err := a.client.Request(ctx, http.MethodGet, "/api/v3/account", nil, true)
	if err != nil {
		return nil, fmt.Errorf("get balances: %w", err)
	}

	var resp accountResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse account: %w", err)
	}

	balances := make(map[string]decimal.Decimal)
	for _, b := range resp.Balances {
		free, err := decimal.NewFromString(b.Free)
		if err != nil {
			continue
		}
		if free.IsPositive() {
			balances[b.Asset] = free
		}
	}

	return balances, nil
}

// GetFees returns the maker and taker fees for a trading pair.
func (a *Adapter) GetFees(_ string) Fees {
	return a.fees
}

// SupportedPairs returns a list of trading pairs available on Binance.
func (a *Adapter) SupportedPairs() []string {
	return a.pairs
}

// mapError maps Binance API errors to sentinel errors.
func (a *Adapter) mapError(err error, pair string) error {
	apiErr, ok := err.(*APIError)
	if !ok {
		return err
	}

	switch apiErr.Code {
	case -2010: // Insufficient balance
		return ErrInsufficientFunds
	case -2011: // Unknown order
		return ErrOrderNotFound
	case -1121: // Invalid symbol
		return ErrPairNotSupported
	case -1015: // Too many orders
		return ErrRateLimitExceeded
	case -1003: // Too many requests
		return ErrRateLimitExceeded
	default:
		return fmt.Errorf("binance error for %s: %w", pair, apiErr)
	}
}

// pairToSymbol converts "BTC/USDT" to "BTCUSDT".
func pairToSymbol(pair string) string {
	return strings.ReplaceAll(pair, "/", "")
}

// symbolToPair converts "BTCUSDT" to "BTC/USDT".
// Note: This is a simplified version that assumes USDT quote currency.
func symbolToPair(symbol string) string {
	// Common quote currencies
	quotes := []string{"USDT", "BUSD", "BTC", "ETH", "BNB"}
	for _, q := range quotes {
		if strings.HasSuffix(symbol, q) {
			base := strings.TrimSuffix(symbol, q)
			return base + "/" + q
		}
	}
	return symbol
}

// API response types

type orderbookResponse struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
}

func (r *orderbookResponse) toDomain(pair string) *domain.Orderbook {
	bids := make([]domain.PriceLevel, 0, len(r.Bids))
	for _, b := range r.Bids {
		if len(b) < 2 {
			continue
		}
		price, _ := decimal.NewFromString(b[0])
		qty, _ := decimal.NewFromString(b[1])
		bids = append(bids, domain.PriceLevel{Price: price, Quantity: qty})
	}

	asks := make([]domain.PriceLevel, 0, len(r.Asks))
	for _, a := range r.Asks {
		if len(a) < 2 {
			continue
		}
		price, _ := decimal.NewFromString(a[0])
		qty, _ := decimal.NewFromString(a[1])
		asks = append(asks, domain.PriceLevel{Price: price, Quantity: qty})
	}

	return &domain.Orderbook{
		Exchange:  "binance",
		Pair:      pair,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.Now(),
		Sequence:  r.LastUpdateID,
	}
}

type orderResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	ClientOrderID       string `json:"clientOrderId"`
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CumulativeQuoteQty  string `json:"cummulativeQuoteQty"`
	Status              string `json:"status"`
	Type                string `json:"type"`
	Side                string `json:"side"`
	TransactTime        int64  `json:"transactTime"`
	UpdateTime          int64  `json:"updateTime"`
}

func (r *orderResponse) toOrder(pair string) *domain.Order {
	price, _ := decimal.NewFromString(r.Price)
	qty, _ := decimal.NewFromString(r.OrigQty)

	return &domain.Order{
		ID:        fmt.Sprintf("%s:%d", r.Symbol, r.OrderID),
		Exchange:  "binance",
		Pair:      pair,
		Side:      domain.OrderSide(strings.ToLower(r.Side)),
		Type:      domain.OrderType(strings.ToLower(r.Type)),
		Price:     price,
		Quantity:  qty,
		Status:    mapOrderStatus(r.Status),
		CreatedAt: time.UnixMilli(r.TransactTime),
		UpdatedAt: time.UnixMilli(r.UpdateTime),
	}
}

func (r *orderResponse) toTrade(pair string) *domain.Trade {
	price, _ := decimal.NewFromString(r.Price)
	qty, _ := decimal.NewFromString(r.ExecutedQty)

	return &domain.Trade{
		ID:        fmt.Sprintf("%s:%d", r.Symbol, r.OrderID),
		OrderID:   fmt.Sprintf("%s:%d", r.Symbol, r.OrderID),
		Exchange:  "binance",
		Pair:      pair,
		Side:      domain.OrderSide(strings.ToLower(r.Side)),
		Price:     price,
		Quantity:  qty,
		Timestamp: time.UnixMilli(r.TransactTime),
	}
}

func mapOrderStatus(status string) domain.OrderStatus {
	switch status {
	case "NEW":
		return domain.OrderStatusOpen
	case "PARTIALLY_FILLED":
		return domain.OrderStatusOpen
	case "FILLED":
		return domain.OrderStatusFilled
	case "CANCELED":
		return domain.OrderStatusCancelled
	case "REJECTED", "EXPIRED":
		return domain.OrderStatusFailed
	default:
		return domain.OrderStatusPending
	}
}

type accountResponse struct {
	Balances []struct {
		Asset  string `json:"asset"`
		Free   string `json:"free"`
		Locked string `json:"locked"`
	} `json:"balances"`
}