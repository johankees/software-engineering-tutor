package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/acme/orders/internal/order"
)

func sampleOrder(createdAt time.Time) order.Order {
	return order.Order{
		ID:         uuid.NewV7(),
		CustomerID: "c-1",
		Items:      []order.Item{{SKU: "A", Quantity: 1, PriceCents: 100}},
		Status:     order.StatusPending,
		CreatedAt:  createdAt,
	}
}

func TestCreateGet(t *testing.T) {
	s := New()
	o := sampleOrder(time.Now())
	if err := s.Create(t.Context(), o); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	got, err := s.Get(t.Context(), o.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != o.ID || got.CustomerID != o.CustomerID {
		t.Errorf("Get() = %+v, want %+v", got, o)
	}
}

func TestCreateDuplicate(t *testing.T) {
	s := New()
	o := sampleOrder(time.Now())
	if err := s.Create(t.Context(), o); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(t.Context(), o); err == nil {
		t.Fatal("second Create() error = nil, want duplicate error")
	}
}

func TestGetNotFound(t *testing.T) {
	_, err := New().Get(t.Context(), uuid.New())
	if !errors.Is(err, order.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
}

func TestStoredDataIsIsolated(t *testing.T) {
	s := New()
	o := sampleOrder(time.Now())
	if err := s.Create(t.Context(), o); err != nil {
		t.Fatal(err)
	}
	o.Items[0].SKU = "MUTATED"

	got, _ := s.Get(t.Context(), o.ID)
	got.Items[0].SKU = "MUTATED AGAIN"

	again, _ := s.Get(t.Context(), o.ID)
	if again.Items[0].SKU != "A" {
		t.Errorf("stored SKU = %q, want %q", again.Items[0].SKU, "A")
	}
}

func TestListSortedByCreation(t *testing.T) {
	s := New()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, offset := range []int{3, 1, 2} {
		if err := s.Create(t.Context(), sampleOrder(base.Add(time.Duration(offset)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].CreatedAt.After(got[i].CreatedAt) {
			t.Fatalf("List() not sorted: %v after %v", got[i-1].CreatedAt, got[i].CreatedAt)
		}
	}
}

func TestCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New().List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("List() error = %v, want context.Canceled", err)
	}
}

// Run with -race: many goroutines writing and reading at once.
func TestConcurrentAccess(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Go(func() {
			o := sampleOrder(time.Now())
			o.CustomerID = fmt.Sprint("c-", i)
			if err := s.Create(t.Context(), o); err != nil {
				t.Error(err)
			}
			if _, err := s.List(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, _ := s.List(t.Context())
	if len(got) != 50 {
		t.Errorf("len(List()) = %d, want 50", len(got))
	}
}
