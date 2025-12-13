package bybit_test

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
	"arbitragebot/internal/exchange/bybit"
)

func newTestAdapter(t *testing.T, handler http.Handler) *bybit.Adapter {
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return bybit.NewAdapterWithBaseURL(server.URL, bybit.Config{
		APIKey:    "test-key",
		APISecret: "test-secret",
		FeeMaker:  decimal.NewFromFloat(0.001),
		FeeTaker:  decimal.NewFromFloat(0.0006),
		Pairs:     []string{"BTC/USDT", "ETH/USDT"},
	})
}

func okResponse(result interface{}) map[string]interface{} {
	return map[string]interface{}{
		"retCode": 0,
		"retMsg":  "OK",
		"result":  result,
	}
}

func TestAdapter_Name(t *testing.T) {
	t.Parallel()

	adapter := bybit.NewAdapter(bybit.Config{})
	assert.Equal(t, "bybit", adapter.Name())
}

func TestAdapter_SupportedPairs(t *testing.T) {
	t.Parallel()

	pairs := []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"}
	adapter := bybit.NewAdapter(bybit.Config{
		Pairs: pairs,
	})

	assert.Equal(t, pairs, adapter.SupportedPairs())
}

func TestAdapter_GetFees(t *testing.T) {
	t.Parallel()

	adapter := bybit.NewAdapter(bybit.Config{
		FeeMaker: decimal.NewFromFloat(0.0001),
		FeeTaker: decimal.NewFromFloat(0.0006),
	})

	fees := adapter.GetFees("BTC/USDT")

	assert.True(t, decimal.NewFromFloat(0.0001).Equal(fees.Maker))
	assert.True(t, decimal.NewFromFloat(0.0006).Equal(fees.Taker))
}

func TestAdapter_Connect(t *testing.T) {
	t.Parallel()

	serverTime := time.Now().Unix()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v5/market/time" {
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	adapter := newTestAdapter(t, handler)

	ctx := context.Background()
	err := adapter.Connect(ctx)

	require.NoError(t, err)
	assert.True(t, adapter.IsConnected())
	_ = serverTime
}

func TestAdapter_Connect_Error(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"retCode": 10001,
			"retMsg":  "params error",
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

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
			"timeSecond": time.Now().Format("1136239445"),
			"timeNano":   "0",
		}))
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
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/market/orderbook":
			assert.Equal(t, "spot", r.URL.Query().Get("category"))
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"s":  "BTCUSDT",
				"ts": time.Now().UnixMilli(),
				"u":  12345,
				"b": [][]string{
					{"50000.00", "1.5"},
					{"49999.00", "2.0"},
				},
				"a": [][]string{
					{"50001.00", "1.0"},
					{"50002.00", "3.0"},
				},
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	orderbook, err := adapter.GetOrderbook(ctx, "BTC/USDT")

	require.NoError(t, err)
	assert.Equal(t, "bybit", orderbook.Exchange)
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

	adapter := bybit.NewAdapter(bybit.Config{})

	ctx := context.Background()
	_, err := adapter.GetOrderbook(ctx, "BTC/USDT")

	require.Error(t, err)
	assert.Equal(t, bybit.ErrNotConnected, err)
}

func TestAdapter_PlaceOrder(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/order/create":
			assert.Equal(t, http.MethodPost, r.Method)
			assert.NotEmpty(t, r.Header.Get("X-BAPI-SIGN"))

			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"orderId":     "1234567890",
				"orderLinkId": "abc123",
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	order := &domain.Order{
		Exchange: "bybit",
		Pair:     "BTC/USDT",
		Side:     domain.OrderSideBuy,
		Type:     domain.OrderTypeLimit,
		Price:    decimal.NewFromFloat(50000),
		Quantity: decimal.NewFromFloat(0.1),
	}

	trade, err := adapter.PlaceOrder(ctx, order)

	require.NoError(t, err)
	assert.Equal(t, "bybit", trade.Exchange)
	assert.Equal(t, "BTC/USDT", trade.Pair)
	assert.Equal(t, domain.OrderSideBuy, trade.Side)
	assert.Contains(t, trade.ID, "BTCUSDT:1234567890")
}

func TestAdapter_PlaceOrder_NotConnected(t *testing.T) {
	t.Parallel()

	adapter := bybit.NewAdapter(bybit.Config{})

	ctx := context.Background()
	_, err := adapter.PlaceOrder(ctx, &domain.Order{})

	require.Error(t, err)
	assert.Equal(t, bybit.ErrNotConnected, err)
}

func TestAdapter_CancelOrder(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/order/cancel":
			assert.Equal(t, http.MethodPost, r.Method)
			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"orderId":     "1234567890",
				"orderLinkId": "",
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	err := adapter.CancelOrder(ctx, "BTCUSDT:1234567890")

	require.NoError(t, err)
}

func TestAdapter_CancelOrder_InvalidFormat(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
			"timeSecond": time.Now().Format("1136239445"),
			"timeNano":   "0",
		}))
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

	createdTime := time.Now().UnixMilli()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/order/realtime":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "spot", r.URL.Query().Get("category"))
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			assert.Equal(t, "1234567890", r.URL.Query().Get("orderId"))

			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"list": []map[string]interface{}{
					{
						"orderId":     "1234567890",
						"symbol":      "BTCUSDT",
						"side":        "Buy",
						"orderType":   "Limit",
						"price":       "50000.00",
						"qty":         "0.1",
						"cumExecQty":  "0.1",
						"orderStatus": "Filled",
						"createdTime": time.Now().Format("2006-01-02T15:04:05.000Z"),
						"updatedTime": time.Now().Format("2006-01-02T15:04:05.000Z"),
					},
				},
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	order, err := adapter.GetOrder(ctx, "BTCUSDT:1234567890")

	require.NoError(t, err)
	assert.Equal(t, "bybit", order.Exchange)
	assert.Equal(t, "BTC/USDT", order.Pair)
	assert.Equal(t, domain.OrderSideBuy, order.Side)
	assert.Equal(t, domain.OrderTypeLimit, order.Type)
	assert.Equal(t, domain.OrderStatusFilled, order.Status)
	assert.True(t, decimal.NewFromFloat(50000).Equal(order.Price))
	assert.True(t, decimal.NewFromFloat(0.1).Equal(order.Quantity))
	_ = createdTime
}

func TestAdapter_GetOrder_NotFound(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/order/realtime":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"list": []interface{}{},
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	adapter := newTestAdapter(t, handler)
	ctx := context.Background()
	_ = adapter.Connect(ctx)

	_, err := adapter.GetOrder(ctx, "BTCUSDT:nonexistent")

	require.Error(t, err)
	assert.Equal(t, bybit.ErrOrderNotFound, err)
}

func TestAdapter_GetBalances(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/market/time":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
				"timeSecond": time.Now().Format("1136239445"),
				"timeNano":   "0",
			}))
		case "/v5/account/wallet-balance":
			_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
				"list": []map[string]interface{}{
					{
						"accountType": "UNIFIED",
						"coin": []map[string]string{
							{"coin": "BTC", "walletBalance": "1.5", "free": "1.5"},
							{"coin": "USDT", "walletBalance": "10000.00", "free": "10000.00"},
							{"coin": "ETH", "walletBalance": "0", "free": "0"},
						},
					},
				},
			}))
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
			errorCode: 110007,
			errorMsg:  "Insufficient balance",
			wantErr:   bybit.ErrInsufficientFunds,
		},
		{
			name:      "order not found",
			errorCode: 110001,
			errorMsg:  "Order does not exist",
			wantErr:   bybit.ErrOrderNotFound,
		},
		{
			name:      "invalid symbol",
			errorCode: 10001,
			errorMsg:  "Invalid symbol",
			wantErr:   bybit.ErrPairNotSupported,
		},
		{
			name:      "rate limit exceeded",
			errorCode: 10006,
			errorMsg:  "Too many requests",
			wantErr:   bybit.ErrRateLimitExceeded,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v5/market/time":
					_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
						"timeSecond": time.Now().Format("1136239445"),
						"timeNano":   "0",
					}))
				case "/v5/order/create":
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"retCode": tt.errorCode,
						"retMsg":  tt.errorMsg,
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
				case "/v5/market/time":
					_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
						"timeSecond": time.Now().Format("1136239445"),
						"timeNano":   "0",
					}))
				case "/v5/market/orderbook":
					assert.Equal(t, tt.symbol, r.URL.Query().Get("symbol"))
					_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
						"s":  tt.symbol,
						"ts": time.Now().UnixMilli(),
						"u":  1,
						"b":  [][]string{},
						"a":  [][]string{},
					}))
				}
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			adapter := bybit.NewAdapterWithBaseURL(server.URL, bybit.Config{
				APIKey:    "test",
				APISecret: "test",
			})
			ctx := context.Background()
			_ = adapter.Connect(ctx)
			_, _ = adapter.GetOrderbook(ctx, tt.pair)
		})
	}
}

func TestSymbolToPair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		orderID  string
		wantPair string
	}{
		{
			name:     "USDT quote",
			orderID:  "BTCUSDT:123",
			wantPair: "BTC/USDT",
		},
		{
			name:     "USDC quote",
			orderID:  "ETHUSDC:456",
			wantPair: "ETH/USDC",
		},
		{
			name:     "BTC quote",
			orderID:  "ETHBTC:789",
			wantPair: "ETH/BTC",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v5/market/time":
					_ = json.NewEncoder(w).Encode(okResponse(map[string]string{
						"timeSecond": time.Now().Format("1136239445"),
						"timeNano":   "0",
					}))
				case "/v5/order/realtime":
					_ = json.NewEncoder(w).Encode(okResponse(map[string]interface{}{
						"list": []map[string]interface{}{
							{
								"orderId":     "123",
								"symbol":      r.URL.Query().Get("symbol"),
								"side":        "Buy",
								"orderType":   "Limit",
								"price":       "50000.00",
								"qty":         "0.1",
								"cumExecQty":  "0.1",
								"orderStatus": "Filled",
								"createdTime": time.Now().Format("2006-01-02T15:04:05.000Z"),
								"updatedTime": time.Now().Format("2006-01-02T15:04:05.000Z"),
							},
						},
					}))
				}
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			adapter := bybit.NewAdapterWithBaseURL(server.URL, bybit.Config{
				APIKey:    "test",
				APISecret: "test",
			})
			ctx := context.Background()
			_ = adapter.Connect(ctx)

			order, err := adapter.GetOrder(ctx, tt.orderID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantPair, order.Pair)
		})
	}
}