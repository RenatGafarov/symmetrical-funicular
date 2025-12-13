package binance_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"arbitragebot/internal/exchange/binance"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         binance.ClientConfig
		wantTestnet bool
	}{
		{
			name: "production mode",
			cfg: binance.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
				Testnet:   false,
				RateLimit: 1200,
			},
			wantTestnet: false,
		},
		{
			name: "testnet mode",
			cfg: binance.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
				Testnet:   true,
				RateLimit: 600,
			},
			wantTestnet: true,
		},
		{
			name: "default rate limit",
			cfg: binance.ClientConfig{
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

			client := binance.NewClient(tt.cfg)
			require.NotNil(t, client)

			if tt.cfg.RateLimit > 0 {
				assert.Equal(t, int64(tt.cfg.RateLimit), client.WeightLimit())
			} else {
				assert.Equal(t, int64(1200), client.WeightLimit())
			}
		})
	}
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	serverTime := time.Now().UnixMilli()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/time", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)

		w.Header().Set("X-MBX-USED-WEIGHT-1M", "10")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]int64{"serverTime": serverTime})
	}))
	defer server.Close()

	client := binance.NewClientWithBaseURL(server.URL, binance.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	result, err := client.GetServerTime(ctx)

	require.NoError(t, err)
	assert.Equal(t, time.UnixMilli(serverTime).Unix(), result.Unix())
}

func TestClient_Request_Unsigned(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/depth", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
		assert.Empty(t, r.URL.Query().Get("signature"))
		assert.Empty(t, r.URL.Query().Get("timestamp"))

		w.Header().Set("X-MBX-USED-WEIGHT-1M", "5")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"lastUpdateId": 123, "bids": [], "asks": []}`))
	}))
	defer server.Close()

	client := binance.NewClientWithBaseURL(server.URL, binance.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	params := map[string]string{"symbol": "BTCUSDT"}
	body, err := client.RequestWithParams(ctx, http.MethodGet, "/api/v3/depth", params, false)

	require.NoError(t, err)
	assert.Contains(t, string(body), "lastUpdateId")
}

func TestClient_Request_Signed(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/account", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.NotEmpty(t, r.URL.Query().Get("signature"))
		assert.NotEmpty(t, r.URL.Query().Get("timestamp"))
		assert.NotEmpty(t, r.URL.Query().Get("recvWindow"))
		assert.Equal(t, "test-key", r.Header.Get("X-MBX-APIKEY"))

		w.Header().Set("X-MBX-USED-WEIGHT-1M", "10")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"balances": []}`))
	}))
	defer server.Close()

	client := binance.NewClientWithBaseURL(server.URL, binance.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
	})

	ctx := context.Background()
	body, err := client.RequestWithParams(ctx, http.MethodGet, "/api/v3/account", nil, true)

	require.NoError(t, err)
	assert.Contains(t, string(body), "balances")
}

func TestClient_Request_APIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		errorCode  int
		errorMsg   string
	}{
		{
			name:       "insufficient balance",
			statusCode: http.StatusBadRequest,
			errorCode:  -2010,
			errorMsg:   "Account has insufficient balance for requested action.",
		},
		{
			name:       "invalid symbol",
			statusCode: http.StatusBadRequest,
			errorCode:  -1121,
			errorMsg:   "Invalid symbol.",
		},
		{
			name:       "rate limit exceeded",
			statusCode: http.StatusTooManyRequests,
			errorCode:  -1003,
			errorMsg:   "Too many requests.",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"code": tt.errorCode,
					"msg":  tt.errorMsg,
				})
			}))
			defer server.Close()

			client := binance.NewClientWithBaseURL(server.URL, binance.ClientConfig{
				APIKey:    "test-key",
				APISecret: "test-secret",
			})

			ctx := context.Background()
			_, err := client.RequestWithParams(ctx, http.MethodGet, "/api/v3/order", nil, true)

			require.Error(t, err)
			apiErr, ok := err.(*binance.APIError)
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
		w.Header().Set("X-MBX-USED-WEIGHT-1M", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := binance.NewClientWithBaseURL(server.URL, binance.ClientConfig{
		APIKey:    "test-key",
		APISecret: "test-secret",
		RateLimit: 50, // Low limit to trigger rate limiting
	})

	ctx := context.Background()

	// First request should succeed and update weight to 100
	_, err := client.RequestWithParams(ctx, http.MethodGet, "/test", nil, false)
	require.NoError(t, err)
	assert.Equal(t, int64(100), client.UsedWeight())

	// Second request should fail due to rate limit (100 >= 50)
	_, err = client.RequestWithParams(ctx, http.MethodGet, "/test", nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	err := &binance.APIError{
		Code:    -2010,
		Message: "Account has insufficient balance",
	}

	assert.Equal(t, "binance api error -2010: Account has insufficient balance", err.Error())
}