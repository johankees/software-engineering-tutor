// Package config loads the ordersd service configuration.
//
// Precedence, from weakest to strongest: built-in defaults, environment
// variables (ORDERS_*), command-line flags. This is the order users expect:
// a container image sets env vars, and a human debugging it can still
// override one value with a flag.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

// Config is the complete, validated service configuration.
type Config struct {
	// Addr is the TCP address to listen on, e.g. ":8080".
	Addr string
	// LogLevel is the minimum level that is logged.
	LogLevel slog.Level
	// LogFormat is "json" (for machines) or "text" (for humans).
	LogFormat string
	// ShutdownDelay is how long the service keeps serving after it reports
	// "not ready", so load balancers can stop sending new traffic first.
	ShutdownDelay time.Duration
	// ShutdownTimeout is the maximum time to finish in-flight requests.
	ShutdownTimeout time.Duration
}

// Default returns the configuration used when nothing is set.
func Default() Config {
	return Config{
		Addr:            ":8080",
		LogLevel:        slog.LevelInfo,
		LogFormat:       "json",
		ShutdownDelay:   0,
		ShutdownTimeout: 15 * time.Second,
	}
}

// Load builds a Config from defaults, then getenv, then args (without the
// program name). Usage and flag errors are written to output.
//
// It returns flag.ErrHelp if the user asked for -h or -help.
func Load(args []string, getenv func(string) string, output io.Writer) (Config, error) {
	cfg := Default()
	if err := cfg.applyEnv(getenv); err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("ordersd", flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen `address` (env ORDERS_ADDR)")
	fs.TextVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "minimum log `level`: debug, info, warn, error (env ORDERS_LOG_LEVEL)")
	fs.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log `format`: json or text (env ORDERS_LOG_FORMAT)")
	fs.DurationVar(&cfg.ShutdownDelay, "shutdown-delay", cfg.ShutdownDelay, "serve this long after turning not-ready (env ORDERS_SHUTDOWN_DELAY)")
	fs.DurationVar(&cfg.ShutdownTimeout, "shutdown-timeout", cfg.ShutdownTimeout, "max time to drain requests (env ORDERS_SHUTDOWN_TIMEOUT)")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnv(getenv func(string) string) error {
	var errs []error
	if v := getenv("ORDERS_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("ORDERS_LOG_LEVEL"); v != "" {
		if err := c.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("ORDERS_LOG_LEVEL: %w", err))
		}
	}
	if v := getenv("ORDERS_LOG_FORMAT"); v != "" {
		c.LogFormat = v
	}
	durations := []struct {
		key string
		dst *time.Duration
	}{
		{"ORDERS_SHUTDOWN_DELAY", &c.ShutdownDelay},
		{"ORDERS_SHUTDOWN_TIMEOUT", &c.ShutdownTimeout},
	}
	for _, d := range durations {
		v := getenv(d.key)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.key, err))
			continue
		}
		*d.dst = parsed
	}
	return errors.Join(errs...)
}

// Validate reports every invalid field at once.
func (c Config) Validate() error {
	var errs []error
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		errs = append(errs, fmt.Errorf("addr %q: %w", c.Addr, err))
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("log format %q: must be json or text", c.LogFormat))
	}
	if c.ShutdownDelay < 0 {
		errs = append(errs, fmt.Errorf("shutdown delay %v: must not be negative", c.ShutdownDelay))
	}
	if c.ShutdownTimeout <= 0 {
		errs = append(errs, fmt.Errorf("shutdown timeout %v: must be positive", c.ShutdownTimeout))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	return nil
}
