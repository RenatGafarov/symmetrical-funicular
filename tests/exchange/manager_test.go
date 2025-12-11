package exchange_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"arbitragebot/internal/domain"
	"arbitragebot/internal/exchange"
)

// mockExchange implements exchange.Exchange for testing.
type mockExchange struct {
	name           string
	connected      atomic.Bool
	pairs          []string
	fees           exchange.Fees
	balances       map[string]decimal.Decimal
	orderbook      *domain.Orderbook
	connectErr     error
	disconnectErr  error
	orderbookErr   error
	balancesErr    error
}

func newMockExchange(name string) *mockExchange {
	return &mockExchange{
		name:  name,
		pairs: []string{"BTC/USDT", "ETH/USDT"},
		fees: exchange.Fees{
			Maker: decimal.NewFromFloat(0.001),
			Taker: decimal.NewFromFloat(0.002),
		},
		balances: map[string]decimal.Decimal{
			"BTC":  decimal.NewFromFloat(1.5),
			"USDT": decimal.NewFromFloat(10000),
		},
		orderbook: &domain.Orderbook{
			Exchange: name,
			Pair:     "BTC/USDT",
			Bids: []domain.PriceLevel{
				{Price: decimal.NewFromFloat(50000), Quantity: decimal.NewFromFloat(1)},
			},
			Asks: []domain.PriceLevel{
				{Price: decimal.NewFromFloat(50100), Quantity: decimal.NewFromFloat(1)},
			},
			Timestamp: time.Now(),
		},
	}
}

func (m *mockExchange) Connect(ctx context.Context) error {
	if m.connectErr != nil {
		return m.connectErr
	}
	m.connected.Store(true)
	return nil
}

func (m *mockExchange) Disconnect() error {
	if m.disconnectErr != nil {
		return m.disconnectErr
	}
	m.connected.Store(false)
	return nil
}

func (m *mockExchange) IsConnected() bool {
	return m.connected.Load()
}

func (m *mockExchange) GetOrderbook(ctx context.Context, pair string) (*domain.Orderbook, error) {
	if m.orderbookErr != nil {
		return nil, m.orderbookErr
	}
	ob := *m.orderbook
	ob.Pair = pair
	return &ob, nil
}

func (m *mockExchange) SubscribeOrderbook(ctx context.Context, pairs []string) (<-chan *domain.Orderbook, error) {
	ch := make(chan *domain.Orderbook)
	return ch, nil
}

func (m *mockExchange) PlaceOrder(ctx context.Context, order *domain.Order) (*domain.Trade, error) {
	return &domain.Trade{
		ID:        "trade-123",
		OrderID:   order.ID,
		Exchange:  m.name,
		Pair:      order.Pair,
		Side:      order.Side,
		Price:     order.Price,
		Quantity:  order.Quantity,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockExchange) CancelOrder(ctx context.Context, orderID string) error {
	return nil
}

func (m *mockExchange) GetOrder(ctx context.Context, orderID string) (*domain.Order, error) {
	return &domain.Order{
		ID:       orderID,
		Exchange: m.name,
		Status:   domain.OrderStatusFilled,
	}, nil
}

func (m *mockExchange) GetBalances(ctx context.Context) (map[string]decimal.Decimal, error) {
	if m.balancesErr != nil {
		return nil, m.balancesErr
	}
	return m.balances, nil
}

func (m *mockExchange) GetFees(pair string) exchange.Fees {
	return m.fees
}

func (m *mockExchange) Name() string {
	return m.name
}

func (m *mockExchange) SupportedPairs() []string {
	return m.pairs
}

func TestManager_Register(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	mock := newMockExchange("binance")

	err := mgr.Register(mock)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Duplicate registration should fail
	err = mgr.Register(mock)
	if err == nil {
		t.Error("Register() duplicate should return error")
	}
}

func TestManager_Get(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	mock := newMockExchange("binance")
	_ = mgr.Register(mock)

	ex, err := mgr.Get("binance")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if ex.Name() != "binance" {
		t.Errorf("Get() name = %q, want %q", ex.Name(), "binance")
	}

	// Non-existent exchange
	_, err = mgr.Get("nonexistent")
	if !errors.Is(err, exchange.ErrExchangeNotFound) {
		t.Errorf("Get() error = %v, want %v", err, exchange.ErrExchangeNotFound)
	}
}

func TestManager_Unregister(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	mock := newMockExchange("binance")
	_ = mgr.Register(mock)

	err := mgr.Unregister("binance")
	if err != nil {
		t.Fatalf("Unregister() error = %v", err)
	}

	// Should not be found anymore
	_, err = mgr.Get("binance")
	if !errors.Is(err, exchange.ErrExchangeNotFound) {
		t.Error("Get() after Unregister() should return ErrExchangeNotFound")
	}

	// Unregister non-existent
	err = mgr.Unregister("nonexistent")
	if !errors.Is(err, exchange.ErrExchangeNotFound) {
		t.Errorf("Unregister() error = %v, want %v", err, exchange.ErrExchangeNotFound)
	}
}

func TestManager_All(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	_ = mgr.Register(newMockExchange("binance"))
	_ = mgr.Register(newMockExchange("bybit"))

	all := mgr.All()
	if len(all) != 2 {
		t.Errorf("All() len = %d, want 2", len(all))
	}
}

func TestManager_Names(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	_ = mgr.Register(newMockExchange("binance"))
	_ = mgr.Register(newMockExchange("bybit"))

	names := mgr.Names()
	if len(names) != 2 {
		t.Errorf("Names() len = %d, want 2", len(names))
	}

	nameMap := make(map[string]bool)
	for _, n := range names {
		nameMap[n] = true
	}
	if !nameMap["binance"] || !nameMap["bybit"] {
		t.Error("Names() should contain binance and bybit")
	}
}

func TestManager_ConnectAll(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	ctx := context.Background()
	err := mgr.ConnectAll(ctx)
	if err != nil {
		t.Fatalf("ConnectAll() error = %v", err)
	}

	if !binance.IsConnected() {
		t.Error("binance should be connected")
	}
	if !bybit.IsConnected() {
		t.Error("bybit should be connected")
	}
}

func TestManager_ConnectAll_Error(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	binance.connectErr = errors.New("connection failed")
	_ = mgr.Register(binance)

	ctx := context.Background()
	err := mgr.ConnectAll(ctx)
	if err == nil {
		t.Error("ConnectAll() should return error")
	}
}

func TestManager_DisconnectAll(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	ctx := context.Background()
	_ = mgr.ConnectAll(ctx)

	err := mgr.DisconnectAll()
	if err != nil {
		t.Fatalf("DisconnectAll() error = %v", err)
	}

	if binance.IsConnected() {
		t.Error("binance should be disconnected")
	}
	if bybit.IsConnected() {
		t.Error("bybit should be disconnected")
	}
}

func TestManager_ConnectedExchanges(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	// None connected initially
	connected := mgr.ConnectedExchanges()
	if len(connected) != 0 {
		t.Errorf("ConnectedExchanges() len = %d, want 0", len(connected))
	}

	// Connect one
	_ = binance.Connect(context.Background())

	connected = mgr.ConnectedExchanges()
	if len(connected) != 1 {
		t.Errorf("ConnectedExchanges() len = %d, want 1", len(connected))
	}
	if connected[0].Name() != "binance" {
		t.Errorf("ConnectedExchanges()[0].Name() = %q, want %q", connected[0].Name(), "binance")
	}
}

func TestManager_GetOrderbooks(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	ctx := context.Background()
	_ = mgr.ConnectAll(ctx)

	orderbooks, err := mgr.GetOrderbooks(ctx, "BTC/USDT")
	if err != nil {
		t.Fatalf("GetOrderbooks() error = %v", err)
	}

	if len(orderbooks) != 2 {
		t.Errorf("GetOrderbooks() len = %d, want 2", len(orderbooks))
	}

	if orderbooks["binance"] == nil {
		t.Error("GetOrderbooks() should have binance")
	}
	if orderbooks["bybit"] == nil {
		t.Error("GetOrderbooks() should have bybit")
	}
}

func TestManager_GetOrderbooks_NotConnected(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	_ = mgr.Register(newMockExchange("binance"))

	ctx := context.Background()
	_, err := mgr.GetOrderbooks(ctx, "BTC/USDT")
	if !errors.Is(err, exchange.ErrNotConnected) {
		t.Errorf("GetOrderbooks() error = %v, want %v", err, exchange.ErrNotConnected)
	}
}

func TestManager_GetAllBalances(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	ctx := context.Background()
	_ = mgr.ConnectAll(ctx)

	balances, err := mgr.GetAllBalances(ctx)
	if err != nil {
		t.Fatalf("GetAllBalances() error = %v", err)
	}

	if len(balances) != 2 {
		t.Errorf("GetAllBalances() len = %d, want 2", len(balances))
	}

	if balances["binance"] == nil {
		t.Error("GetAllBalances() should have binance")
	}
	if balances["binance"]["BTC"].IsZero() {
		t.Error("GetAllBalances() binance should have BTC balance")
	}
}

func TestManager_HealthCheck(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	binance := newMockExchange("binance")
	bybit := newMockExchange("bybit")
	_ = mgr.Register(binance)
	_ = mgr.Register(bybit)

	ctx := context.Background()

	// Not connected - should be unhealthy
	health := mgr.HealthCheck(ctx)
	if health["binance"] {
		t.Error("HealthCheck() binance should be unhealthy when disconnected")
	}

	// Connect binance only
	_ = binance.Connect(ctx)

	health = mgr.HealthCheck(ctx)
	if !health["binance"] {
		t.Error("HealthCheck() binance should be healthy when connected")
	}
	if health["bybit"] {
		t.Error("HealthCheck() bybit should be unhealthy when disconnected")
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	mgr := exchange.NewManager(nil)
	for i := 0; i < 10; i++ {
		_ = mgr.Register(newMockExchange(string(rune('a' + i))))
	}

	ctx := context.Background()
	_ = mgr.ConnectAll(ctx)

	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			_ = mgr.All()
			_ = mgr.Names()
			_ = mgr.ConnectedExchanges()
			_, _ = mgr.GetOrderbooks(ctx, "BTC/USDT")
			_, _ = mgr.GetAllBalances(ctx)
			_ = mgr.HealthCheck(ctx)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}