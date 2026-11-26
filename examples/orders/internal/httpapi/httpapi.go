// Package httpapi exposes the order service over HTTP with JSON.
//
// It translates between the wire format and the domain: it decodes
// requests, calls the service, and maps domain errors to HTTP statuses.
// It contains no business rules.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"uuid"

	"github.com/acme/orders/internal/order"
)

// OrderService is what the HTTP layer needs from the domain. Declaring it
// here, at the consumer, keeps the dependency small and makes handlers easy
// to test with any implementation.
type OrderService interface {
	Create(ctx context.Context, n order.NewOrder) (order.Order, error)
	Get(ctx context.Context, id uuid.UUID) (order.Order, error)
	List(ctx context.Context) ([]order.Order, error)
}

// maxBodyBytes caps request bodies. Without a cap, one client can send a
// huge body and exhaust the server's memory.
const maxBodyBytes = 1 << 20 // 1 MiB

type api struct {
	svc    OrderService
	logger *slog.Logger
	ready  func() bool
}

// NewHandler returns the root handler of the service: all routes, wrapped
// in the standard middleware. ready reports whether the service should
// receive traffic; it backs the /readyz endpoint.
func NewHandler(svc OrderService, logger *slog.Logger, ready func() bool) http.Handler {
	a := &api{svc: svc, logger: logger, ready: ready}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orders", a.createOrder)
	mux.HandleFunc("GET /v1/orders", a.listOrders)
	mux.HandleFunc("GET /v1/orders/{id}", a.getOrder)
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)

	// Outermost first: every request gets an ID, then is logged, and a
	// panic anywhere inside becomes a 500 instead of a dropped connection.
	return withRequestID(logRequests(logger, recoverPanic(logger, mux)))
}

// healthz is the liveness probe: "the process is not stuck". It must not
// check dependencies, or a database outage would make the orchestrator
// restart every healthy pod.
func (a *api) healthz(w http.ResponseWriter, r *http.Request) {
	a.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is the readiness probe: "send me traffic". It turns false during
// shutdown so load balancers stop routing to this instance.
func (a *api) readyz(w http.ResponseWriter, r *http.Request) {
	if !a.ready() {
		a.writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "shutting down"})
		return
	}
	a.writeJSON(w, r, http.StatusOK, map[string]string{"status": "ready"})
}
