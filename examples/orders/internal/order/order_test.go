package order

import (
	"errors"
	"strings"
	"testing"
)

func TestNewOrderValidate(t *testing.T) {
	valid := Item{SKU: "BOOK-1", Quantity: 2, PriceCents: 1250}

	tests := []struct {
		name    string
		in      NewOrder
		wantErr string // substring; empty means no error
	}{
		{name: "valid", in: NewOrder{CustomerID: "c-1", Items: []Item{valid}}},
		{name: "missing customer", in: NewOrder{Items: []Item{valid}}, wantErr: "customer ID is required"},
		{name: "no items", in: NewOrder{CustomerID: "c-1"}, wantErr: "at least one item"},
		{name: "too many items", in: NewOrder{CustomerID: "c-1", Items: make([]Item, MaxItems+1)}, wantErr: "at most 100 items"},
		{name: "empty SKU", in: NewOrder{CustomerID: "c-1", Items: []Item{{Quantity: 1}}}, wantErr: "item 0: SKU is required"},
		{name: "zero quantity", in: NewOrder{CustomerID: "c-1", Items: []Item{{SKU: "A"}}}, wantErr: "quantity must be between 1 and 1000, got 0"},
		{name: "negative price", in: NewOrder{CustomerID: "c-1", Items: []Item{{SKU: "A", Quantity: 1, PriceCents: -1}}}, wantErr: "price must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want error wrapping ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	err := NewOrder{Items: []Item{{}}}.Validate()
	for _, want := range []string{"customer ID", "SKU", "quantity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, want it to mention %q", err, want)
		}
	}
}

func TestTotalCents(t *testing.T) {
	o := Order{Items: []Item{
		{SKU: "A", Quantity: 3, PriceCents: 199},
		{SKU: "B", Quantity: 1, PriceCents: 1000},
	}}
	if got, want := o.TotalCents(), int64(1597); got != want {
		t.Errorf("TotalCents() = %d, want %d", got, want)
	}
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusPaid, StatusShipped, StatusCancelled} {
		if !s.Valid() {
			t.Errorf("%q.Valid() = false, want true", s)
		}
	}
	if Status("lost").Valid() {
		t.Error(`"lost".Valid() = true, want false`)
	}
}
