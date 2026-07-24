//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func baseURL() string {
	if v := os.Getenv("BOLTRUNNER_API_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestWalkingSkeletonEndToEnd(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}

	createBody, _ := json.Marshal(map[string]any{
		"name": "ci-integration-smoke", "target_url": baseURL() + "/healthz",
		"virtual_users": 3, "duration_seconds": 15,
	})
	resp, err := client.Post(baseURL()+"/api/tests", "application/json", bytes.NewReader(createBody))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("create test failed: err=%v status=%v", err, resp)
	}
	var test struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&test)
	resp.Body.Close()

	runResp, err := client.Post(baseURL()+"/api/tests/"+test.ID+"/runs", "application/json", nil)
	if err != nil || runResp.StatusCode != http.StatusCreated {
		t.Fatalf("start run failed: err=%v status=%v", err, runResp)
	}
	var run struct {
		ID string `json:"id"`
	}
	json.NewDecoder(runResp.Body).Decode(&run)
	runResp.Body.Close()

	deadline := time.Now().Add(90 * time.Second)
	var finalStatus string
	var sawMetrics bool
	for time.Now().Before(deadline) {
		getResp, err := client.Get(baseURL() + "/api/runs/" + run.ID)
		if err != nil {
			t.Fatalf("get run failed: %v", err)
		}
		var body struct {
			Run struct {
				Status string `json:"status"`
			} `json:"run"`
			Latest *struct {
				SampleCount int `json:"sample_count"`
			} `json:"latest"`
		}
		json.NewDecoder(getResp.Body).Decode(&body)
		getResp.Body.Close()

		if body.Latest != nil && body.Latest.SampleCount > 0 {
			sawMetrics = true
		}
		if body.Run.Status == "completed" || body.Run.Status == "failed" {
			finalStatus = body.Run.Status
			break
		}
		time.Sleep(2 * time.Second)
	}

	if finalStatus != "completed" {
		t.Fatalf("expected run to complete, got status=%q", finalStatus)
	}
	if !sawMetrics {
		t.Fatal("expected at least one non-zero metric snapshot during the run")
	}
}
