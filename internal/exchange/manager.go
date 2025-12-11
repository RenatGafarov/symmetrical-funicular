package exchange

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"arbitragebot/internal/domain"
)

// Manager coordinates multiple exchanges and provides unified access to them.
// It handles parallel operations across exchanges and is safe for concurrent use.
type Manager struct {
	exchanges map[string]Exchange
	mu        sync.RWMutex
	logger    *zap.Logger
}

// NewManager creates a new exchange manager with the given logger.
// If logger is nil, a no-op logger is used.
func NewManager(logger *zap.Logger) *Manager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		exchanges: make(map[string]Exchange),
		logger:    logger,
	}
}

// Register adds an exchange to the manager.
// Returns error if an exchange with the same name is already registered.
// The exchange is not connected automatically; call ConnectAll or connect individually.
func (m *Manager) Register(ex Exchange) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := ex.Name()
	if _, exists := m.exchanges[name]; exists {
		return fmt.Errorf("exchange %s already registered", name)
	}

	m.exchanges[name] = ex
	m.logger.Info("exchange registered", zap.String("exchange", name))
	return nil
}

// Unregister removes an exchange from the manager by name.
// If the exchange is connected, it will be disconnected first.
// Returns ErrExchangeNotFound if no exchange with given name exists.
func (m *Manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ex, exists := m.exchanges[name]
	if !exists {
		return ErrExchangeNotFound
	}

	if ex.IsConnected() {
		if err := ex.Disconnect(); err != nil {
			m.logger.Warn("error disconnecting exchange",
				zap.String("exchange", name),
				zap.Error(err))
		}
	}

	delete(m.exchanges, name)
	m.logger.Info("exchange unregistered", zap.String("exchange", name))
	return nil
}

// Get returns an exchange by name.
// Returns ErrExchangeNotFound if no exchange with given name exists.
func (m *Manager) Get(name string) (Exchange, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ex, exists := m.exchanges[name]
	if !exists {
		return nil, ErrExchangeNotFound
	}
	return ex, nil
}

// All returns all registered exchanges.
// The returned slice is a copy and safe to iterate without locking.
func (m *Manager) All() []Exchange {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exchanges := make([]Exchange, 0, len(m.exchanges))
	for _, ex := range m.exchanges {
		exchanges = append(exchanges, ex)
	}
	return exchanges
}

// Names returns names of all registered exchanges.
// The returned slice is a copy and safe to iterate without locking.
func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.exchanges))
	for name := range m.exchanges {
		names = append(names, name)
	}
	return names
}

// ConnectedExchanges returns all exchanges that are currently connected.
// The returned slice is a copy and safe to iterate without locking.
func (m *Manager) ConnectedExchanges() []Exchange {
	m.mu.RLock()
	defer m.mu.RUnlock()

	exchanges := make([]Exchange, 0, len(m.exchanges))
	for _, ex := range m.exchanges {
		if ex.IsConnected() {
			exchanges = append(exchanges, ex)
		}
	}
	return exchanges
}

// ConnectAll connects to all registered exchanges in parallel using errgroup.
// If any connection fails, returns the first error and cancels remaining connections.
// Use context to set overall timeout for all connections.
func (m *Manager) ConnectAll(ctx context.Context) error {
	m.mu.RLock()
	exchanges := make([]Exchange, 0, len(m.exchanges))
	for _, ex := range m.exchanges {
		exchanges = append(exchanges, ex)
	}
	m.mu.RUnlock()

	g, ctx := errgroup.WithContext(ctx)
	for _, ex := range exchanges {
		ex := ex
		g.Go(func() error {
			if err := ex.Connect(ctx); err != nil {
				return fmt.Errorf("connect to %s: %w", ex.Name(), err)
			}
			m.logger.Info("exchange connected", zap.String("exchange", ex.Name()))
			return nil
		})
	}

	return g.Wait()
}

// DisconnectAll disconnects from all connected exchanges sequentially.
// Returns the first error encountered, but continues disconnecting remaining exchanges.
func (m *Manager) DisconnectAll() error {
	m.mu.RLock()
	exchanges := make([]Exchange, 0, len(m.exchanges))
	for _, ex := range m.exchanges {
		exchanges = append(exchanges, ex)
	}
	m.mu.RUnlock()

	var firstErr error
	for _, ex := range exchanges {
		if ex.IsConnected() {
			if err := ex.Disconnect(); err != nil {
				m.logger.Error("error disconnecting exchange",
					zap.String("exchange", ex.Name()),
					zap.Error(err))
				if firstErr == nil {
					firstErr = err
				}
			} else {
				m.logger.Info("exchange disconnected", zap.String("exchange", ex.Name()))
			}
		}
	}

	return firstErr
}

// GetOrderbooks fetches orderbooks from all connected exchanges for a given pair in parallel.
// The pair format should be "BASE/QUOTE" (e.g., "BTC/USDT").
// Returns a map of exchange name to orderbook. Exchanges that fail are logged but not included.
// Returns ErrNotConnected if no exchanges are connected.
func (m *Manager) GetOrderbooks(ctx context.Context, pair string) (map[string]*domain.Orderbook, error) {
	exchanges := m.ConnectedExchanges()
	if len(exchanges) == 0 {
		return nil, ErrNotConnected
	}

	result := make(map[string]*domain.Orderbook)
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)

	for _, ex := range exchanges {
		ex := ex
		g.Go(func() error {
			ob, err := ex.GetOrderbook(ctx, pair)
			if err != nil {
				m.logger.Warn("failed to get orderbook",
					zap.String("exchange", ex.Name()),
					zap.String("pair", pair),
					zap.Error(err))
				return nil // Don't fail the whole operation
			}

			mu.Lock()
			result[ex.Name()] = ob
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetAllBalances fetches balances from all connected exchanges in parallel.
// Returns a map of exchange name to balance map (asset symbol -> amount).
// Exchanges that fail are logged but not included in the result.
// Returns ErrNotConnected if no exchanges are connected.
func (m *Manager) GetAllBalances(ctx context.Context) (map[string]map[string]decimal.Decimal, error) {
	exchanges := m.ConnectedExchanges()
	if len(exchanges) == 0 {
		return nil, ErrNotConnected
	}

	result := make(map[string]map[string]decimal.Decimal)
	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)

	for _, ex := range exchanges {
		ex := ex
		g.Go(func() error {
			balances, err := ex.GetBalances(ctx)
			if err != nil {
				m.logger.Warn("failed to get balances",
					zap.String("exchange", ex.Name()),
					zap.Error(err))
				return nil
			}

			mu.Lock()
			result[ex.Name()] = balances
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return result, nil
}

// HealthCheck checks connectivity to all registered exchanges in parallel.
// Returns a map of exchange name to health status (true = healthy).
// An exchange is considered healthy if it's connected and can fetch an orderbook.
func (m *Manager) HealthCheck(ctx context.Context) map[string]bool {
	m.mu.RLock()
	exchanges := make(map[string]Exchange)
	for name, ex := range m.exchanges {
		exchanges[name] = ex
	}
	m.mu.RUnlock()

	result := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, ex := range exchanges {
		wg.Add(1)
		go func(name string, ex Exchange) {
			defer wg.Done()

			healthy := ex.IsConnected()
			if healthy {
				// Try to fetch orderbook as a health check
				_, err := ex.GetOrderbook(ctx, "BTC/USDT")
				healthy = err == nil
			}

			mu.Lock()
			result[name] = healthy
			mu.Unlock()
		}(name, ex)
	}

	wg.Wait()
	return result
}
