// Package memory is an in-memory order.Repository. It is used for local
// development and tests; a production deployment would use a database-backed
// implementation of the same interface.
package memory

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"uuid"

	"github.com/acme/orders/internal/order"
)

// Store is an order.Repository that keeps orders in a map.
// The zero value is not usable; call New.
type Store struct {
	mu     sync.RWMutex
	orders map[uuid.UUID]order.Order
}

// New returns an empty Store.
func New() *Store {
	return &Store{orders: make(map[uuid.UUID]order.Order)}
}

// Compile-time proof that Store satisfies the interface.
var _ order.Repository = (*Store)(nil)

// Get implements order.Repository.
func (s *Store) Get(ctx context.Context, id uuid.UUID) (order.Order, error) {
	if err := ctx.Err(); err != nil {
		return order.Order{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	if !ok {
		return order.Order{}, order.ErrNotFound
	}
	return clone(o), nil
}

// Create implements order.Repository.
func (s *Store) Create(ctx context.Context, o order.Order) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.orders[o.ID]; exists {
		return fmt.Errorf("order %s already exists", o.ID)
	}
	s.orders[o.ID] = clone(o)
	return nil
}

// List implements order.Repository.
func (s *Store) List(ctx context.Context) ([]order.Order, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	all := slices.Collect(maps.Values(s.orders))
	s.mu.RUnlock()

	for i := range all {
		all[i] = clone(all[i])
	}
	slices.SortFunc(all, func(a, b order.Order) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), a.ID.Compare(b.ID))
	})
	return all, nil
}

// clone copies the Items slice, so callers can never change stored data
// through a shared backing array.
func clone(o order.Order) order.Order {
	o.Items = slices.Clone(o.Items)
	return o
}
