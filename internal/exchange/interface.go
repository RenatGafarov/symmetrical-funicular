// Package exchange provides a unified interface for interacting with cryptocurrency exchanges.
// It defines the Exchange interface that all exchange adapters must implement,
// as well as the Manager for coordinating multiple exchanges.
package exchange

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"arbitragebot/internal/domain"
)

// Sentinel errors for exchange operations.
var (
	// ErrNotConnected is returned when an operation requires a connection but the exchange is not connected.
	ErrNotConnected = errors.New("exchange not connected")
	// ErrExchangeNotFound is returned when requesting an exchange that is not registered.
	ErrExchangeNotFound = errors.New("exchange not found")
	// ErrPairNotSupported is returned when a trading pair is not available on the exchange.
	ErrPairNotSupported = errors.New("trading pair not supported")
	// ErrInsufficientFunds is returned when there's not enough balance to place an order.
	ErrInsufficientFunds = errors.New("insufficient funds")
	// ErrOrderNotFound is returned when an order ID doesn't exist on the exchange.
	ErrOrderNotFound = errors.New("order not found")
	// ErrRateLimitExceeded is returned when the API rate limit has been exceeded.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// Fees represents maker and taker fees for a trading pair.
type Fees struct {
	// Maker is the fee for limit orders that add liquidity (expressed as decimal, e.g., 0.001 for 0.1%).
	Maker decimal.Decimal
	// Taker is the fee for market orders that remove liquidity (expressed as decimal, e.g., 0.001 for 0.1%).
	Taker decimal.Decimal
}

// Exchange defines the unified interface for all exchange adapters.
// Implementations must be safe for concurrent use.
type Exchange interface {
	// Connect establishes connection to the exchange API.
	// It should initialize WebSocket connections and authenticate if required.
	// Returns error if connection fails or context is cancelled.
	Connect(ctx context.Context) error

	// Disconnect closes all connections to the exchange.
	// It should gracefully shutdown WebSocket connections and cleanup resources.
	// Safe to call multiple times.
	Disconnect() error

	// IsConnected returns true if the exchange connection is active and healthy.
	IsConnected() bool

	// GetOrderbook fetches the current orderbook for a trading pair.
	// The pair format should be "BASE/QUOTE" (e.g., "BTC/USDT").
	// Returns ErrPairNotSupported if the pair is not available on this exchange.
	GetOrderbook(ctx context.Context, pair string) (*domain.Orderbook, error)

	// SubscribeOrderbook opens a real-time orderbook stream for the given pairs.
	// Returns a channel that receives orderbook updates.
	// The channel is closed when context is cancelled or connection is lost.
	// Caller should handle reconnection by calling this method again.
	SubscribeOrderbook(ctx context.Context, pairs []string) (<-chan *domain.Orderbook, error)

	// PlaceOrder submits a new order to the exchange.
	// Returns the resulting trade if the order is filled immediately (market orders),
	// or a trade with zero quantity if the order is placed but not yet filled (limit orders).
	// Returns ErrInsufficientFunds if balance is not enough.
	PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Trade, error)

	// CancelOrder cancels an open order by its ID.
	// Returns ErrOrderNotFound if the order doesn't exist or is already filled/cancelled.
	CancelOrder(ctx context.Context, orderID string) error

	// GetOrder retrieves the current state of an order by its ID.
	// Returns ErrOrderNotFound if the order doesn't exist.
	GetOrder(ctx context.Context, orderID string) (*domain.Order, error)

	// GetBalances returns available balances for all assets.
	// The map keys are asset symbols (e.g., "BTC", "USDT").
	// Only non-zero balances are included.
	GetBalances(ctx context.Context) (map[string]decimal.Decimal, error)

	// GetFees returns the maker and taker fees for a trading pair.
	// Fees are expressed as decimals (e.g., 0.001 for 0.1%).
	GetFees(pair string) Fees

	// Name returns the unique identifier of this exchange (e.g., "binance", "bybit").
	Name() string

	// SupportedPairs returns a list of trading pairs available on this exchange.
	// Pairs are in "BASE/QUOTE" format.
	SupportedPairs() []string
}

// Config holds common exchange configuration used to initialize an exchange adapter.
type Config struct {
	// Name is the exchange identifier (e.g., "binance", "bybit").
	Name string
	// APIKey is the API key for authentication.
	APIKey string
	// APISecret is the API secret for signing requests.
	APISecret string
	// Testnet enables testnet/sandbox mode.
	Testnet bool
	// FeeMaker is the default maker fee for this exchange.
	FeeMaker decimal.Decimal
	// FeeTaker is the default taker fee for this exchange.
	FeeTaker decimal.Decimal
	// RateLimit is the maximum API requests per minute.
	RateLimit int
	// Pairs is the list of trading pairs to enable.
	Pairs []string
}
