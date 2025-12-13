package bybit_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"arbitragebot/internal/exchange/bybit"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         bybit.ClientConfig
		wantTestnet bool
	}{
		{
			name: "production mode",
			cfg: bybit.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
				Testnet:   false,
				RateLimit: 600,
			},
			wantTestnet: false,
		},
		{
			name: "testnet mode",
			cfg: bybit.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
				Testnet:   true,
				RateLimit: 300,
			},
			wantTestnet: true,
		},
		{
			name: "default rate limit",
			cfg: bybit.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
			},
			wantTestnet: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := bybit.NewClient(tt.cfg)
			require.NotNil(t, client)

			if tt.cfg.RateLimit > 0 {
				assert.Equal(t, int64(tt.cfg.RateLimit), client.RateLimit())
			} else {
				assert.Equal(t, int64(600), client.RateLimit())
			}
		})
	}
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	serverTime := time.Now().Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v5/market/time", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"retCode": 0,
			"retMsg":  "OK",
			"result": map[string]string{
				"timeSecond": strconv.FormatInt(time.Now().Unix(), 10),
				"timeNano":   "0",
			},
		})
	}))
	defer server.Close()

	client := bybit.NewClientWithBaseURL(server.URL, bybit.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	result, err := client.GetServerTime(ctx)

	require.NoError(t, err)
	assert.InDelta(t, serverTime, result.Unix(), 5) // Allow 5 second tolerance
}

func TestClient_Request_Unsigned(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v5/market/orderbook", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "spot", r.URL.Query().Get("category"))
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		assert.Empty(t, r.Header.Get("X-BAPI-SIGN"))

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"retCode": 0,
			"retMsg":  "OK",
			"result": map[string]interface{}{
				"s": "BTCUSDT",
				"b": [][]string{},
				"a": [][]string{},
			},
		})
	}))
	defer server.Close()

	client := bybit.NewClientWithBaseURL(server.URL, bybit.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	params := map[string]string{
		"category": "spot",
		"symbol":   "BTCUSDT",
	}
	body, err := client.RequestWithParams(ctx, http.MethodGet, "/v5/market/orderbook", params, false)

	require.NoError(t, err)
	assert.Contains(t, string(body), "BTCUSDT")
}

func TestClient_Request_Signed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v5/account/wallet-balance", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.NotEmpty(t, r.Header.Get("X-BAPI-SIGN"))
		assert.NotEmpty(t, r.Header.Get("X-BAPI-TIMESTAMP"))
		assert.NotEmpty(t, r.Header.Get("X-BAPI-RECV-WINDOW"))
		assert.Equal(t, "test-key", r.Header.Get("X-BAPI-API-KEY"))

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"retCode": 0,
			"retMsg":  "OK",
			"result": map[string]interface{}{
				"list": []interface{}{},
			},
		})
	}))
	defer server.Close()

	client := bybit.NewClientWithBaseURL(server.URL, bybit.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	params := map[string]string{"accountType": "UNIFIED"}
	body, err := client.RequestWithParams(ctx, http.MethodGet, "/v5/account/wallet-balance", params, true)

	require.NoError(t, err)
	assert.Contains(t, string(body), "list")
}

func TestClient_Request_APIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		errorCode int
		errorMsg  string
	}{
		{
			name:      "insufficient balance",
			errorCode: 110007,
			errorMsg:  "Insufficient balance",
		},
		{
			name:      "order not found",
			errorCode: 110001,
			errorMsg:  "Order does not exist",
		},
		{
			name:      "invalid symbol",
			errorCode: 10001,
			errorMsg:  "Invalid symbol",
		},
		{
			name:      "rate limit exceeded",
			errorCode: 10006,
			errorMsg:  "Too many requests",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK) // Bybit returns 200 for all responses
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"retCode": tt.errorCode,
					"retMsg":  tt.errorMsg,
				})
			}))
			defer server.Close()

			client := bybit.NewClientWithBaseURL(server.URL, bybit.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
			})

			ctx := context.Background()
			_, err := client.RequestWithParams(ctx, http.MethodGet, "/v5/order/realtime", nil, true)

			require.Error(t, err)
			apiErr, ok := err.(*bybit.APIError)
			require.True(t, ok, "expected APIError, got %T", err)
			assert.Equal(t, tt.errorCode, apiErr.Code)
			assert.Equal(t, tt.errorMsg, apiErr.Message)
		})
	}
}

func TestClient_RateLimit(t *testing.T) {
	t.Parallel()

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"retCode": 0,
			"retMsg":  "OK",
			"result":  map[string]interface{}{},
		})
	}))
	defer server.Close()

	client := bybit.NewClientWithBaseURL(server.URL, bybit.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
		RateLimit: 1, // Low limit to trigger rate limiting
	})

	ctx := context.Background()

	// First request should succeed
	_, err := client.RequestWithParams(ctx, http.MethodGet, "/test", nil, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1), client.RequestCount())

	// Second request should fail due to rate limit
	_, err = client.RequestWithParams(ctx, http.MethodGet, "/test", nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	err := &bybit.APIError{
		Code:    110007,
		Message: "Insufficient balance",
	}

	assert.Equal(t, "bybit api error 110007: Insufficient balance", err.Error())
}