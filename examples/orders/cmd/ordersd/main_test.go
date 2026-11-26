package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRunServesAndShutsDown starts the whole service in-process, exactly as
// main does, talks to it over real TCP, then stops it like SIGTERM would.
func TestRunServesAndShutsDown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	logs, logW := io.Pipe()
	done := make(chan error, 1)
	go func() {
		err := run(ctx, []string{"-addr", "127.0.0.1:0"}, func(string) string { return "" }, logW, io.Discard)
		_ = logW.Close()
		done <- err
	}()

	lines := bufio.NewScanner(logs)
	addr := waitForLog(t, lines, "listening")["addr"].(string)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /readyz status = %d, want 200", resp.StatusCode)
	}

	cancel() // what SIGTERM does via signal.NotifyContext
	waitForLog(t, lines, "shutdown complete")
	go func() { _, _ = io.Copy(io.Discard, logs) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after cancel")
	}
}

func waitForLog(t *testing.T, lines *bufio.Scanner, msg string) map[string]any {
	t.Helper()
	for lines.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(lines.Bytes(), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q", lines.Text())
		}
		if entry["msg"] == msg {
			return entry
		}
	}
	t.Fatalf("log stream ended before %q", msg)
	return nil
}

func TestRunConfigErrors(t *testing.T) {
	var stderr strings.Builder
	err := run(t.Context(), []string{"-h"}, func(string) string { return "" }, io.Discard, &stderr)
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("run(-h) error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(stderr.String(), "-shutdown-timeout") {
		t.Errorf("usage not printed to stderr:\n%s", stderr.String())
	}

	getenv := func(k string) string {
		if k == "ORDERS_LOG_FORMAT" {
			return "xml"
		}
		return ""
	}
	if err := run(t.Context(), nil, getenv, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "log format") {
		t.Errorf("run() with bad env error = %v, want log format error", err)
	}
}

func TestRunListenError(t *testing.T) {
	err := run(t.Context(), []string{"-addr", "256.0.0.1:80"}, func(string) string { return "" }, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Errorf("run() error = %v, want listen error", err)
	}
}
