package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func newTestServer() *Server {
	ps := memstore.NewProjectStore()
	ts := memstore.NewTestStore(ps)
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, ps, fakeClient, cfg)
}

func metaListOpts() (opts metav1.ListOptions) { return }

func TestStartRunCreatesJob(t *testing.T) {
	s := newTestServer()

	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	if err := s.testStore.CreateTest(nil, test); err != nil {
		t.Fatalf("seed test: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var run model.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.Status != model.RunPending {
		t.Fatalf("expected pending, got %s", run.Status)
	}

	jobs, err := s.k8sClient.BatchV1().Jobs("boltrunner").List(req.Context(), metaListOpts())
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("expected 1 job created, got %d", len(jobs.Items))
	}
}

func TestStartRunUnknownTest(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/tests/missing/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostMetricsAndGetRun(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)
	run := &model.Run{TestID: test.ID, Status: model.RunRunning}
	_ = s.runStore.CreateRun(nil, run)

	body, _ := json.Marshal(map[string]any{
		"elapsed_seconds": 1, "throughput_rps": 12.5, "avg_response_time_ms": 210.0,
		"error_rate_pct": 0.0, "sample_count": 12,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/metrics", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID, nil)
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var resp struct {
		Run     model.Run                 `json:"run"`
		Latest  *model.RunMetricSnapshot  `json:"latest"`
		History []model.RunMetricSnapshot `json:"history"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Latest == nil || resp.Latest.ThroughputRPS != 12.5 {
		t.Fatalf("unexpected latest: %+v", resp.Latest)
	}
	if len(resp.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(resp.History))
	}
}

func TestGetRunUnknown(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/runs/missing", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestPostMetricsUnknownRun(t *testing.T) {
	s := newTestServer()
	body, _ := json.Marshal(map[string]any{"elapsed_seconds": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/missing/metrics", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestCancelRunDeletesJob(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	var run model.Run
	json.Unmarshal(rec.Body.Bytes(), &run)

	cancelReq := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	cancelRec := httptest.NewRecorder()
	s.Router().ServeHTTP(cancelRec, cancelReq)
	if cancelRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", cancelRec.Code, cancelRec.Body.String())
	}

	got, _ := s.runStore.GetRun(cancelReq.Context(), run.ID)
	if got.Status != model.RunStopped {
		t.Fatalf("expected stopped, got %s", got.Status)
	}

	jobs, _ := s.k8sClient.BatchV1().Jobs("boltrunner").List(cancelReq.Context(), metaListOpts())
	if len(jobs.Items) != 0 {
		t.Fatalf("expected job to be deleted, still have %d", len(jobs.Items))
	}
}

func TestCancelRunUnknown(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/runs/missing/cancel", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListRunsForTest(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)
	run1 := &model.Run{TestID: test.ID, Status: model.RunCompleted}
	_ = s.runStore.CreateRun(nil, run1)
	run2 := &model.Run{TestID: test.ID, Status: model.RunRunning}
	_ = s.runStore.CreateRun(nil, run2)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var runs []model.Run
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
}

func TestListRunsForTestEmptyIsNotNull(t *testing.T) {
	s := newTestServer()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = s.testStore.CreateTest(nil, test)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "[]\n" {
		t.Fatalf("expected an empty JSON array, got %q", rec.Body.String())
	}
}

func TestListRunsForUnknownTest(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/tests/missing/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
