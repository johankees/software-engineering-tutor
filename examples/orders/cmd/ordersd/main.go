// Command ordersd serves the orders HTTP API.
//
// Usage:
//
//	ordersd [flags]
//
// Run "ordersd -h" for the flags. Every flag can also be set with an
// ORDERS_* environment variable; flags win.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/acme/orders/internal/buildinfo"
	"github.com/acme/orders/internal/config"
	"github.com/acme/orders/internal/httpapi"
	"github.com/acme/orders/internal/order"
	"github.com/acme/orders/internal/storage/memory"
)

// HTTP server limits. These protect the process from slow or hostile
// clients; they are engineering decisions, not deployment settings.
const (
	readHeaderTimeout = 5 * time.Second  // slow-header ("Slowloris") attacks
	readTimeout       = 10 * time.Second // slow request bodies
	writeTimeout      = 15 * time.Second // slow readers of our responses
	idleTimeout       = 60 * time.Second // idle keep-alive connections
	maxHeaderBytes    = 64 << 10         // 64 KiB of request headers
)

func main() {
	// SIGTERM is what Kubernetes, systemd and Docker send to stop a process.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		// After the first signal, restore default handling: a second Ctrl-C
		// kills the process at once instead of waiting for shutdown.
		<-ctx.Done()
		stop()
	}()

	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	switch {
	case err == nil:
	case errors.Is(err, flag.ErrHelp):
		// Usage was already printed.
	default:
		fmt.Fprintln(os.Stderr, "ordersd:", err)
		os.Exit(1)
	}
}

// run is the real main. It takes everything from the outside world as
// parameters, so tests can call it with their own args, environment and
// output, and stop it by cancelling ctx.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	cfg, err := config.Load(args, getenv, stderr)
	if err != nil {
		return err
	}
	logger := newLogger(cfg, stdout)

	info := buildinfo.Read()
	logger.InfoContext(ctx, "starting",
		slog.String("version", info.Version),
		slog.String("revision", info.Revision),
		slog.String("go_version", info.GoVersion))

	// Wiring: build every dependency here, explicitly, and pass it down.
	// Swapping the store for a database means changing this one line.
	svc := order.NewService(memory.New())
	var ready atomic.Bool

	srv := &http.Server{
		Handler:           httpapi.NewHandler(svc, logger, ready.Load),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	// Listen first, then serve: with "-addr :0" the OS picks a free port,
	// and we can log the real address.
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	ready.Store(true)
	logger.InfoContext(ctx, "listening", slog.String("addr", ln.Addr().String()))

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	return shutdown(ctx, cfg, logger, srv, &ready, serveErr)
}

// shutdown stops the server in three steps:
//  1. report "not ready" and keep serving for ShutdownDelay, so load
//     balancers notice and stop sending new requests;
//  2. stop accepting connections and wait for in-flight requests, up to
//     ShutdownTimeout;
//  3. if that takes too long, close all connections hard.
func shutdown(ctx context.Context, cfg config.Config, logger *slog.Logger, srv *http.Server, ready *atomic.Bool, serveErr <-chan error) error {
	logger.InfoContext(ctx, "shutting down",
		slog.Duration("delay", cfg.ShutdownDelay),
		slog.Duration("timeout", cfg.ShutdownTimeout))
	ready.Store(false)
	time.Sleep(cfg.ShutdownDelay)

	// ctx is already cancelled. WithoutCancel keeps its values but not its
	// cancellation, so the shutdown gets its own, fresh deadline.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		closeErr := srv.Close()
		return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	logger.InfoContext(ctx, "shutdown complete")
	return nil
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var h slog.Handler = slog.NewJSONHandler(w, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(w, opts)
	}
	return slog.New(h).With(slog.String("service", "ordersd"))
}
