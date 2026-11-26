package order

import (
	"context"
	"fmt"
	"slices"
	"time"
	"uuid"
)

// Repository stores orders. It is defined here, where it is used, so the
// domain decides what it needs and storage packages adapt to it.
//
// Get returns ErrNotFound when no order has the given ID.
// List returns orders sorted by creation time, oldest first.
type Repository interface {
	Get(ctx context.Context, id uuid.UUID) (Order, error)
	Create(ctx context.Context, o Order) error
	List(ctx context.Context) ([]Order, error)
}

// Service applies the domain rules on top of a Repository.
// It is safe for concurrent use if the Repository is.
type Service struct {
	repo  Repository
	now   func() time.Time
	newID func() uuid.UUID
}

// NewService returns a Service backed by repo.
func NewService(repo Repository) *Service {
	return &Service{
		repo:  repo,
		now:   time.Now,
		newID: uuid.NewV7,
	}
}

// Create validates n and stores it as a new pending order.
func (s *Service) Create(ctx context.Context, n NewOrder) (Order, error) {
	if err := n.Validate(); err != nil {
		return Order{}, err
	}
	o := Order{
		ID:         s.newID(),
		CustomerID: n.CustomerID,
		Items:      slices.Clone(n.Items),
		Status:     StatusPending,
		CreatedAt:  s.now().UTC(),
	}
	if err := s.repo.Create(ctx, o); err != nil {
		return Order{}, fmt.Errorf("store order: %w", err)
	}
	return o, nil
}

// Get returns the order with the given ID, or an error wrapping ErrNotFound.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Order, error) {
	o, err := s.repo.Get(ctx, id)
	if err != nil {
		return Order{}, fmt.Errorf("get order %s: %w", id, err)
	}
	return o, nil
}

// List returns all orders, oldest first.
func (s *Service) List(ctx context.Context) ([]Order, error) {
	orders, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	return orders, nil
}
