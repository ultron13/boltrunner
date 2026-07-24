package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/boltrunner/backend/internal/jtl"
)

func readNewLines(f *os.File, offset int64) ([]string, int64, error) {
	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}
	var lines []string
	scanner := bufio.NewScanner(f)
	newOffset := offset
	for scanner.Scan() {
		line := scanner.Text()
		newOffset += int64(len(line)) + 1 // +1 for the newline
		lines = append(lines, line)
	}
	return lines, newOffset, scanner.Err()
}

func postSnapshot(backendURL, runID string, agg jtl.AggregateResult, elapsedSeconds int) error {
	payload := map[string]any{
		"elapsed_seconds":      elapsedSeconds,
		"throughput_rps":       agg.ThroughputRPS,
		"avg_response_time_ms": agg.AvgResponseTimeMs,
		"error_rate_pct":       agg.ErrorRatePct,
		"sample_count":         agg.SampleCount,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	url := strings.TrimRight(backendURL, "/") + "/api/runs/" + runID + "/metrics"
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func main() {
	runID := os.Getenv("RUN_ID")
	backendURL := os.Getenv("BACKEND_URL")
	jtlPath := os.Getenv("JTL_PATH")
	donePath := os.Getenv("DONE_PATH")

	var f *os.File
	var err error
	for {
		f, err = os.Open(jtlPath)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	defer f.Close()

	var offset int64
	elapsed := 0
	for {
		lines, newOffset, err := readNewLines(f, offset)
		offset = newOffset
		if err != nil {
			log.Printf("read error: %v", err)
		}

		var samples []jtl.Sample
		for _, line := range lines {
			s, ok, perr := jtl.ParseLine(line)
			if perr == nil && ok {
				samples = append(samples, s)
			}
		}
		elapsed++
		agg := jtl.Aggregate(samples, 1.0)
		if err := postSnapshot(backendURL, runID, agg, elapsed); err != nil {
			log.Printf("post snapshot failed: %v", err)
		}

		if _, err := os.Stat(donePath); err == nil {
			log.Println("done marker found, exiting")
			return
		}
		time.Sleep(1 * time.Second)
	}
}
