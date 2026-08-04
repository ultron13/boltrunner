package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	getErr            error
	createErr         error
	listErr           error
	updateErr         error
	listForProjectErr error
	moveErr           error
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

func (f *faultyTestStore) ListTestsForProject(ctx context.Context, projectID string) ([]model.Test, error) {
	if f.listForProjectErr != nil {
		return nil, f.listForProjectErr
	}
	return f.TestStore.ListTestsForProject(ctx, projectID)
}

func (f *faultyTestStore) MoveTest(ctx context.Context, catalogID, projectID string) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	return f.TestStore.MoveTest(ctx, catalogID, projectID)
}

// faultyProjectStore wraps a real in-memory ProjectStore and lets individual
// tests force specific methods to fail, without needing a second real
// backing store implementation. It embeds the same *memstore.ProjectStore
// that the accompanying TestStore is built on (see newServerWithStores
// callers below) so the two never disagree about what projects exist --
// only the call this test cares about is intercepted.
type faultyProjectStore struct {
	*memstore.ProjectStore
	listErr   error
	deleteErr error
}

func (f *faultyProjectStore) ListProjects(ctx context.Context) ([]model.Project, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.ProjectStore.ListProjects(ctx)
}

func (f *faultyProjectStore) DeleteProject(ctx context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return f.ProjectStore.DeleteProject(ctx, id)
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

// newServerWithStores takes ps explicitly (rather than building one itself)
// so a test that needs a real, non-default project can wire the exact same
// store the TestStore was built on through to the server -- see the
// package-level warning above faultyProjectStore. Callers that don't care
// pass a fresh memstore.NewProjectStore().
func newServerWithStores(ts store.TestStore, rs store.RunStore, ps store.ProjectStore, k8sClient *k8sfake.Clientset) *Server {
	cfg := k8sjob.Config{Namespace: "boltrunner", JMeterImage: "jmeter:local", SidecarImage: "sidecar:local", BackendURL: "http://backend:8080"}
	return NewServer(ts, rs, ps, k8sClient, cfg)
}

// --- handleStartRun error branches ---

func TestStartRunGetTestStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(memstore.NewProjectStore()), getErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/tests/anything/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunCreateRunError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), createErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunInvalidTargetURLFailsPlanGeneration(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "not-a-url", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), fakeClient)

	req := httptest.NewRequest(http.MethodPost, "/api/tests/"+test.ID+"/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestStartRunJobCreateError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := memstore.NewRunStore()
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), fakeClient)

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
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), appendErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), getErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/runs/anything", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRunListSnapshotsError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	realRS := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = realRS.CreateRun(context.Background(), run)
	rs := &faultyRunStore{RunStore: realRS, listSnapErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+run.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleCancelRun error branches ---

func TestCancelRunJobDeleteError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	rs := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = rs.CreateRun(context.Background(), run)
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("delete", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), fakeClient)

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelRunUpdateStatusError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	realRS := memstore.NewRunStore()
	run := &model.Run{TestID: "t1", Status: model.RunRunning}
	_ = realRS.CreateRun(context.Background(), run)
	rs := &faultyRunStore{RunStore: realRS, updateStatusErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPost, "/api/runs/"+run.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleListRunsForTest error branches ---

func TestListRunsForTestGetTestStoreError(t *testing.T) {
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(memstore.NewProjectStore()), getErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/tests/anything/runs", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListRunsForTestListByTestError(t *testing.T) {
	ts := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = ts.CreateTest(context.Background(), test)
	rs := &faultyRunStore{RunStore: memstore.NewRunStore(), listErr: errBoom}
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(memstore.NewProjectStore()), createErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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
	ts := &faultyTestStore{TestStore: memstore.NewTestStore(memstore.NewProjectStore()), listErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodGet, "/api/tests", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleUpdateTest error branches ---

func TestUpdateTestConflictReturns409(t *testing.T) {
	realTS := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, updateErr: store.ErrConflict}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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
	realTS := memstore.NewTestStore(memstore.NewProjectStore())
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 5, DurationSeconds: 10}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, updateErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, memstore.NewProjectStore(), k8sfake.NewSimpleClientset())

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

// --- handleDeleteProject error branches ---
//
// Every test in this block shares one real *memstore.ProjectStore between
// the TestStore and the (possibly faulty) ProjectStore passed to the
// server. Building the TestStore on a *different* ProjectStore than the one
// the handler queries would let the two disagree about which projects
// exist -- exactly the trap called out on faultyProjectStore above.

func TestDeleteProjectListProjectsErrorReturns500(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	ps := &faultyProjectStore{ProjectStore: realPS, listErr: errBoom}
	ts := memstore.NewTestStore(realPS)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, ps, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteProjectListTestsForProjectErrorReturns500(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	realTS := memstore.NewTestStore(realPS)
	ts := &faultyTestStore{TestStore: realTS, listForProjectErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, realPS, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// A DeleteProject call that returns ErrNotFound simulates a project deleted
// by a concurrent request between the handler's existence check and the
// delete itself.
func TestDeleteProjectStoreNotFoundReturns404(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	ps := &faultyProjectStore{ProjectStore: realPS, deleteErr: store.ErrNotFound}
	ts := memstore.NewTestStore(realPS)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, ps, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ErrProtected out of DeleteProject itself is unreachable in production --
// is_default is only ever set by migration 0005, so the handler's own
// IsDefault check above always catches it first. It is exercised here only
// to prove the branch still maps to the right status if the store ever
// grows a second way to protect a project; it is not a real runtime path.
func TestDeleteProjectStoreProtectedReturns409(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	ps := &faultyProjectStore{ProjectStore: realPS, deleteErr: store.ErrProtected}
	ts := memstore.NewTestStore(realPS)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, ps, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "default project cannot be deleted") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// ErrNotEmpty out of DeleteProject itself simulates a test filed under the
// project between the handler's count and the delete. The message must not
// repeat a count -- re-reading it now would report a number already stale.
func TestDeleteProjectStoreNotEmptyReturns409(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	ps := &faultyProjectStore{ProjectStore: realPS, deleteErr: store.ErrNotEmpty}
	ts := memstore.NewTestStore(realPS)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, ps, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Payments still has tests; move or delete them first") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestDeleteProjectStoreErrorReturns500(t *testing.T) {
	realPS := memstore.NewProjectStore()
	proj := &model.Project{Name: "Payments"}
	_ = realPS.CreateProject(context.Background(), proj)
	ps := &faultyProjectStore{ProjectStore: realPS, deleteErr: errBoom}
	ts := memstore.NewTestStore(realPS)
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, ps, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+proj.ID, nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- handleMoveTest error branches ---

func TestMoveTestStoreErrorReturns500(t *testing.T) {
	realPS := memstore.NewProjectStore()
	dest := &model.Project{Name: "Billing"}
	_ = realPS.CreateProject(context.Background(), dest)
	realTS := memstore.NewTestStore(realPS)
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, moveErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, realPS, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+test.ID+"/project",
		strings.NewReader(`{"project_id":"`+dest.ID+`"}`))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestMoveTestGetTestErrorAfterMoveReturns500(t *testing.T) {
	realPS := memstore.NewProjectStore()
	dest := &model.Project{Name: "Billing"}
	_ = realPS.CreateProject(context.Background(), dest)
	realTS := memstore.NewTestStore(realPS)
	test := &model.Test{Name: "smoke", TargetURL: "http://example.com", VirtualUsers: 1, DurationSeconds: 1}
	_ = realTS.CreateTest(context.Background(), test)
	ts := &faultyTestStore{TestStore: realTS, getErr: errBoom}
	rs := memstore.NewRunStore()
	s := newServerWithStores(ts, rs, realPS, k8sfake.NewSimpleClientset())

	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+test.ID+"/project",
		strings.NewReader(`{"project_id":"`+dest.ID+`"}`))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
