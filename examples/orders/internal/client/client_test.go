package client

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"uuid"

	"github.com/acme/orders/internal/httpapi"
	"github.com/acme/orders/internal/order"
	"github.com/acme/orders/internal/storage/memory"
)

// newTestClient starts the real server handler on an in-memory network.
// This is a contract test: client and server wire types must agree.
func newTestClient(t *testing.T) *Client {
	t.Helper()
	h := httpapi.NewHandler(order.NewService(memory.New()), slog.New(slog.DiscardHandler), func() bool { return true })
	srv := httptest.NewTestServer(t, h)
	c, err := New(srv.URL, "orderctl-test", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := newTestClient(t)
	ctx := t.Context()

	created, err := c.CreateOrder(ctx, order.NewOrder{
		CustomerID: "c-7",
		Items:      []order.Item{{SKU: "MUG", Quantity: 3, PriceCents: 899}},
	})
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	if created.TotalCents() != 2697 || created.Status != order.StatusPending {
		t.Errorf("created = %+v", created)
	}

	got, err := c.GetOrder(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetOrder() error = %v", err)
	}
	if got.ID != created.ID || !got.CreatedAt.Equal(created.CreatedAt) || got.Items[0] != created.Items[0] {
		t.Errorf("GetOrder() = %+v, want %+v", got, created)
	}

	list, err := c.ListOrders(ctx)
	if err != nil {
		t.Fatalf("ListOrders() error = %v", err)
	}
	if len(list) != 1 {
		t.Errorf("len(ListOrders()) = %d, want 1", len(list))
	}
}

func TestDomainErrorsSurviveTheNetwork(t *testing.T) {
	c := newTestClient(t)

	_, err := c.GetOrder(t.Context(), uuid.New())
	if !errors.Is(err, order.ErrNotFound) {
		t.Errorf("GetOrder(unknown) error = %v, want ErrNotFound", err)
	}
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.RequestID == "" {
		t.Errorf("error = %#v, want *APIError with request ID", err)
	}

	_, err = c.CreateOrder(t.Context(), order.NewOrder{})
	if !errors.Is(err, order.ErrInvalid) {
		t.Errorf("CreateOrder(empty) error = %v, want ErrInvalid", err)
	}
}

func TestNonJSONErrorBody(t *testing.T) {
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "<html>502 Bad Gateway</html>", http.StatusBadGateway)
	}))
	c, _ := New(srv.URL, "test", srv.Client())

	_, err := c.ListOrders(t.Context())
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %v, want *APIError with status 502", err)
	}
}

func TestContextDeadline(t *testing.T) {
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // a server that never answers
	}))
	c, _ := New(srv.URL, "test", srv.Client())

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := c.ListOrders(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestNewValidatesURL(t *testing.T) {
	for _, bad := range []string{"localhost:8080", "ftp://x", "http://", "://"} {
		if _, err := New(bad, "test", nil); err == nil {
			t.Errorf("New(%q) error = nil, want error", bad)
		}
	}
}
