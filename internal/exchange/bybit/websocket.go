package bybit

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

// WebSocketManager manages WebSocket connections to Bybit.
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

	if err := m.subscribe(); err != nil {
		m.conn.Close()
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

	m.logger.Info("connecting to websocket", zap.String("url", m.config.URL))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, m.config.URL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket: %w", err)
	}

	m.conn = conn
	m.logger.Info("websocket connected")

	return nil
}

// subscribe sends subscription requests for orderbook topics.
func (m *WebSocketManager) subscribe() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Build topic list: orderbook.50.BTCUSDT
	topics := make([]string, 0, len(m.config.Pairs))
	for _, pair := range m.config.Pairs {
		symbol := strings.ToUpper(pairToSymbol(pair))
		topics = append(topics, fmt.Sprintf("orderbook.50.%s", symbol))
	}

	subMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": topics,
	}

	if err := m.conn.WriteJSON(subMsg); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	m.logger.Info("subscribed to topics", zap.Strings("topics", topics))
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
			// Not all messages are orderbook updates (e.g., pong responses)
			m.logger.Debug("parse message", zap.Error(err))
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

			// Bybit uses JSON ping
			pingMsg := map[string]interface{}{
				"op":     "ping",
				"req_id": fmt.Sprintf("%d", time.Now().UnixMilli()),
			}

			if err := conn.WriteJSON(pingMsg); err != nil {
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

	if err := m.connect(ctx); err != nil {
		return err
	}

	return m.subscribe()
}

// orderbookMessage represents a Bybit orderbook WebSocket message.
type orderbookMessage struct {
	Topic string `json:"topic"`
	Type  string `json:"type"` // snapshot or delta
	Ts    int64  `json:"ts"`
	Data  struct {
		Symbol   string     `json:"s"`
		Bids     [][]string `json:"b"`
		Asks     [][]string `json:"a"`
		UpdateID int64      `json:"u"`
		Seq      int64      `json:"seq"`
	} `json:"data"`
}

// parseMessage parses a WebSocket message into an orderbook.
func (m *WebSocketManager) parseMessage(data []byte) (*domain.Orderbook, error) {
	var msg orderbookMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	// Check if it's an orderbook message
	if !strings.HasPrefix(msg.Topic, "orderbook.") {
		return nil, fmt.Errorf("not an orderbook message")
	}

	return m.parseOrderbookData(msg), nil
}

// parseOrderbookData converts raw depth data to domain.Orderbook.
func (m *WebSocketManager) parseOrderbookData(msg orderbookMessage) *domain.Orderbook {
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
		Exchange:  "bybit",
		Pair:      symbolToPair(msg.Data.Symbol),
		Bids:      parseLevels(msg.Data.Bids),
		Asks:      parseLevels(msg.Data.Asks),
		Timestamp: time.UnixMilli(msg.Ts),
		Sequence:  msg.Data.Seq,
	}
}