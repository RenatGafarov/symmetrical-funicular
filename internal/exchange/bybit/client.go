// Package bybit provides a Bybit Spot exchange adapter.
package bybit

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
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// BaseURL is the production Bybit API endpoint.
	BaseURL = "https://api.bybit.com"
	// TestnetBaseURL is the testnet Bybit API endpoint.
	TestnetBaseURL = "https://api-testnet.bybit.com"

	// defaultRecvWindow is the default receive window for signed requests (milliseconds).
	defaultRecvWindow = 5000
)

// Client is an HTTP client for the Bybit V5 API.
// It handles request signing, rate limiting, and error handling.
type Client struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
	recvWindow int64
	logger     *zap.Logger

	// Rate limiting (600 requests per 5 seconds)
	requestCount atomic.Int64
	rateLimitMu  sync.Mutex
	windowStart  time.Time
	rateLimit    int64
}

// ClientConfig holds configuration for creating a new Client.
type ClientConfig struct {
	// APIKey is the Bybit API key.
	APIKey string
	// APISecret is the Bybit API secret for signing requests.
	APISecret string
	// Testnet enables testnet mode.
	Testnet bool
	// RateLimit is the maximum requests per 5 seconds.
	RateLimit int
	// Logger is the logger instance.
	Logger *zap.Logger
}

// NewClient creates a new Bybit API client.
func NewClient(cfg ClientConfig) *Client {
	baseURL := BaseURL
	if cfg.Testnet {
		baseURL = TestnetBaseURL
	}

	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	rateLimit := int64(600)
	if cfg.RateLimit > 0 {
		rateLimit = int64(cfg.RateLimit)
	}

	return &Client{
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		recvWindow:  defaultRecvWindow,
		logger:      logger,
		windowStart: time.Now(),
		rateLimit:   rateLimit,
	}
}

// sign creates an HMAC-SHA256 signature for Bybit V5 API.
// Signature format: timestamp + apiKey + recvWindow + queryString (for GET) or jsonBody (for POST)
func (c *Client) sign(timestamp int64, payload string) string {
	signPayload := fmt.Sprintf("%d%s%d%s", timestamp, c.apiKey, c.recvWindow, payload)
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(signPayload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Request sends an HTTP request to the Bybit API.
// If signed is true, the request will include authentication headers.
func (c *Client) Request(ctx context.Context, method, endpoint string, params url.Values, signed bool) ([]byte, error) {
	if err := c.checkRateLimit(); err != nil {
		return nil, err
	}

	if params == nil {
		params = url.Values{}
	}

	reqURL := c.baseURL + endpoint
	var body io.Reader
	var payload string

	timestamp := time.Now().UnixMilli()

	if method == http.MethodGet {
		// Sort parameters for consistent signing
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		var sortedParams []string
		for _, k := range keys {
			sortedParams = append(sortedParams, fmt.Sprintf("%s=%s", k, params.Get(k)))
		}
		payload = strings.Join(sortedParams, "&")

		if len(params) > 0 {
			reqURL += "?" + payload
		}
	} else {
		// For POST requests, use JSON body
		jsonBody := make(map[string]interface{})
		for k, v := range params {
			if len(v) > 0 {
				jsonBody[k] = v[0]
			}
		}
		jsonBytes, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, fmt.Errorf("marshal json: %w", err)
		}
		payload = string(jsonBytes)
		body = strings.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}

	if signed {
		signature := c.sign(timestamp, payload)
		req.Header.Set("X-BAPI-API-KEY", c.apiKey)
		req.Header.Set("X-BAPI-TIMESTAMP", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-BAPI-SIGN", signature)
		req.Header.Set("X-BAPI-RECV-WINDOW", strconv.FormatInt(c.recvWindow, 10))
	}

	c.logger.Debug("sending request",
		zap.String("method", method),
		zap.String("endpoint", endpoint),
		zap.Bool("signed", signed))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	c.incrementRequestCount()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Bybit returns 200 for all responses, errors are in the body
	var baseResp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
	}
	if err := json.Unmarshal(respBody, &baseResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if baseResp.RetCode != 0 {
		return nil, c.parseError(baseResp.RetCode, baseResp.RetMsg)
	}

	return respBody, nil
}

// checkRateLimit verifies we haven't exceeded the rate limit.
// Bybit uses 600 requests per 5-second window.
func (c *Client) checkRateLimit() error {
	c.rateLimitMu.Lock()
	defer c.rateLimitMu.Unlock()

	// Reset counter every 5 seconds
	if time.Since(c.windowStart) > 5*time.Second {
		c.requestCount.Store(0)
		c.windowStart = time.Now()
	}

	if c.requestCount.Load() >= c.rateLimit {
		return fmt.Errorf("rate limit exceeded: %d/%d per 5s", c.requestCount.Load(), c.rateLimit)
	}

	return nil
}

// incrementRequestCount increments the request counter.
func (c *Client) incrementRequestCount() {
	c.requestCount.Add(1)
}

// APIError represents a Bybit API error response.
type APIError struct {
	Code    int    `json:"retCode"`
	Message string `json:"retMsg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bybit api error %d: %s", e.Code, e.Message)
}

// parseError creates an APIError from response.
func (c *Client) parseError(code int, message string) error {
	c.logger.Warn("api error",
		zap.Int("code", code),
		zap.String("message", message))

	return &APIError{
		Code:    code,
		Message: message,
	}
}

// GetServerTime fetches the current server time from Bybit.
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
	body, err := c.Request(ctx, http.MethodGet, "/v5/market/time", nil, false)
	if err != nil {
		return time.Time{}, err
	}

	var resp struct {
		Result struct {
			TimeSecond string `json:"timeSecond"`
			TimeNano   string `json:"timeNano"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return time.Time{}, fmt.Errorf("parse response: %w", err)
	}

	seconds, err := strconv.ParseInt(resp.Result.TimeSecond, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time: %w", err)
	}

	return time.Unix(seconds, 0), nil
}

// RequestCount returns the current request count in the window.
func (c *Client) RequestCount() int64 {
	return c.requestCount.Load()
}

// RateLimit returns the maximum requests per 5 seconds.
func (c *Client) RateLimit() int64 {
	return c.rateLimit
}

// NewClientWithBaseURL creates a new Bybit API client with a custom base URL.
// This is primarily used for testing with mock servers.
func NewClientWithBaseURL(baseURL string, cfg ClientConfig) *Client {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	rateLimit := int64(600)
	if cfg.RateLimit > 0 {
		rateLimit = int64(cfg.RateLimit)
	}

	return &Client{
		apiKey:      cfg.APIKey,
		apiSecret:   cfg.APISecret,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		recvWindow:  defaultRecvWindow,
		logger:      logger,
		windowStart: time.Now(),
		rateLimit:   rateLimit,
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