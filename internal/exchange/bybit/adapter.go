package bybit

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
	ErrNotConnected      = errors.New("exchange not connected")
	ErrPairNotSupported  = errors.New("trading pair not supported")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrOrderNotFound     = errors.New("order not found")
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// Fees represents maker and taker fees.
type Fees struct {
	Maker decimal.Decimal
	Taker decimal.Decimal
}

// Adapter implements the exchange.Exchange interface for Bybit Spot.
type Adapter struct {
	client    *Client
	wsManager *WebSocketManager
	config    Config
	fees      Fees
	pairs     []string
	connected atomic.Bool
	logger    *zap.Logger
}

// Config holds configuration for the Bybit adapter.
type Config struct {
	// APIKey is the Bybit API key.
	APIKey string
	// APISecret is the Bybit API secret.
	APISecret string
	// Testnet enables testnet mode.
	Testnet bool
	// FeeMaker is the maker fee as a decimal.
	FeeMaker decimal.Decimal
	// FeeTaker is the taker fee as a decimal.
	FeeTaker decimal.Decimal
	// RateLimit is the maximum API requests per 5 seconds.
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

// NewAdapter creates a new Bybit adapter.
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

// NewAdapterWithBaseURL creates a new Bybit adapter with a custom base URL.
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
	return "bybit"
}

// Connect establishes connection to Bybit API.
func (a *Adapter) Connect(ctx context.Context) error {
	// Verify connectivity by fetching server time
	serverTime, err := a.client.GetServerTime(ctx)
	if err != nil {
		return fmt.Errorf("connect to bybit: %w", err)
	}

	localTime := time.Now()
	drift := localTime.Sub(serverTime)
	if drift < 0 {
		drift = -drift
	}

	a.logger.Info("connected to bybit",
		zap.Time("server_time", serverTime),
		zap.Duration("clock_drift", drift))

	if drift > 5*time.Second {
		a.logger.Warn("significant clock drift detected", zap.Duration("drift", drift))
	}

	a.connected.Store(true)
	return nil
}

// Disconnect closes all connections to Bybit.
func (a *Adapter) Disconnect() error {
	a.connected.Store(false)

	if a.wsManager != nil {
		a.wsManager.Close()
	}

	a.logger.Info("disconnected from bybit")
	return nil
}

// IsConnected returns true if connected to Bybit.
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
	params.Set("category", "spot")
	params.Set("symbol", symbol)
	params.Set("limit", "50")

	body, err := a.client.Request(ctx, http.MethodGet, "/v5/market/orderbook", params, false)
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

	wsURL := "wss://stream.bybit.com/v5/public/spot"
	if a.config.Testnet {
		wsURL = "wss://stream-testnet.bybit.com/v5/public/spot"
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

// PlaceOrder submits a new order to Bybit.
func (a *Adapter) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Trade, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	symbol := pairToSymbol(order.Pair)
	params := url.Values{}
	params.Set("category", "spot")
	params.Set("symbol", symbol)
	params.Set("side", capitalizeFirst(string(order.Side)))
	params.Set("orderType", capitalizeFirst(string(order.Type)))
	params.Set("qty", order.Quantity.String())

	if order.Type == domain.OrderTypeLimit {
		params.Set("price", order.Price.String())
		params.Set("timeInForce", "GTC")
	} else {
		params.Set("timeInForce", "IOC")
	}

	body, err := a.client.Request(ctx, http.MethodPost, "/v5/order/create", params, true)
	if err != nil {
		return nil, a.mapError(err, order.Pair)
	}

	var resp createOrderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse order response: %w", err)
	}

	return resp.toTrade(order), nil
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
	params.Set("category", "spot")
	params.Set("symbol", symbol)
	params.Set("orderId", id)

	_, err := a.client.Request(ctx, http.MethodPost, "/v5/order/cancel", params, true)
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
	params.Set("category", "spot")
	params.Set("symbol", symbol)
	params.Set("orderId", id)

	body, err := a.client.Request(ctx, http.MethodGet, "/v5/order/realtime", params, true)
	if err != nil {
		return nil, a.mapError(err, symbolToPair(symbol))
	}

	var resp getOrderResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse order: %w", err)
	}

	if len(resp.Result.List) == 0 {
		return nil, ErrOrderNotFound
	}

	return resp.Result.List[0].toOrder(symbolToPair(symbol)), nil
}

// GetBalances returns available balances for all assets.
func (a *Adapter) GetBalances(ctx context.Context) (map[string]decimal.Decimal, error) {
	if !a.IsConnected() {
		return nil, ErrNotConnected
	}

	params := url.Values{}
	params.Set("accountType", "UNIFIED")

	body, err := a.client.Request(ctx, http.MethodGet, "/v5/account/wallet-balance", params, true)
	if err != nil {
		return nil, fmt.Errorf("get balances: %w", err)
	}

	var resp walletBalanceResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse balances: %w", err)
	}

	balances := make(map[string]decimal.Decimal)
	for _, account := range resp.Result.List {
		for _, coin := range account.Coin {
			free, err := decimal.NewFromString(coin.WalletBalance)
			if err != nil {
				continue
			}
			if free.IsPositive() {
				balances[coin.Coin] = free
			}
		}
	}

	return balances, nil
}

// GetFees returns the maker and taker fees for a trading pair.
func (a *Adapter) GetFees(_ string) Fees {
	return a.fees
}

// SupportedPairs returns a list of trading pairs available on Bybit.
func (a *Adapter) SupportedPairs() []string {
	return a.pairs
}

// mapError maps Bybit API errors to sentinel errors.
func (a *Adapter) mapError(err error, pair string) error {
	apiErr, ok := err.(*APIError)
	if !ok {
		return err
	}

	switch apiErr.Code {
	case 110007: // Insufficient balance
		return ErrInsufficientFunds
	case 110001: // Order does not exist
		return ErrOrderNotFound
	case 10001: // Invalid symbol
		return ErrPairNotSupported
	case 10006: // Too many requests
		return ErrRateLimitExceeded
	default:
		return fmt.Errorf("bybit error for %s: %w", pair, apiErr)
	}
}

// pairToSymbol converts "BTC/USDT" to "BTCUSDT".
func pairToSymbol(pair string) string {
	return strings.ReplaceAll(pair, "/", "")
}

// symbolToPair converts "BTCUSDT" to "BTC/USDT".
func symbolToPair(symbol string) string {
	// Common quote currencies
	quotes := []string{"USDT", "USDC", "BTC", "ETH"}
	for _, q := range quotes {
		if strings.HasSuffix(symbol, q) {
			base := strings.TrimSuffix(symbol, q)
			return base + "/" + q
		}
	}
	return symbol
}

// capitalizeFirst capitalizes the first letter of a string.
func capitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

// API response types

type orderbookResponse struct {
	Result struct {
		Symbol    string     `json:"s"`
		Bids      [][]string `json:"b"`
		Asks      [][]string `json:"a"`
		Timestamp int64      `json:"ts"`
		UpdateID  int64      `json:"u"`
	} `json:"result"`
}

func (r *orderbookResponse) toDomain(pair string) *domain.Orderbook {
	bids := make([]domain.PriceLevel, 0, len(r.Result.Bids))
	for _, b := range r.Result.Bids {
		if len(b) < 2 {
			continue
		}
		price, _ := decimal.NewFromString(b[0])
		qty, _ := decimal.NewFromString(b[1])
		bids = append(bids, domain.PriceLevel{Price: price, Quantity: qty})
	}

	asks := make([]domain.PriceLevel, 0, len(r.Result.Asks))
	for _, a := range r.Result.Asks {
		if len(a) < 2 {
			continue
		}
		price, _ := decimal.NewFromString(a[0])
		qty, _ := decimal.NewFromString(a[1])
		asks = append(asks, domain.PriceLevel{Price: price, Quantity: qty})
	}

	return &domain.Orderbook{
		Exchange:  "bybit",
		Pair:      pair,
		Bids:      bids,
		Asks:      asks,
		Timestamp: time.UnixMilli(r.Result.Timestamp),
		Sequence:  r.Result.UpdateID,
	}
}

type createOrderResponse struct {
	Result struct {
		OrderID     string `json:"orderId"`
		OrderLinkID string `json:"orderLinkId"`
	} `json:"result"`
}

func (r *createOrderResponse) toTrade(order *domain.Order) *domain.Trade {
	symbol := pairToSymbol(order.Pair)
	return &domain.Trade{
		ID:        fmt.Sprintf("%s:%s", symbol, r.Result.OrderID),
		OrderID:   fmt.Sprintf("%s:%s", symbol, r.Result.OrderID),
		Exchange:  "bybit",
		Pair:      order.Pair,
		Side:      order.Side,
		Price:     order.Price,
		Quantity:  decimal.Zero, // Will be filled asynchronously
		Timestamp: time.Now(),
	}
}

type getOrderResponse struct {
	Result struct {
		List []orderInfo `json:"list"`
	} `json:"result"`
}

type orderInfo struct {
	OrderID     string `json:"orderId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	OrderType   string `json:"orderType"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	CumExecQty  string `json:"cumExecQty"`
	OrderStatus string `json:"orderStatus"`
	CreatedTime string `json:"createdTime"`
	UpdatedTime string `json:"updatedTime"`
}

func (o *orderInfo) toOrder(pair string) *domain.Order {
	price, _ := decimal.NewFromString(o.Price)
	qty, _ := decimal.NewFromString(o.Qty)
	createdTime, _ := time.Parse("2006-01-02T15:04:05.000Z", o.CreatedTime)
	updatedTime, _ := time.Parse("2006-01-02T15:04:05.000Z", o.UpdatedTime)

	// Try parsing as milliseconds if time parsing failed
	if createdTime.IsZero() {
		if ms, err := decimal.NewFromString(o.CreatedTime); err == nil {
			createdTime = time.UnixMilli(ms.IntPart())
		}
	}
	if updatedTime.IsZero() {
		if ms, err := decimal.NewFromString(o.UpdatedTime); err == nil {
			updatedTime = time.UnixMilli(ms.IntPart())
		}
	}

	return &domain.Order{
		ID:        fmt.Sprintf("%s:%s", o.Symbol, o.OrderID),
		Exchange:  "bybit",
		Pair:      pair,
		Side:      domain.OrderSide(strings.ToLower(o.Side)),
		Type:      domain.OrderType(strings.ToLower(o.OrderType)),
		Price:     price,
		Quantity:  qty,
		Status:    mapOrderStatus(o.OrderStatus),
		CreatedAt: createdTime,
		UpdatedAt: updatedTime,
	}
}

func mapOrderStatus(status string) domain.OrderStatus {
	switch status {
	case "New":
		return domain.OrderStatusOpen
	case "PartiallyFilled":
		return domain.OrderStatusOpen
	case "Filled":
		return domain.OrderStatusFilled
	case "Cancelled":
		return domain.OrderStatusCancelled
	case "Rejected", "Expired":
		return domain.OrderStatusFailed
	default:
		return domain.OrderStatusPending
	}
}

type walletBalanceResponse struct {
	Result struct {
		List []struct {
			AccountType string `json:"accountType"`
			Coin        []struct {
				Coin          string `json:"coin"`
				WalletBalance string `json:"walletBalance"`
				Free          string `json:"free"`
			} `json:"coin"`
		} `json:"list"`
	} `json:"result"`
}