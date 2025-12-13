package binance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"arbitragebot/internal/domain"
	"arbitragebot/internal/exchange/binance"
)

func newTestAdapter(t *testing.T, handler http.Handler) *binance.Adapter {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return binance.NewAdapterWithBaseURL(server.URL, binance.Config{
		APIKey:    "test-key",
		APISecret: "test-secret",
		FeeMaker:  decimal.NewFromFloat(0.001),
		FeeTaker:  decimal.NewFromFloat(0.001),
		Pairs:     []string{"BTC/USDT", "ETH/USDT"},
	})
}

func TestAdapter_Name(t *testing.T) {
	t.Parallel()

	adapter := binance.NewAdapter(binance.Config{})
	assert.Equal(t, "binance", adapter.Name())
}

func TestAdapter_SupportedPairs(t *testing.T) {
	t.Parallel()

	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}
	adapter := binance.NewAdapter(binance.Config{
		Pairs: pairs,
	})

	assert.Equal(t, pairs, adapter.SupportedPairs())
}

func TestAdapter_GetFees(t *testing.T) {
	t.Parallel()

	adapter := binance.NewAdapter(binance.Config{
		FeeMaker: decimal.NewFromFloat(0.0002),
		FeeTaker: decimal.NewFromFloat(0.0004),
	})

	fees := adapter.GetFees("BTC/USDT")

	assert.True(t, decimal.NewFromFloat(0.0002).Equal(fees.Maker))
	assert.True(t, decimal.NewFromFloat(0.0004).Equal(fees.Taker))
}

func TestAdapter_Connect(t *testing.T) {
	t.Parallel()

	serverTime := time.Now().UnixMilli()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/time" {
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverTime})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	adapter := newTestAdapter(t, handler)

	ctx := context.Background()
	err := adapter.Connect(ctx)

	require.NoError(t, err)
	assert.True(t, adapter.IsConnected())
}

func TestAdapter_Connect_Error(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"code": -1000,
			"msg":  "An unknown error occurred while processing the request.",
		})
	})

	adapter := newTestAdapter(t, handler)

	ctx := context.Background()
	err := adapter.Connect(ctx)

	require.Error(t, err)
	assert.False(t, adapter.IsConnected())
}

func TestAdapter_Disconnect(t *testing.T) {
	t.Parallel()

	serverTime := time.Now().UnixMilli()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverTime})
	})

	adapter := newTestAdapter(t, handler)

	ctx := context.Background()
	_ = adapter.Connect(ctx)
	assert.True(t, adapter.IsConnected())

	err := adapter.Disconnect()
	require.NoError(t, err)
	assert.False(t, adapter.IsConnected())
}

func TestAdapter_GetOrderbook(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case "/api/v3/depth":
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"lastUpdateId": 12345,
				"bids": [][]string{
					{"50000.00", "1.5"},
					{"49999.00", "2.0"},
				},
				"asks": [][]string{
					{"50001.00", "1.0"},
					{"50002.00", "3.0"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	orderbook, err := adapter.GetOrderbook(ctx, "BTC/USDT")

	require.NoError(t, err)
	assert.Equal(t, "binance", orderbook.Exchange)
	assert.Equal(t, "BTC/USDT", orderbook.Pair)
	assert.Equal(t, int64(12345), orderbook.Sequence)

	require.Len(t, orderbook.Bids, 2)
	assert.True(t, decimal.NewFromFloat(50000).Equal(orderbook.Bids[0].Price))
	assert.True(t, decimal.NewFromFloat(1.5).Equal(orderbook.Bids[0].Quantity))

	require.Len(t, orderbook.Asks, 2)
	assert.True(t, decimal.NewFromFloat(50001).Equal(orderbook.Asks[0].Price))
	assert.True(t, decimal.NewFromFloat(1.0).Equal(orderbook.Asks[0].Quantity))
}

func TestAdapter_GetOrderbook_NotConnected(t *testing.T) {
	t.Parallel()

	adapter := binance.NewAdapter(binance.Config{})

	ctx := context.Background()
	_, err := adapter.GetOrderbook(ctx, "BTC/USDT")

	require.Error(t, err)
	assert.Equal(t, binance.ErrNotConnected, err)
}

func TestAdapter_PlaceOrder(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case "/api/v3/order":
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			assert.Equal(t, "BUY", r.URL.Query().Get("side"))
			assert.Equal(t, "LIMIT", r.URL.Query().Get("type"))
			assert.NotEmpty(t, r.URL.Query().Get("signature"))

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbol":             "BTCUSDT",
				"orderId":            123456,
				"clientOrderId":      "abc123",
				"price":              "50000.00",
				"origQty":            "0.1",
				"executedQty":        "0.1",
				"cummulativeQuoteQty": "5000.00",
				"status":             "FILLED",
				"type":               "LIMIT",
				"side":               "BUY",
				"transactTime":       time.Now().UnixMilli(),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	order := &domain.Order{
		Exchange: "binance",
		Pair:     "BTC/USDT",
		Side:     domain.OrderSideBuy,
		Type:     domain.OrderTypeLimit,
		Price:    decimal.NewFromFloat(50000),
		Quantity: decimal.NewFromFloat(0.1),
	}

	trade, err := adapter.PlaceOrder(ctx, order)

	require.NoError(t, err)
	assert.Equal(t, "binance", trade.Exchange)
	assert.Equal(t, "BTC/USDT", trade.Pair)
	assert.Equal(t, domain.OrderSideBuy, trade.Side)
	assert.Contains(t, trade.ID, "BTCUSDT:123456")
}

func TestAdapter_PlaceOrder_NotConnected(t *testing.T) {
	t.Parallel()

	adapter := binance.NewAdapter(binance.Config{})

	ctx := context.Background()
	_, err := adapter.PlaceOrder(ctx, &domain.Order{})

	require.Error(t, err)
	assert.Equal(t, binance.ErrNotConnected, err)
}

func TestAdapter_CancelOrder(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case "/api/v3/order":
			if r.Method == http.MethodDelete {
				assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
				assert.Equal(t, "123456", r.URL.Query().Get("orderId"))
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"symbol":  "BTCUSDT",
					"orderId": 123456,
					"status":  "CANCELED",
				})
				return
			}
			w.WriteHeader(http.StatusMethodNotAllowed)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	err := adapter.CancelOrder(ctx, "BTCUSDT:123456")

	require.NoError(t, err)
}

func TestAdapter_CancelOrder_InvalidFormat(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	err := adapter.CancelOrder(ctx, "invalid-order-id")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid order ID format")
}

func TestAdapter_GetOrder(t *testing.T) {
	t.Parallel()

	transactTime := time.Now().UnixMilli()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case "/api/v3/order":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			assert.Equal(t, "123456", r.URL.Query().Get("orderId"))

			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"symbol":        "BTCUSDT",
				"orderId":       123456,
				"price":         "50000.00",
				"origQty":       "0.1",
				"executedQty":   "0.1",
				"status":        "FILLED",
				"type":          "LIMIT",
				"side":          "BUY",
				"transactTime":  transactTime,
				"updateTime":    transactTime,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	order, err := adapter.GetOrder(ctx, "BTCUSDT:123456")

	require.NoError(t, err)
	assert.Equal(t, "binance", order.Exchange)
	assert.Equal(t, "BTC/USDT", order.Pair)
	assert.Equal(t, domain.OrderSideBuy, order.Side)
	assert.Equal(t, domain.OrderTypeLimit, order.Type)
	assert.Equal(t, domain.OrderStatusFilled, order.Status)
	assert.True(t, decimal.NewFromFloat(50000).Equal(order.Price))
	assert.True(t, decimal.NewFromFloat(0.1).Equal(order.Quantity))
}

func TestAdapter_GetBalances(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/time":
			_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
		case "/api/v3/account":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"balances": []map[string]string{
					{"asset": "BTC", "free": "1.5", "locked": "0.5"},
					{"asset": "USDT", "free": "10000.00", "locked": "0"},
					{"asset": "ETH", "free": "0", "locked": "0"},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	balances, err := adapter.GetBalances(ctx)

	require.NoError(t, err)
	assert.Len(t, balances, 2) // ETH has 0 balance, should be excluded

	btcBalance, ok := balances["BTC"]
	require.True(t, ok)
	assert.True(t, decimal.NewFromFloat(1.5).Equal(btcBalance))

	usdtBalance, ok := balances["USDT"]
	require.True(t, ok)
	assert.True(t, decimal.NewFromFloat(10000).Equal(usdtBalance))
}

func TestAdapter_MapError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		errorCode int
		errorMsg  string
		wantErr   error
	}{
		{
			name:      "insufficient funds",
			errorCode: -2010,
			errorMsg:  "Account has insufficient balance",
			wantErr:   binance.ErrInsufficientFunds,
		},
		{
			name:      "order not found",
			errorCode: -2011,
			errorMsg:  "Unknown order sent",
			wantErr:   binance.ErrOrderNotFound,
		},
		{
			name:      "invalid symbol",
			errorCode: -1121,
			errorMsg:  "Invalid symbol",
			wantErr:   binance.ErrPairNotSupported,
		},
		{
			name:      "rate limit - too many orders",
			errorCode: -1015,
			errorMsg:  "Too many new orders",
			wantErr:   binance.ErrRateLimitExceeded,
		},
		{
			name:      "rate limit - too many requests",
			errorCode: -1003,
			errorMsg:  "Too many requests",
			wantErr:   binance.ErrRateLimitExceeded,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/time":
					_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
				case "/api/v3/order":
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"code": tt.errorCode,
						"msg":  tt.errorMsg,
					})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			})

			adapter := newTestAdapter(t, handler)
			ctx := context.Background()
			_ = adapter.Connect(ctx)

			order := &domain.Order{
				Pair:     "BTC/USDT",
				Side:     domain.OrderSideBuy,
				Type:     domain.OrderTypeLimit,
				Price:    decimal.NewFromFloat(50000),
				Quantity: decimal.NewFromFloat(0.1),
			}
			_, err := adapter.PlaceOrder(ctx, order)

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestPairToSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pair   string
		symbol string
	}{
		{"BTC/USDT", "BTCUSDT"},
		{"ETH/BTC", "ETHBTC"},
		{"SOL/USDT", "SOLUSDT"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.pair, func(t *testing.T) {
			t.Parallel()
			// Test via orderbook request
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v3/time":
					_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": time.Now().UnixMilli()})
				case "/api/v3/depth":
					assert.Equal(t, tt.symbol, r.URL.Query().Get("symbol"))
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"lastUpdateId": 1,
						"bids":         [][]string{},
						"asks":         [][]string{},
					})
				}
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			adapter := binance.NewAdapterWithBaseURL(server.URL, binance.Config{
				APIKey:    "test",
				APISecret: "test",
			})
			ctx := context.Background()
			_ = adapter.Connect(ctx)
			_, _ = adapter.GetOrderbook(ctx, tt.pair)
		})
	}
}