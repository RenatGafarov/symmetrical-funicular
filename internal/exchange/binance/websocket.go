package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"arbitragebot/internal/domain"
)

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second
	// defaultPingInterval is the default interval to send ping messages.
	defaultPingInterval = 20 * time.Second
	// defaultReconnectDelay is the default delay before reconnecting.
	defaultReconnectDelay = 5 * time.Second
)

// WebSocketConfig holds configuration for the WebSocket manager.
type WebSocketConfig struct {
	// URL is the WebSocket server URL.
	URL string
	// Pairs is the list of trading pairs to subscribe.
	Pairs []string
	// PingInterval is the interval between ping messages.
	PingInterval time.Duration
	// ReconnectDelay is the delay before attempting reconnection.
	ReconnectDelay time.Duration
	// Logger is the logger instance.
	Logger *zap.Logger
}

// WebSocketManager manages WebSocket connections to Binance.
type WebSocketManager struct {
	config     WebSocketConfig
	conn       *websocket.Conn
	orderbooks chan *domain.Orderbook
	done       chan struct{}
	mu         sync.Mutex
	logger     *zap.Logger
}

// NewWebSocketManager creates a new WebSocket manager.
func NewWebSocketManager(cfg WebSocketConfig) *WebSocketManager {
	logger := cfg.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	if cfg.PingInterval == 0 {
		cfg.PingInterval = defaultPingInterval
	}
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = defaultReconnectDelay
	}

	return &WebSocketManager{
		config:     cfg,
		orderbooks: make(chan *domain.Orderbook, 100),
		done:       make(chan struct{}),
		logger:     logger,
	}
}

// Subscribe connects to WebSocket and returns a channel of orderbook updates.
func (m *WebSocketManager) Subscribe(ctx context.Context) (<-chan *domain.Orderbook, error) {
	if err := m.connect(ctx); err != nil {
		return nil, err
	}

	go m.readLoop(ctx)
	go m.pingLoop(ctx)

	return m.orderbooks, nil
}

// Close closes the WebSocket connection.
func (m *WebSocketManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	close(m.done)

	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
}

// connect establishes a WebSocket connection.
func (m *WebSocketManager) connect(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build stream names: btcusdt@depth20@100ms
	streams := make([]string, 0, len(m.config.Pairs))
	for _, pair := range m.config.Pairs {
		symbol := strings.ToLower(pairToSymbol(pair))
		streams = append(streams, fmt.Sprintf("%s@depth20@100ms", symbol))
	}

	// Combined stream URL
	url := fmt.Sprintf("%s/%s", m.config.URL, strings.Join(streams, "/"))
	if len(streams) > 1 {
		url = fmt.Sprintf("%s/stream?streams=%s", m.config.URL, strings.Join(streams, "/"))
	}

	m.logger.Info("connecting to websocket", zap.String("url", url))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	m.conn = conn
	m.logger.Info("websocket connected", zap.Int("streams", len(streams)))

	return nil
}

// readLoop continuously reads messages from WebSocket.
func (m *WebSocketManager) readLoop(ctx context.Context) {
	defer func() {
		m.mu.Lock()
		if m.conn != nil {
			m.conn.Close()
		}
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		default:
		}

		m.mu.Lock()
		conn := m.conn
		m.mu.Unlock()

		if conn == nil {
			return
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.logger.Error("websocket read error", zap.Error(err))
			}

			// Attempt reconnect
			if err := m.reconnect(ctx); err != nil {
				m.logger.Error("reconnect failed", zap.Error(err))
				return
			}
			continue
		}

		ob, err := m.parseMessage(message)
		if err != nil {
			m.logger.Warn("parse message error", zap.Error(err))
			continue
		}

		if ob != nil {
			select {
			case m.orderbooks <- ob:
			default:
				m.logger.Warn("orderbook channel full, dropping update")
			}
		}
	}
}

// pingLoop sends periodic ping messages.
func (m *WebSocketManager) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(m.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.done:
			return
		case <-ticker.C:
			m.mu.Lock()
			conn := m.conn
			m.mu.Unlock()

			if conn == nil {
				return
			}

			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				m.logger.Warn("ping error", zap.Error(err))
			}
		}
	}
}

// reconnect attempts to reconnect to WebSocket.
func (m *WebSocketManager) reconnect(ctx context.Context) error {
	m.mu.Lock()
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	m.mu.Unlock()

	m.logger.Info("reconnecting in", zap.Duration("delay", m.config.ReconnectDelay))

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.done:
		return fmt.Errorf("websocket closed")
	case <-time.After(m.config.ReconnectDelay):
	}

	return m.connect(ctx)
}

// depthUpdate represents a Binance depth stream message.
type depthUpdate struct {
	Stream string `json:"stream"`
	Data   struct {
		EventType     string     `json:"e"`
		EventTime     int64      `json:"E"`
		Symbol        string     `json:"s"`
		FirstUpdateID int64      `json:"U"`
		FinalUpdateID int64      `json:"u"`
		Bids          [][]string `json:"b"`
		Asks          [][]string `json:"a"`
	} `json:"data"`
}

// singleDepthUpdate for single stream (non-combined).
type singleDepthUpdate struct {
	EventType     string     `json:"e"`
	EventTime     int64      `json:"E"`
	Symbol        string     `json:"s"`
	FirstUpdateID int64      `json:"U"`
	FinalUpdateID int64      `json:"u"`
	Bids          [][]string `json:"b"`
	Asks          [][]string `json:"a"`
}

// parseMessage parses a WebSocket message into an orderbook.
func (m *WebSocketManager) parseMessage(data []byte) (*domain.Orderbook, error) {
	// Try combined stream format first
	var combined depthUpdate
	if err := json.Unmarshal(data, &combined); err == nil && combined.Stream != "" {
		return m.parseDepthData(
			combined.Data.Symbol,
			combined.Data.FinalUpdateID,
			combined.Data.EventTime,
			combined.Data.Bids,
			combined.Data.Asks,
		), nil
	}

	// Try single stream format
	var single singleDepthUpdate
	if err := json.Unmarshal(data, &single); err == nil && single.EventType == "depthUpdate" {
		return m.parseDepthData(
			single.Symbol,
			single.FinalUpdateID,
			single.EventTime,
			single.Bids,
			single.Asks,
		), nil
	}

	return nil, fmt.Errorf("unknown message format")
}

// parseDepthData converts raw depth data to domain.Orderbook.
func (m *WebSocketManager) parseDepthData(symbol string, sequence, eventTime int64, bids, asks [][]string) *domain.Orderbook {
	parseLevels := func(levels [][]string) []domain.PriceLevel {
		result := make([]domain.PriceLevel, 0, len(levels))
		for _, level := range levels {
			if len(level) < 2 {
				continue
			}
			price, err := decimal.NewFromString(level[0])
			if err != nil {
				continue
			}
			qty, err := decimal.NewFromString(level[1])
			if err != nil {
				continue
			}
			// Skip zero quantity (removed level)
			if qty.IsZero() {
				continue
			}
			result = append(result, domain.PriceLevel{Price: price, Quantity: qty})
		}
		return result
	}

	return &domain.Orderbook{
		Exchange:  "binance",
		Pair:      symbolToPair(symbol),
		Bids:      parseLevels(bids),
		Asks:      parseLevels(asks),
		Timestamp: time.UnixMilli(eventTime),
		Sequence:  sequence,
	}
}