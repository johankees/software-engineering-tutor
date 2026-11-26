// Package client is a Go client for the orders HTTP API.
//
// It has its own copy of the wire types instead of importing the server's.
// A little copying keeps the client independent of server internals, and
// the client tests run against the real server handler, so any drift
// between the two copies fails the build.
package client

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"uuid"

	"github.com/acme/orders/internal/order"
)

// DefaultTimeout bounds every request when the caller passes no client.
// http.DefaultClient has no timeout at all: one stuck server would hang
// the caller forever.
const DefaultTimeout = 10 * time.Second

// maxResponseBytes protects the client from a misbehaving server.
const maxResponseBytes = 10 << 20 // 10 MiB

// Client talks to one orders service. It is safe for concurrent use.
type Client struct {
	base      *url.URL
	http      *http.Client
	userAgent string
}

// New returns a Client for the service at baseURL, e.g.
// "http://localhost:8080". If httpClient is nil, a client with
// DefaultTimeout is used.
func New(baseURL, userAgent string, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("base URL %q: scheme must be http or https", baseURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("base URL %q: host is required", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}
	return &Client{base: u, http: httpClient, userAgent: userAgent}, nil
}

// APIError is a non-2xx response from the service.
type APIError struct {
	StatusCode int
	Title      string
	Detail     string
	RequestID  string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("orders API: %d %s", e.StatusCode, e.Title)
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// Unwrap maps HTTP statuses back to domain errors, so callers can write
// errors.Is(err, order.ErrNotFound) whether they use the service directly
// or over the network.
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusNotFound:
		return order.ErrNotFound
	case http.StatusBadRequest:
		return order.ErrInvalid
	default:
		return nil
	}
}

// CreateOrder creates a new order.
func (c *Client) CreateOrder(ctx context.Context, n order.NewOrder) (order.Order, error) {
	req := createOrderRequest{CustomerID: n.CustomerID, Items: make([]itemJSON, len(n.Items))}
	for i, it := range n.Items {
		req.Items[i] = itemJSON(it)
	}
	var resp orderJSON
	if err := c.do(ctx, http.MethodPost, "/v1/orders", req, http.StatusCreated, &resp); err != nil {
		return order.Order{}, err
	}
	return resp.toOrder(), nil
}

// GetOrder fetches one order.
func (c *Client) GetOrder(ctx context.Context, id uuid.UUID) (order.Order, error) {
	var resp orderJSON
	if err := c.do(ctx, http.MethodGet, "/v1/orders/"+id.String(), nil, http.StatusOK, &resp); err != nil {
		return order.Order{}, err
	}
	return resp.toOrder(), nil
}

// ListOrders fetches all orders, oldest first.
func (c *Client) ListOrders(ctx context.Context) ([]order.Order, error) {
	var resp struct {
		Orders []orderJSON `json:"orders"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/orders", nil, http.StatusOK, &resp); err != nil {
		return nil, err
	}
	out := make([]order.Order, len(resp.Orders))
	for i, o := range resp.Orders {
		out[i] = o.toOrder()
	}
	return out, nil
}

// do sends one request and decodes the response into out.
func (c *Client) do(ctx context.Context, method, path string, in any, wantStatus int, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		// err is a *url.Error, which already names the method and URL.
		return fmt.Errorf("send request: %w", err)
	}
	// Read to EOF and close, so the TCP connection goes back to the pool.
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()
	limited := io.LimitReader(resp.Body, maxResponseBytes)

	if resp.StatusCode != wantStatus {
		return decodeAPIError(resp.StatusCode, limited)
	}
	if err := json.UnmarshalRead(limited, out); err != nil {
		return fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return nil
}

func decodeAPIError(status int, body io.Reader) error {
	apiErr := &APIError{StatusCode: status, Title: http.StatusText(status)}
	var p struct {
		Title     string `json:"title"`
		Detail    string `json:"detail"`
		RequestID string `json:"request_id"`
	}
	// The body may not be JSON at all (a proxy's HTML error page, say).
	// Then we still return the status, which is the important part.
	if err := json.UnmarshalRead(body, &p); err == nil {
		apiErr.Detail = p.Detail
		apiErr.RequestID = p.RequestID
		if p.Title != "" {
			apiErr.Title = p.Title
		}
	}
	return apiErr
}

// Wire types: a deliberate copy of the server's JSON contract.

type itemJSON struct {
	SKU        string `json:"sku"`
	Quantity   int    `json:"quantity"`
	PriceCents int64  `json:"price_cents"`
}

type createOrderRequest struct {
	CustomerID string     `json:"customer_id"`
	Items      []itemJSON `json:"items"`
}

type orderJSON struct {
	ID         uuid.UUID  `json:"id"`
	CustomerID string     `json:"customer_id"`
	Items      []itemJSON `json:"items"`
	Status     string     `json:"status"`
	TotalCents int64      `json:"total_cents"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (o orderJSON) toOrder() order.Order {
	items := make([]order.Item, len(o.Items))
	for i, it := range o.Items {
		items[i] = order.Item(it)
	}
	return order.Order{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Items:      items,
		Status:     order.Status(o.Status),
		CreatedAt:  o.CreatedAt,
	}
}
