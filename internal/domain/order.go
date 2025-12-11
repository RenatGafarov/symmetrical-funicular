// Package domain contains core business entities and value objects.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// OrderSide represents the direction of an order (buy or sell).
type OrderSide string

const (
	// OrderSideBuy indicates a buy order.
	OrderSideBuy OrderSide = "buy"
	// OrderSideSell indicates a sell order.
	OrderSideSell OrderSide = "sell"
)

// OrderType represents the type of order execution.
type OrderType string

const (
	// OrderTypeLimit is a limit order that executes at the specified price or better.
	OrderTypeLimit OrderType = "limit"
	// OrderTypeMarket is a market order that executes immediately at the best available price.
	OrderTypeMarket OrderType = "market"
)

// OrderStatus represents the current state of an order.
type OrderStatus string

const (
	// OrderStatusPending indicates the order is created but not yet submitted.
	OrderStatusPending OrderStatus = "pending"
	// OrderStatusOpen indicates the order is submitted and waiting to be filled.
	OrderStatusOpen OrderStatus = "open"
	// OrderStatusFilled indicates the order has been completely filled.
	OrderStatusFilled OrderStatus = "filled"
	// OrderStatusCancelled indicates the order was cancelled before being filled.
	OrderStatusCancelled OrderStatus = "cancelled"
	// OrderStatusFailed indicates the order failed due to an error.
	OrderStatusFailed OrderStatus = "failed"
)

// Order represents a trading order on an exchange.
type Order struct {
	// ID is the unique identifier assigned by the exchange.
	ID string
	// Exchange is the name of the exchange where the order is placed.
	Exchange string
	// Pair is the trading pair in "BASE/QUOTE" format (e.g., "BTC/USDT").
	Pair string
	// Side indicates whether this is a buy or sell order.
	Side OrderSide
	// Type indicates limit or market order.
	Type OrderType
	// Price is the limit price for limit orders (ignored for market orders).
	Price decimal.Decimal
	// Quantity is the amount of base currency to buy or sell.
	Quantity decimal.Decimal
	// Status is the current state of the order.
	Status OrderStatus
	// CreatedAt is when the order was created.
	CreatedAt time.Time
	// UpdatedAt is when the order was last updated.
	UpdatedAt time.Time
}

// Trade represents an executed trade resulting from an order fill.
type Trade struct {
	// ID is the unique identifier for this trade.
	ID string
	// OrderID is the ID of the order that created this trade.
	OrderID string
	// Exchange is the name of the exchange where the trade occurred.
	Exchange string
	// Pair is the trading pair in "BASE/QUOTE" format.
	Pair string
	// Side indicates buy or sell.
	Side OrderSide
	// Price is the execution price.
	Price decimal.Decimal
	// Quantity is the amount of base currency traded.
	Quantity decimal.Decimal
	// Fee is the trading fee charged.
	Fee decimal.Decimal
	// FeeCurrency is the currency in which the fee was charged.
	FeeCurrency string
	// Timestamp is when the trade was executed.
	Timestamp time.Time
}
