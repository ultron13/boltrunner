package api

import (
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
	ts := memstore.NewTestStore()
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, fakeClient, cfg)
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
