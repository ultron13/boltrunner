package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func TestCreateAndListTests(t *testing.T) {
	s := newTestServer()

	body, _ := json.Marshal(map[string]any{
		"name": "smoke", "target_url": "http://example.com",
		"virtual_users": 10, "duration_seconds": 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected an ID")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/tests", nil)
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec2.Code)
	}
	var list []model.Test
	if err := json.Unmarshal(rec2.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 test, got %d", len(list))
	}
}

func createTestViaAPI(t *testing.T, s *Server, name string) model.Test {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"name": name, "target_url": "http://example.com",
		"virtual_users": 1, "duration_seconds": 1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed test: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode seeded test: %v", err)
	}
	return created
}

func TestUpdateTestCreatesANewVersion(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "editable")

	body, _ := json.Marshal(map[string]any{
		"name": "editable", "target_url": "http://changed",
		"virtual_users": 7, "duration_seconds": 70,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
	if updated.ID != created.ID {
		t.Fatalf("expected a stable catalog id %q, got %q", created.ID, updated.ID)
	}
	if updated.TargetURL != "http://changed" {
		t.Fatalf("expected the edited target url, got %q", updated.TargetURL)
	}
}

func TestUpdateTestValidatesBodyAndID(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "validated")

	// Malformed JSON.
	req := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader([]byte("{")))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed json, got %d", rec.Code)
	}

	// Valid JSON, invalid values.
	bad, _ := json.Marshal(map[string]any{
		"name": "", "target_url": "", "virtual_users": 0, "duration_seconds": 0,
	})
	req2 := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(bad))
	rec2 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid values, got %d", rec2.Code)
	}

	// Unknown test.
	good, _ := json.Marshal(map[string]any{
		"name": "x", "target_url": "http://example.com",
		"virtual_users": 1, "duration_seconds": 1,
	})
	req3 := httptest.NewRequest(http.MethodPut, "/api/tests/missing", bytes.NewReader(good))
	rec3 := httptest.NewRecorder()
	s.Router().ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown test, got %d", rec3.Code)
	}
}

func TestListTestVersionsReturnsNewestFirst(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "history")

	body, _ := json.Marshal(map[string]any{
		"name": "history", "target_url": "http://v2",
		"virtual_users": 1, "duration_seconds": 1,
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(body))
	s.Router().ServeHTTP(httptest.NewRecorder(), putReq)

	req := httptest.NewRequest(http.MethodGet, "/api/tests/"+created.ID+"/versions", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var versions []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected newest-first, got v%d then v%d", versions[0].Version, versions[1].Version)
	}
}

func TestListTestVersionsUnknownTestIs404(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/tests/missing/versions", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestListProjectsReturnsTheDefaultProject(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var projects []model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "Default" {
		t.Fatalf("expected exactly the Default project, got %+v", projects)
	}
}

// TestRunIsPinnedToTheExecutedVersion guards the exact line that makes run
// pinning work (runs.go's handleStartRun sets TestID: test.VersionID). Every
// other test in this package only ever creates v1 tests, where VersionID ==
// ID in memstore -- so a regression to TestID: test.ID would pass the rest of
// the suite silently. Editing the test first forces VersionID != ID, so a run
// started afterward can only pin correctly if the version id, not the catalog
// id, is actually used.
func TestRunIsPinnedToTheExecutedVersion(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "pinned")

	body, _ := json.Marshal(map[string]any{
		"name": "pinned", "target_url": "http://changed",
		"virtual_users": 7, "duration_seconds": 70,
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/tests/"+created.ID, bytes.NewReader(body))
	putRec := httptest.NewRecorder()
	s.Router().ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
	var updated model.Test
	if err := json.Unmarshal(putRec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated test: %v", err)
	}
	// The test proves nothing unless versioning actually produced a distinct
	// version id -- assert that loudly rather than let a versioning
	// regression masquerade as a pinning regression.
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
	if updated.VersionID == created.ID {
		t.Fatalf("expected a version id distinct from the catalog id %q, got the same value", created.ID)
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/tests/"+created.ID+"/runs", nil)
	runRec := httptest.NewRecorder()
	s.Router().ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", runRec.Code, runRec.Body.String())
	}
	var run model.Run
	if err := json.Unmarshal(runRec.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.TestID != updated.VersionID {
		t.Fatalf("expected run.TestID (%q) to pin the executed version.VersionID (%q)", run.TestID, updated.VersionID)
	}
	if run.TestCatalogID != created.ID {
		t.Fatalf("expected run.TestCatalogID (%q) to be the catalog id (%q)", run.TestCatalogID, created.ID)
	}

	historyReq := httptest.NewRequest(http.MethodGet, "/api/tests/"+created.ID+"/runs", nil)
	historyRec := httptest.NewRecorder()
	s.Router().ServeHTTP(historyRec, historyReq)
	if historyRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", historyRec.Code, historyRec.Body.String())
	}
	var runs []model.Run
	if err := json.Unmarshal(historyRec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode run history: %v", err)
	}
	found := false
	for _, r := range runs {
		if r.ID == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the pinned run %q to appear in the catalog id's run history, got %+v", run.ID, runs)
	}
}

func TestCreatedTestBelongsToTheDefaultProject(t *testing.T) {
	s := newTestServer()
	created := createTestViaAPI(t, s, "projected")

	if created.ProjectID == "" {
		t.Fatal("expected the created test to carry a project id")
	}
	if created.Version != 1 {
		t.Fatalf("expected version 1, got %d", created.Version)
	}
	if created.VersionID == "" {
		t.Fatal("expected a version_id")
	}
}

// An unknown project_id is a client mistake, not a server fault: it must not
// surface as a 500. Both stores reject it -- postgres via the
// tests.project_id foreign key, memstore because Default is the only project
// it knows -- so the handler can map it to 400 uniformly.
func TestCreateTestWithUnknownProjectIsRejected(t *testing.T) {
	s := newTestServer()

	body, _ := json.Marshal(map[string]any{
		"name": "orphan", "target_url": "http://example.com",
		"virtual_users": 1, "duration_seconds": 1,
		"project_id": "99999999-9999-9999-9999-999999999999",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tests", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		t.Fatalf("an unknown project_id must not surface as a 500: %s", rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListTestsFiltersByProjectIDWhenGiven(t *testing.T) {
	srv := newTestServer()

	body := `{"name":"in-default","target_url":"http://a","virtual_users":1,"duration_seconds":1,"project_id":"` + memstore.DefaultProjectID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests?project_id="+memstore.DefaultProjectID, nil))
	var scoped []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &scoped); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(scoped) != 1 {
		t.Fatalf("expected 1 test in Default, got %d", len(scoped))
	}

	rec = httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests?project_id=00000000-0000-0000-0000-0000000000ff", nil))
	var empty []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no tests for an unknown project, got %d", len(empty))
	}
}

// The three e2e specs and the Go integration test all call GET /api/tests with
// no parameter and expect everything back.
func TestListTestsWithoutProjectIDStaysUnfiltered(t *testing.T) {
	srv := newTestServer()
	body := `{"name":"anything","target_url":"http://a","virtual_users":1,"duration_seconds":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tests", nil))
	var all []model.Test
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected the unfiltered list to contain the test, got %d", len(all))
	}
}
