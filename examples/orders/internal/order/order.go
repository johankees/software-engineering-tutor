// Package order is the domain core of the orders system: the Order type,
// its rules, and the Service that applies them.
//
// The package has no knowledge of HTTP, JSON or databases. Those live in
// outer packages that depend on this one, never the other way around.
package order

import (
	"errors"
	"fmt"
	"time"
	"uuid"
)

// Sentinel errors. Callers check them with errors.Is; outer layers map them
// to transport details such as HTTP status codes.
var (
	// ErrNotFound means no order exists with the requested ID.
	ErrNotFound = errors.New("order not found")
	// ErrInvalid means the input breaks a domain rule.
	ErrInvalid = errors.New("invalid order")
)

// Status is the lifecycle state of an order.
type Status string

// The possible order states.
const (
	StatusPending   Status = "pending"
	StatusPaid      Status = "paid"
	StatusShipped   Status = "shipped"
	StatusCancelled Status = "cancelled"
)

// Valid reports whether s is one of the known states.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusPaid, StatusShipped, StatusCancelled:
		return true
	default:
		return false
	}
}

// Item is one line of an order. Money is stored in integer cents: floats
// cannot represent 0.10 exactly, and rounding errors in money are bugs.
type Item struct {
	SKU        string
	Quantity   int
	PriceCents int64
}

// Order is a customer's order.
type Order struct {
	ID         uuid.UUID
	CustomerID string
	Items      []Item
	Status     Status
	CreatedAt  time.Time
}

// TotalCents returns the sum of all item lines.
func (o Order) TotalCents() int64 {
	var total int64
	for _, it := range o.Items {
		total += int64(it.Quantity) * it.PriceCents
	}
	return total
}

// Limits that keep a single order within sane bounds.
const (
	MaxItems    = 100
	MaxQuantity = 1_000
)

// NewOrder is the input needed to create an order. The Service fills in
// the ID, status and creation time.
type NewOrder struct {
	CustomerID string
	Items      []Item
}

// Validate checks the domain rules. It reports every problem at once, so a
// client can fix all of them in one round trip. Every returned error wraps
// ErrInvalid.
func (n NewOrder) Validate() error {
	var errs []error
	if n.CustomerID == "" {
		errs = append(errs, errors.New("customer ID is required"))
	}
	switch {
	case len(n.Items) == 0:
		errs = append(errs, errors.New("at least one item is required"))
	case len(n.Items) > MaxItems:
		errs = append(errs, fmt.Errorf("at most %d items are allowed, got %d", MaxItems, len(n.Items)))
	}
	for i, it := range n.Items {
		if it.SKU == "" {
			errs = append(errs, fmt.Errorf("item %d: SKU is required", i))
		}
		if it.Quantity < 1 || it.Quantity > MaxQuantity {
			errs = append(errs, fmt.Errorf("item %d: quantity must be between 1 and %d, got %d", i, MaxQuantity, it.Quantity))
		}
		if it.PriceCents < 0 {
			errs = append(errs, fmt.Errorf("item %d: price must not be negative, got %d", i, it.PriceCents))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrInvalid, errors.Join(errs...))
}
