package config

import (
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// env turns a map into a getenv function, so tests never touch the real
// process environment and can run in parallel.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadPrecedence(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want Config
	}{
		{
			name: "defaults",
			want: Default(),
		},
		{
			name: "env overrides defaults",
			env:  map[string]string{"ORDERS_ADDR": ":9000", "ORDERS_LOG_LEVEL": "debug", "ORDERS_SHUTDOWN_TIMEOUT": "30s"},
			want: Config{Addr: ":9000", LogLevel: slog.LevelDebug, LogFormat: "json", ShutdownTimeout: 30 * time.Second},
		},
		{
			name: "flags override env",
			args: []string{"-addr", "127.0.0.1:7000", "-log-format", "text"},
			env:  map[string]string{"ORDERS_ADDR": ":9000", "ORDERS_LOG_FORMAT": "json"},
			want: Config{Addr: "127.0.0.1:7000", LogLevel: slog.LevelInfo, LogFormat: "text", ShutdownTimeout: 15 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Load(tt.args, env(tt.env), io.Discard)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantErr []string
	}{
		{name: "bad env duration", env: map[string]string{"ORDERS_SHUTDOWN_DELAY": "soon"}, wantErr: []string{"ORDERS_SHUTDOWN_DELAY"}},
		{name: "bad env level", env: map[string]string{"ORDERS_LOG_LEVEL": "loud"}, wantErr: []string{"ORDERS_LOG_LEVEL"}},
		{name: "unknown flag", args: []string{"-port", "80"}, wantErr: []string{"flag provided but not defined"}},
		{name: "extra args", args: []string{"serve"}, wantErr: []string{"unexpected arguments: serve"}},
		{
			name:    "all validation problems at once",
			args:    []string{"-addr", "nope", "-log-format", "xml", "-shutdown-timeout", "0s"},
			wantErr: []string{"addr", "log format", "shutdown timeout"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Load(tt.args, env(tt.env), io.Discard)
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Load() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadHelp(t *testing.T) {
	var out strings.Builder
	_, err := Load([]string{"-h"}, env(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("Load(-h) error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out.String(), "ORDERS_ADDR") {
		t.Errorf("usage output does not document env vars:\n%s", out.String())
	}
}
