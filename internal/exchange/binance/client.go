// Package binance provides a Binance Spot exchange adapter.
package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// BaseURL is the production Binance API endpoint.
	BaseURL = "https://api.binance.com"
	// TestnetBaseURL is the testnet Binance API endpoint.
	TestnetBaseURL = "https://testnet.binance.vision"

	// defaultRecvWindow is the default receive window for signed requests (milliseconds).
	defaultRecvWindow = 5000
)

// Client is an HTTP client for the Binance Spot API.
// It handles request signing, rate limiting, and error handling.
type Client struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
	recvWindow int64
	logger     *zap.Logger

	// Rate limiting
	usedWeight   atomic.Int64
	weightLimit  int64
	rateLimitMu  sync.Mutex
	lastResetAt  time.Time
}

// ClientConfig holds configuration for creating a new Client.
type ClientConfig struct {
	// APIKey is the Binance API key.
	APIKey string
	// APISecret is the Binance API secret for signing requests.
	APISecret string
	// Testnet enables testnet mode.
	Testnet bool
	// RateLimit is the maximum request weight per minute.
	RateLimit int
	// Logger is the logger instance.
	Logger *zap.Logger
}

// NewClient creates a new Binance API client.
func NewClient(cfg ClientConfig) *Client {
	baseURL := BaseURL
	if cfg.Testnet {
		baseURL = TestnetBaseURL
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	weightLimit := int64(1200)
	if cfg.RateLimit > 0 {
		weightLimit = int64(cfg.RateLimit)
	}

	return &Client{
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		recvWindow:  defaultRecvWindow,
		logger:      logger,
		weightLimit: weightLimit,
		lastResetAt: time.Now(),
	}
}

// sign creates an HMAC-SHA256 signature for the given query string.
func (c *Client) sign(queryString string) string {
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

// Request sends an HTTP request to the Binance API.
// If signed is true, the request will include timestamp and signature.
func (c *Client) Request(ctx context.Context, method, endpoint string, params url.Values, signed bool) ([]byte, error) {
	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	if params == nil {
		params = url.Values{}
	}

	if signed {
		params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		params.Set("recvWindow", strconv.FormatInt(c.recvWindow, 10))
		params.Set("signature", c.sign(params.Encode()))
	}

	reqURL := c.baseURL + endpoint
	if len(params) > 0 && method == http.MethodGet {
		reqURL += "?" + params.Encode()
	}

	var body io.Reader
	if method != http.MethodGet && len(params) > 0 {
		body = nil // For POST/DELETE, params go in URL for Binance
		reqURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	c.logger.Debug("sending request",
		zap.String("method", method),
		zap.String("endpoint", endpoint),
		zap.Bool("signed", signed))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	c.updateRateLimit(resp.Header)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseError(resp.StatusCode, respBody)
	}

	return respBody, nil
}

// checkRateLimit verifies we haven't exceeded the rate limit.
func (c *Client) checkRateLimit() error {
	c.rateLimitMu.Lock()
	defer c.rateLimitMu.Unlock()

	// Reset counter every minute
	if time.Since(c.lastResetAt) > time.Minute {
		c.usedWeight.Store(0)
		c.lastResetAt = time.Now()
	}

	if c.usedWeight.Load() >= c.weightLimit {
		return fmt.Errorf("rate limit exceeded: %d/%d", c.usedWeight.Load(), c.weightLimit)
	}

	return nil
}

// updateRateLimit updates the rate limit counter from response headers.
func (c *Client) updateRateLimit(headers http.Header) {
	if weight := headers.Get("X-MBX-USED-WEIGHT-1M"); weight != "" {
		if w, err := strconv.ParseInt(weight, 10, 64); err == nil {
			c.usedWeight.Store(w)
			c.logger.Debug("rate limit updated", zap.Int64("used_weight", w))
		}
	}
}

// APIError represents a Binance API error response.
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("binance api error %d: %s", e.Code, e.Message)
}

// parseError parses an error response from Binance.
func (c *Client) parseError(statusCode int, body []byte) error {
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("http %d: %s", statusCode, string(body))
	}

	c.logger.Warn("api error",
		zap.Int("code", apiErr.Code),
		zap.String("message", apiErr.Message))

	return &apiErr
}

// GetServerTime fetches the current server time from Binance.
// This can be used to check connectivity and clock synchronization.
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
	body, err := c.Request(ctx, http.MethodGet, "/api/v3/time", nil, false)
	if err != nil {
		return time.Time{}, err
	}

	var resp struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return time.Time{}, fmt.Errorf("parse response: %w", err)
	}

	return time.UnixMilli(resp.ServerTime), nil
}

// UsedWeight returns the current used request weight.
func (c *Client) UsedWeight() int64 {
	return c.usedWeight.Load()
}

// WeightLimit returns the maximum request weight per minute.
func (c *Client) WeightLimit() int64 {
	return c.weightLimit
}

// NewClientWithBaseURL creates a new Binance API client with a custom base URL.
// This is primarily used for testing with mock servers.
func NewClientWithBaseURL(baseURL string, cfg ClientConfig) *Client {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	weightLimit := int64(1200)
	if cfg.RateLimit > 0 {
		weightLimit = int64(cfg.RateLimit)
	}

	return &Client{
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		recvWindow:  defaultRecvWindow,
		logger:      logger,
		weightLimit: weightLimit,
		lastResetAt: time.Now(),
	}
}

// RequestWithParams is a convenience method that accepts a map of parameters.
func (c *Client) RequestWithParams(ctx context.Context, method, endpoint string, params map[string]string, signed bool) ([]byte, error) {
	urlParams := url.Values{}
	for k, v := range params {
		urlParams.Set(k, v)
	}
	return c.Request(ctx, method, endpoint, urlParams, signed)
}