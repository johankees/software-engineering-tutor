package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/acme/orders/internal/order"
)

// Wire types. They are separate from the domain types on purpose: the JSON
// contract with clients must not change just because we rename a field in
// the domain, and the domain must not grow JSON tags.

type itemJSON struct {
	SKU        string `json:"sku"`
	Quantity   int    `json:"quantity"`
	PriceCents int64  `json:"price_cents"`
}

type createOrderRequest struct {
	CustomerID string     `json:"customer_id"`
	Items      []itemJSON `json:"items"`
}

type orderResponse struct {
	ID         uuid.UUID  `json:"id"`
	CustomerID string     `json:"customer_id"`
	Items      []itemJSON `json:"items"`
	Status     string     `json:"status"`
	TotalCents int64      `json:"total_cents"`
	CreatedAt  time.Time  `json:"created_at"`
}

type listOrdersResponse struct {
	Orders []orderResponse `json:"orders"`
}

func toResponse(o order.Order) orderResponse {
	items := make([]itemJSON, len(o.Items))
	for i, it := range o.Items {
		items[i] = itemJSON(it)
	}
	return orderResponse{
		ID:         o.ID,
		CustomerID: o.CustomerID,
		Items:      items,
		Status:     string(o.Status),
		TotalCents: o.TotalCents(),
		CreatedAt:  o.CreatedAt,
	}
}

func (a *api) createOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if !a.decodeJSON(w, r, &req) {
		return
	}
	n := order.NewOrder{CustomerID: req.CustomerID, Items: make([]order.Item, len(req.Items))}
	for i, it := range req.Items {
		n.Items[i] = order.Item(it)
	}

	o, err := a.svc.Create(r.Context(), n)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/orders/"+o.ID.String())
	a.writeJSON(w, r, http.StatusCreated, toResponse(o))
}

func (a *api) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		a.writeProblem(w, r, http.StatusBadRequest, "order ID must be a UUID")
		return
	}
	o, err := a.svc.Get(r.Context(), id)
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	a.writeJSON(w, r, http.StatusOK, toResponse(o))
}

func (a *api) listOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := a.svc.List(r.Context())
	if err != nil {
		a.writeError(w, r, err)
		return
	}
	resp := listOrdersResponse{Orders: make([]orderResponse, len(orders))}
	for i, o := range orders {
		resp.Orders[i] = toResponse(o)
	}
	a.writeJSON(w, r, http.StatusOK, resp)
}
