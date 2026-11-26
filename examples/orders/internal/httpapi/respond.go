package httpapi

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/acme/orders/internal/order"
)

// problem is an RFC 9457 "problem details" body. Using the standard shape
// means clients, gateways and tools already know how to read our errors.
type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitzero"`
	RequestID string `json:"request_id,omitzero"`
}

// decodeJSON reads a JSON request body into dst. On failure it writes the
// error response itself and returns false, so handlers stay short:
//
//	if !a.decodeJSON(w, r, &req) { return }
func (a *api) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		a.writeProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	err := json.UnmarshalRead(body, dst, json.RejectUnknownMembers(true))
	if err == nil {
		return true
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		a.writeProblem(w, r, http.StatusRequestEntityTooLarge, "request body must not exceed 1 MiB")
		return false
	}
	a.writeProblem(w, r, http.StatusBadRequest, "malformed JSON: "+err.Error())
	return false
}

// writeError maps a service error to a response. This is the single place
// where domain errors become HTTP statuses.
func (a *api) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, order.ErrNotFound):
		a.writeProblem(w, r, http.StatusNotFound, "order not found")
	case errors.Is(err, order.ErrInvalid):
		// Validation messages are written for clients, so they are safe to show.
		a.writeProblem(w, r, http.StatusBadRequest, strings.ReplaceAll(err.Error(), "\n", "; "))
	case errors.Is(err, context.Canceled):
		// The client went away. Nobody will read a response; just note it.
		a.logger.InfoContext(r.Context(), "request canceled by client", slog.String("request_id", requestIDFrom(r.Context())))
	default:
		// Unknown errors are logged in full but never shown: they may contain
		// internal details such as hostnames or SQL.
		a.logger.ErrorContext(r.Context(), "request failed",
			slog.String("request_id", requestIDFrom(r.Context())),
			slog.Any("error", err))
		a.writeProblem(w, r, http.StatusInternalServerError, "")
	}
}

func (a *api) writeProblem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	p := problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    detail,
		RequestID: requestIDFrom(r.Context()),
	}
	a.write(w, r, status, "application/problem+json", p)
}

func (a *api) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	a.write(w, r, status, "application/json", v)
}

// write marshals v fully before sending anything. If marshaling fails we can
// still send a clean 500, instead of a 200 with half a body.
func (a *api) write(w http.ResponseWriter, r *http.Request, status int, contentType string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		a.logger.ErrorContext(r.Context(), "encode response", slog.Any("error", err))
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"about:blank","title":"Internal Server Error","status":500}`+"\n")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	//nolint:gosec // G705: a JSON body with an explicit Content-Type and nosniff is not rendered as HTML.
	if _, err := w.Write(append(body, '\n')); err != nil {
		a.logger.DebugContext(r.Context(), "write response", slog.Any("error", err))
	}
}
