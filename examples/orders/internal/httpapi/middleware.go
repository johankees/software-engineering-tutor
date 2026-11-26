package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
	"uuid"
)

// Middleware is a plain func(http.Handler) http.Handler. No framework is
// needed: every middleware wraps the next handler and calls it.

type ctxKey int

const requestIDKey ctxKey = iota

// requestIDFrom returns the request ID stored by withRequestID, or "".
func requestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// withRequestID gives every request an ID, so one request can be followed
// through logs of many services. A valid incoming X-Request-ID (set by a
// gateway or caller) is kept; otherwise a new UUIDv7 is generated.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = uuid.NewV7().String()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validRequestID accepts short, printable ASCII IDs only. Anything else
// could be used to inject fake lines or huge values into our logs.
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for i := range len(id) {
		if id[i] < '!' || id[i] > '~' {
			return false
		}
	}
	return true
}

// statusRecorder remembers the status code and body size for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the real writer (for Flush,
// deadlines, ...) through this wrapper.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// logRequests writes one structured log line per request.
func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		// Probes run every few seconds; logging them at info drowns real traffic.
		level := slog.LevelInfo
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			level = slog.LevelDebug
		}
		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("request_id", requestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route", r.Pattern),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// recoverPanic turns a panic in a handler into a 500 and a log line with
// the stack trace. net/http would recover too, but it just drops the
// connection and prints to stderr, outside our structured logs.
func recoverPanic(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer handlePanic(logger, w, r)
		next.ServeHTTP(w, r)
	})
}

// handlePanic must be deferred directly: recover only works when called
// by the deferred function itself.
func handlePanic(logger *slog.Logger, w http.ResponseWriter, r *http.Request) {
	v := recover()
	if v == nil {
		return
	}
	if v == http.ErrAbortHandler { //nolint:errorlint // a panic value, compared exactly like net/http does
		panic(v) // a deliberate abort: let net/http handle it
	}
	ctx := r.Context()
	logger.ErrorContext(ctx, "panic in handler",
		slog.String("request_id", requestIDFrom(ctx)),
		slog.String("panic", fmt.Sprint(v)),
		slog.String("stack", string(debug.Stack())))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = fmt.Fprintf(w, `{"type":"about:blank","title":"Internal Server Error","status":500,"request_id":%q}`+"\n", requestIDFrom(ctx))
}
