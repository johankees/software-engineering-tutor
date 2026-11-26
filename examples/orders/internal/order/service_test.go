package order

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"
)

// fakeRepo is a hand-written fake. It is simpler than a mocking library and
// it behaves like a real store, so tests check results, not call sequences.
type fakeRepo struct {
	orders    map[uuid.UUID]Order
	createErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{orders: map[uuid.UUID]Order{}} }

func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (Order, error) {
	o, ok := f.orders[id]
	if !ok {
		return Order{}, ErrNotFound
	}
	return o, nil
}

func (f *fakeRepo) Create(_ context.Context, o Order) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.orders[o.ID] = o
	return nil
}

func (f *fakeRepo) List(context.Context) ([]Order, error) {
	out := make([]Order, 0, len(f.orders))
	for _, o := range f.orders {
		out = append(out, o)
	}
	return out, nil
}

func newTestService(repo Repository) *Service {
	s := NewService(repo)
	s.now = func() time.Time { return time.Date(2026, 11, 26, 9, 30, 0, 0, time.UTC) }
	s.newID = func() uuid.UUID { return uuid.MustParse("019ac0de-0000-7000-8000-000000000001") }
	return s
}

func TestServiceCreate(t *testing.T) {
	repo := newFakeRepo()
	svc := newTestService(repo)

	items := []Item{{SKU: "BOOK-1", Quantity: 1, PriceCents: 1999}}
	got, err := svc.Create(t.Context(), NewOrder{CustomerID: "c-1", Items: items})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if got.Status != StatusPending {
		t.Errorf("Status = %q, want %q", got.Status, StatusPending)
	}
	if want := svc.now(); !got.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want)
	}
	if _, ok := repo.orders[got.ID]; !ok {
		t.Error("order was not stored")
	}

	items[0].SKU = "CHANGED"
	if got.Items[0].SKU != "BOOK-1" {
		t.Error("Create kept a reference to the caller's slice")
	}
}

func TestServiceCreateInvalid(t *testing.T) {
	svc := newTestService(newFakeRepo())
	_, err := svc.Create(t.Context(), NewOrder{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Create() error = %v, want ErrInvalid", err)
	}
}

func TestServiceCreateStoreError(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = errors.New("disk full")
	svc := newTestService(repo)

	_, err := svc.Create(t.Context(), NewOrder{CustomerID: "c", Items: []Item{{SKU: "A", Quantity: 1}}})
	if !errors.Is(err, repo.createErr) {
		t.Fatalf("Create() error = %v, want wrapped %v", err, repo.createErr)
	}
}

func TestServiceGetNotFound(t *testing.T) {
	svc := newTestService(newFakeRepo())
	_, err := svc.Get(t.Context(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}
