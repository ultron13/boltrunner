package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	k8stesting "k8s.io/client-go/testing"

	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
	"github.com/boltrunner/backend/internal/store/memstore"
)

// errBoom is a generic, non-ErrNotFound failure used to exercise the
// "unexpected store error" (500) branches that the happy-path tests don't
// reach.
var errBoom = errors.New("boom")

// faultyTestStore wraps a real in-memory TestStore and lets individual
// tests force specific methods to fail, without needing a second real
// backing store implementation.
type faultyTestStore struct {
	*memstore.TestStore
	getErr    error
	createErr error
	listErr   error
	updateErr error
}

func (f *faultyTestStore) GetTest(ctx context.Context, id string) (*model.Test, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.TestStore.GetTest(ctx, id)
}

func (f *faultyTestStore) CreateTest(ctx context.Context, t *model.Test) error {
	if f.createErr != nil {
		return f.createErr
	}
	return f.TestStore.CreateTest(ctx, t)
}

func (f *faultyTestStore) UpdateTest(ctx context.Context, t *model.Test) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	return f.TestStore.UpdateTest(ctx, t)
}

func (f *faultyTestStore) ListTests(ctx context.Context) ([]model.Test, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.TestStore.ListTests(ctx)
}

// faultyRunStore wraps a real in-memory RunStore with the same knobs.
type faultyRunStore struct {
	*memstore.RunStore
	createErr       error
	getErr          error
	listErr         error
	updateStatusErr error
	appendErr       error
	listSnapErr     error
}

func (f *faultyRunStore) CreateRun(ctx context.Context, r *model.Run) error {
	if f.createErr != nil {
		return f.createErr
	}
	return f.RunStore.CreateRun(ctx, r)
}

func (f *faultyRunStore) GetRun(ctx context.Context, id string) (*model.Run, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.RunStore.GetRun(ctx, id)
}

func (f *faultyRunStore) ListByTest(ctx context.Context, testID string) ([]model.Run, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.RunStore.ListByTest(ctx, testID)
}

func (f *faultyRunStore) UpdateRunStatus(ctx context.Context, id string, status model.RunStatus, errMsg string) error {
	if f.updateStatusErr != nil {
		return f.updateStatusErr
	}
	return f.RunStore.UpdateRunStatus(ctx, id, status, errMsg)
}

func (f *faultyRunStore) AppendMetricSnapshot(ctx context.Context, s *model.RunMetricSnapshot) error {
	if f.appendErr != nil {
		return f.appendErr
	}
	return f.RunStore.AppendMetricSnapshot(ctx, s)
}

func (f *faultyRunStore) ListSnapshots(ctx context.Context, runID string) ([]model.RunMetricSnapshot, error) {
	if f.listSnapErr != nil {
		return nil, f.listSnapErr
	}
	return f.RunStore.ListSnapshots(ctx, runID)
}

func newServerWithStores(ts store.TestStore, rs store.RunStore, k8sClient *k8sfake.Clientset) *Server {
	ps := memstore.NewProjectStore()
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, ps, k8sClient, cfg)
}

// --- handleStartRun error branches ---

func TestStartRunGetTestStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(), getErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/tests/anything/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunCreateRunError(t *testing.T) {
	ts := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), createErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunInvalidTargetURLFailsPlanGeneration(t *testing.T) {
	ts := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "not-a-url", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	runs, _ := rs.ListByTest(req.Context(), test.ID)
	if len(runs) != 1 || runs[0].Status != model.RunFailed {
		t.Fatalf("expected exactly one failed run, got %+v", runs)
	}
}

func TestStartRunConfigMapCreateError(t *testing.T) {
	ts := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, fakeClient)

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunJobCreateError(t *testing.T) {
	ts := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, fakeClient)

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handlePostMetrics error branches ---

func TestPostMetricsInvalidBody(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/runs/anything/metrics", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPostMetricsStoreError(t *testing.T) {
	ts := memstore.NewTestStore()
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), appendErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	body, _ := json.Marshal(map[string]any{"elapsed_seconds": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/runs/anything/metrics", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleGetRun error branches ---

func TestGetRunStoreError(t *testing.T) {
	ts := memstore.NewTestStore()
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), getErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/runs/anything", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRunListSnapshotsError(t *testing.T) {
	ts := memstore.NewTestStore()
	realRS := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = realRS.CreateRun(context.Background(), run)
	rs := &faultyRunStore{RunStore: realRS, listSnapErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleCancelRun error branches ---

func TestCancelRunJobDeleteError(t *testing.T) {
	ts := memstore.NewTestStore()
	rs := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = rs.CreateRun(context.Background(), run)
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, fakeClient)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelRunUpdateStatusError(t *testing.T) {
	ts := memstore.NewTestStore()
	realRS := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = realRS.CreateRun(context.Background(), run)
	rs := &faultyRunStore{RunStore: realRS, updateStatusErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleListRunsForTest error branches ---

func TestListRunsForTestGetTestStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(), getErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/tests/anything/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListRunsForTestListByTestError(t *testing.T) {
	ts := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), listErr: errBoom}
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleCreateTest / handleListTests error branches ---

func TestCreateTestInvalidBody(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader([]byte("{not json")))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTestMissingFields(t *testing.T) {
	s := newTestServer()
	body, _ := json.Marshal(map[string]any{"name": "", "target_url": "", "virtual_users": 0, "duration_seconds": 0})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTestStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(), createErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	body, _ := json.Marshal(map[string]any{
		"name": "smoke", "target_url": "http://example.com",
		"virtual_users": 10, "duration_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListTestsStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(), listErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/tests", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleUpdateTest error branches ---

func TestUpdateTestConflictReturns409(t *testing.T) {
	realTS := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, updateErr: store.ErrConflict}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	body, _ := json.Marshal(map[string]any{
		"name": "smoke-renamed", "target_url": "http://example.com",
		"virtual_users": 10, "duration_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+test.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusNotFound {
		t.Fatalf("ErrConflict must not be reported as 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("ErrConflict must not be reported as 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateTestStoreErrorReturns500(t *testing.T) {
	realTS := memstore.NewTestStore()
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, updateErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, k8sfake.NewSimpleClientset())

	body, _ := json.Marshal(map[string]any{
		"name": "smoke-renamed", "target_url": "http://example.com",
		"virtual_users": 10, "duration_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+test.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
