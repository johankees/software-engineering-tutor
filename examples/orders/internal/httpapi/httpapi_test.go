package httpapi

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"uuid"

	"github.com/acme/orders/internal/order"
	"github.com/acme/orders/internal/storage/memory"
)

// newTestHandler wires the real service and in-memory store. Handler tests
// then exercise real behavior instead of mocks.
func newTestHandler(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := order.NewService(memory.New())
	return NewHandler(svc, logger, func() bool { return true }), &logs
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

const validOrder = `{"customer_id":"c-42","items":[{"sku":"BOOK-1","quantity":2,"price_cents":1250}]}`

func TestCreateAndGetOrder(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := do(t, h, http.MethodPost, "/v1/orders", validOrder)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body)
	}
	created := decode[orderResponse](t, rec)
	if created.TotalCents != 2500 || created.Status != "pending" {
		t.Errorf("created = %+v, want total 2500 and status pending", created)
	}
	if got, want := rec.Header().Get("Location"), "/v1/orders/"+created.ID.String(); got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}

	rec = do(t, h, http.MethodGet, "/v1/orders/"+created.ID.String(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if got := decode[orderResponse](t, rec); got.ID != created.ID {
		t.Errorf("GET id = %s, want %s", got.ID, created.ID)
	}

	rec = do(t, h, http.MethodGet, "/v1/orders", "")
	if got := decode[listOrdersResponse](t, rec); len(got.Orders) != 1 {
		t.Errorf("list has %d orders, want 1", len(got.Orders))
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		target      string
		body        string
		contentType string
		wantStatus  int
		wantDetail  string
	}{
		{"unknown order", http.MethodGet, "/v1/orders/019ac0de-0000-7000-8000-000000000001", "", "", http.StatusNotFound, "order not found"},
		{"bad id", http.MethodGet, "/v1/orders/42", "", "", http.StatusBadRequest, "must be a UUID"},
		{"invalid order", http.MethodPost, "/v1/orders", `{"customer_id":"","items":[]}`, "application/json", http.StatusBadRequest, "customer ID is required; at least one item"},
		{"unknown field", http.MethodPost, "/v1/orders", `{"customer":"c-1"}`, "application/json", http.StatusBadRequest, "unknown object member"},
		{"broken JSON", http.MethodPost, "/v1/orders", `{"customer_id":`, "application/json", http.StatusBadRequest, "malformed JSON"},
		{"wrong content type", http.MethodPost, "/v1/orders", validOrder, "text/plain", http.StatusUnsupportedMediaType, "application/json"},
		{"body too large", http.MethodPost, "/v1/orders", `{"customer_id":"` + strings.Repeat("x", maxBodyBytes) + `"}`, "application/json", http.StatusRequestEntityTooLarge, "1 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := newTestHandler(t)
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.target, strings.NewReader(tt.body))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tt.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
			p := decode[problem](t, rec)
			if !strings.Contains(p.Detail, tt.wantDetail) {
				t.Errorf("detail = %q, want it to contain %q", p.Detail, tt.wantDetail)
			}
			if p.RequestID == "" || p.RequestID != rec.Header().Get("X-Request-ID") {
				t.Errorf("problem request_id = %q, header = %q; want equal and non-empty", p.RequestID, rec.Header().Get("X-Request-ID"))
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := do(t, h, http.MethodDelete, "/v1/orders", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
		t.Errorf("Allow = %q, want it to list POST", allow)
	}
}

// errService fails every call with err. It shows why OrderService is an
// interface: we can simulate a database outage in three lines.
type errService struct{ err error }

func (s errService) Create(context.Context, order.NewOrder) (order.Order, error) {
	return order.Order{}, s.err
}
func (s errService) Get(context.Context, uuid.UUID) (order.Order, error) { return order.Order{}, s.err }
func (s errService) List(context.Context) ([]order.Order, error)         { return nil, s.err }

func TestInternalErrorIsHidden(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	svc := errService{err: errors.New("dial tcp 10.0.0.7:5432: connection refused")}
	h := NewHandler(svc, logger, func() bool { return true })

	rec := do(t, h, http.MethodGet, "/v1/orders", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("response leaks internal error: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "10.0.0.7") {
		t.Errorf("log does not contain the internal error: %s", logs.String())
	}
}

func TestRequestID(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "gateway-123")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "gateway-123" {
		t.Errorf("X-Request-ID = %q, want incoming ID to be kept", got)
	}

	req.Header.Set("X-Request-ID", "evil\nINFO fake log line")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); strings.Contains(got, "\n") || got == "" {
		t.Errorf("X-Request-ID = %q, want a fresh generated ID", got)
	}
}

func TestReadiness(t *testing.T) {
	var ready atomic.Bool
	ready.Store(true)
	h := NewHandler(order.NewService(memory.New()), slog.New(slog.DiscardHandler), ready.Load)

	if rec := do(t, h, http.MethodGet, "/readyz", ""); rec.Code != http.StatusOK {
		t.Errorf("ready: status = %d, want 200", rec.Code)
	}
	ready.Store(false)
	if rec := do(t, h, http.MethodGet, "/readyz", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("not ready: status = %d, want 503", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("healthz while not ready: status = %d, want 200", rec.Code)
	}
}

func TestRecoverPanic(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("nil map write") })
	h := withRequestID(recoverPanic(logger, boom))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(logs.String(), "nil map write") || !strings.Contains(logs.String(), "stack") {
		t.Errorf("panic not logged with stack: %s", logs.String())
	}
	if p := decode[problem](t, rec); p.Status != 500 {
		t.Errorf("body = %s, want valid problem JSON", rec.Body)
	}
}

func TestAccessLog(t *testing.T) {
	h, logs := newTestHandler(t)
	do(t, h, http.MethodGet, "/v1/orders/42", "")

	var line struct {
		Msg    string `json:"msg"`
		Route  string `json:"route"`
		Status int    `json:"status"`
	}
	first, _, _ := strings.Cut(logs.String(), "\n")
	if err := json.Unmarshal([]byte(first), &line); err != nil {
		t.Fatalf("parse log line %q: %v", first, err)
	}
	if line.Msg != "http request" || line.Route != "GET /v1/orders/{id}" || line.Status != 400 {
		t.Errorf("log line = %+v, want http request, route pattern, status 400", line)
	}
}
