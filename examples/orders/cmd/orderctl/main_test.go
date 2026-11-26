package main

import (
	"encoding/json/v2"
	"errors"
	"flag"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/orders/internal/httpapi"
	"github.com/acme/orders/internal/order"
	"github.com/acme/orders/internal/storage/memory"
)

// cli runs orderctl in-process against a real server and returns what it
// printed, like a user at a terminal would see it.
type cli struct {
	t      *testing.T
	server string
}

func newCLI(t *testing.T) cli {
	h := httpapi.NewHandler(order.NewService(memory.New()), slog.New(slog.DiscardHandler), func() bool { return true })
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return cli{t: t, server: srv.URL}
}

func (c cli) run(args ...string) (stdout, stderr string, err error) {
	c.t.Helper()
	var out, errOut strings.Builder
	getenv := func(k string) string {
		if k == "ORDERS_SERVER" {
			return c.server
		}
		return ""
	}
	err = run(c.t.Context(), args, getenv, &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestCreateGetList(t *testing.T) {
	c := newCLI(t)

	stdout, _, err := c.run("-o", "json", "create", "-customer", "c-9", "-item", "BOOK-1:2:1250", "-item", "MUG:1:899")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created []orderOutput
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("create output is not JSON: %v\n%s", err, stdout)
	}
	if len(created) != 1 || created[0].TotalCents != 3399 || created[0].Items != 2 {
		t.Fatalf("created = %+v, want one order of 2 items totalling 3399", created)
	}

	stdout, _, err = c.run("get", created[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(stdout, created[0].ID) || !strings.Contains(stdout, "33.99") {
		t.Errorf("get table output missing id or total:\n%s", stdout)
	}

	stdout, _, err = c.run("list")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if lines := strings.Count(stdout, "\n"); lines != 2 {
		t.Errorf("list printed %d lines, want header + 1 row:\n%s", lines, stdout)
	}
}

func TestExitCodes(t *testing.T) {
	c := newCLI(t)
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"-h"}, exitOK},
		{"version", []string{"version"}, exitOK},
		{"no command", nil, exitUsage},
		{"unknown command", []string{"delete"}, exitUsage},
		{"unknown flag", []string{"-verbose", "list"}, exitUsage},
		{"bad output format", []string{"-o", "yaml", "list"}, exitUsage},
		{"bad item flag", []string{"create", "-item", "BOOK"}, exitUsage},
		{"bad id", []string{"get", "42"}, exitUsage},
		{"not found", []string{"get", "019ac0de-0000-7000-8000-000000000001"}, exitError},
		{"server rejects order", []string{"create", "-customer", "c-1"}, exitError},
		{"server down", []string{"-server", "http://127.0.0.1:1", "list"}, exitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := c.run(tt.args...)
			if got := exitCode(err); got != tt.want {
				t.Errorf("exit code = %d, want %d (err = %v)", got, tt.want, err)
			}
		})
	}
}

func TestNotFoundIsDomainError(t *testing.T) {
	_, _, err := newCLI(t).run("get", "019ac0de-0000-7000-8000-000000000001")
	if !errors.Is(err, order.ErrNotFound) {
		t.Errorf("err = %v, want it to wrap order.ErrNotFound", err)
	}
}

func TestFlagErrorsAreNotPrintedTwice(t *testing.T) {
	_, stderr, err := newCLI(t).run("-verbose")
	if !alreadyReported(err) {
		t.Errorf("flag error not marked as reported: %v", err)
	}
	if !strings.Contains(stderr, "flag provided but not defined") {
		t.Errorf("stderr = %q, want flag package message", stderr)
	}
	if errors.Is(err, flag.ErrHelp) {
		t.Error("unknown flag must not be treated as -h")
	}
}

func TestFormatCents(t *testing.T) {
	for in, want := range map[int64]string{0: "0.00", 5: "0.05", 1250: "12.50", -199: "-1.99"} {
		if got := formatCents(in); got != want {
			t.Errorf("formatCents(%d) = %q, want %q", in, got, want)
		}
	}
}
