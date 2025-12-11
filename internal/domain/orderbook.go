package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// PriceLevel represents a single price level in the orderbook with price and quantity.
type PriceLevel struct {
	// Price is the price at this level.
	Price decimal.Decimal
	// Quantity is the total quantity available at this price.
	Quantity decimal.Decimal
}

// Orderbook represents the current state of bids and asks for a trading pair.
type Orderbook struct {
	// Exchange is the name of the exchange this orderbook is from.
	Exchange string
	// Pair is the trading pair in "BASE/QUOTE" format (e.g., "BTC/USDT").
	Pair string
	// Bids are buy orders sorted descending by price (best bid first).
	Bids []PriceLevel
	// Asks are sell orders sorted ascending by price (best ask first).
	Asks []PriceLevel
	// Timestamp is when this orderbook snapshot was taken.
	Timestamp time.Time
	// Sequence is the exchange-provided sequence number for ordering updates.
	Sequence int64
}

// BestBid returns the highest bid (best buy price) in the orderbook.
// Returns false if there are no bids.
func (o *Orderbook) BestBid() (PriceLevel, bool) {
	if len(o.Bids) == 0 {
		return PriceLevel{}, false
	}
	return o.Bids[0], true
}

// BestAsk returns the lowest ask (best sell price) in the orderbook.
// Returns false if there are no asks.
func (o *Orderbook) BestAsk() (PriceLevel, bool) {
	if len(o.Asks) == 0 {
		return PriceLevel{}, false
	}
	return o.Asks[0], true
}

// Spread returns the difference between best ask and best bid.
// Returns zero if either side is empty.
func (o *Orderbook) Spread() decimal.Decimal {
	bestBid, hasBid := o.BestBid()
	bestAsk, hasAsk := o.BestAsk()
	if !hasBid || !hasAsk {
		return decimal.Zero
	}
	return bestAsk.Price.Sub(bestBid.Price)
}

// MidPrice returns the average of best bid and best ask prices.
// Returns zero if either side is empty.
func (o *Orderbook) MidPrice() decimal.Decimal {
	bestBid, hasBid := o.BestBid()
	bestAsk, hasAsk := o.BestAsk()
	if !hasBid || !hasAsk {
		return decimal.Zero
	}
	return bestBid.Price.Add(bestAsk.Price).Div(decimal.NewFromInt(2))
}
