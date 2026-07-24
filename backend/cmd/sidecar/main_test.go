package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/boltrunner/backend/internal/jtl"
)

func TestReadNewLinesIncremental(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "results-*.jtl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer f.Close()

	f.WriteString("header\nline1\n")
	lines, offset, err := readNewLines(f, 0)
	if err != nil {
		t.Fatalf("readNewLines: %v", err)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}

	f.WriteString("line2\n")
	more, offset2, err := readNewLines(f, offset)
	if err != nil {
		t.Fatalf("readNewLines: %v", err)
	}
	if len(more) != 1 || more[0] != "line2" {
		t.Fatalf("expected [line2], got %v", more)
	}
	if offset2 <= offset {
		t.Fatalf("expected offset to advance, got %d -> %d", offset, offset2)
	}
}

func TestPostSnapshotSendsExpectedJSON(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	agg := jtl.AggregateResult{ThroughputRPS: 5, AvgResponseTimeMs: 120, ErrorRatePct: 1.5, SampleCount: 5}
	if err := postSnapshot(srv.URL, "run-1", agg, 3); err != nil {
		t.Fatalf("postSnapshot: %v", err)
	}
	if received["elapsed_seconds"].(float64) != 3 {
		t.Fatalf("unexpected payload: %+v", received)
	}
	if received["throughput_rps"].(float64) != 5 {
		t.Fatalf("unexpected payload: %+v", received)
	}
}

func TestPostSnapshotPostErrorIsReturned(t *testing.T) {
	agg := jtl.AggregateResult{ThroughputRPS: 5, AvgResponseTimeMs: 120, ErrorRatePct: 1.5, SampleCount: 5}
	// Port 0 on loopback is guaranteed to refuse the connection, forcing
	// http.Post to fail without needing a real unreachable host.
	err := postSnapshot("http://127.0.0.1:0", "run-1", agg, 3)
	if err == nil {
		t.Fatal("expected an error when the backend is unreachable")
	}
}

func TestReadNewLinesSeekErrorIsReturned(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "results-*.jtl")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	f.Close() // Seeking on a closed file returns an error.

	if _, _, err := readNewLines(f, 0); err == nil {
		t.Fatal("expected an error when seeking on a closed file")
	}
}
