package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/boltrunner/backend/internal/model"
)

func TestCreateProjectReturns201AndTheProject(t *testing.T) {
	srv := newTestServer()
	body := strings.NewReader(`{"name":"Payments"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", body)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Payments" || got.ID == "" {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestCreateProjectRejectsAnEmptyName(t *testing.T) {
	for _, name := range []string{`""`, `"   "`} {
		srv := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":`+name+`}`))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("name=%s: expected 400, got %d", name, rec.Code)
		}
	}
}

func TestCreateProjectRejectsAnOverlongName(t *testing.T) {
	srv := newTestServer()
	long := strings.Repeat("a", 101)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+long+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateProjectAcceptsANameAtTheLimit(t *testing.T) {
	srv := newTestServer()
	atLimit := strings.Repeat("a", 100)
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+atLimit+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a name exactly at the limit, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// The 100-character limit must count runes, not bytes: a 34-character
// Japanese name is well under the limit the error message promises, but each
// character is 3 bytes in UTF-8, so len() on the raw string would put it over
// 100 and reject it with a message ("100 characters or fewer") that does not
// match what actually happened.
func TestCreateProjectAcceptsAMultiByteNameUnderTheRuneLimit(t *testing.T) {
	srv := newTestServer()
	name := strings.Repeat("あ", 34) // 34 runes, 102 bytes
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+name+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for a 34-rune (102-byte) name, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateProjectRejectsAMalformedBody(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateProjectReturns409ForADuplicateName(t *testing.T) {
	srv := newTestServer()
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"Payments"}`))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if i == 0 && rec.Code != http.StatusCreated {
			t.Fatalf("first create: expected 201, got %d", rec.Code)
		}
		if i == 1 && rec.Code != http.StatusConflict {
			t.Fatalf("second create: expected 409, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
}

// A name that differs only by surrounding whitespace is the same name: it is
// trimmed before the uniqueness check, so it must conflict rather than create
// a near-duplicate the user cannot tell apart in the switcher.
func TestCreateProjectTrimsBeforeCheckingUniqueness(t *testing.T) {
	srv := newTestServer()
	first := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"Payments"}`))
	srv.Router().ServeHTTP(httptest.NewRecorder(), first)

	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"  Payments  "}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a whitespace-padded duplicate, got %d", rec.Code)
	}
}

// The trimmed name is what gets stored, so the switcher never shows a name
// with invisible padding the user did not intend.
func TestCreateProjectStoresTheTrimmedName(t *testing.T) {
	srv := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"  Payments  "}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)

	var got model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Payments" {
		t.Fatalf("expected the stored name to be trimmed, got %q", got.Name)
	}
}

func createProjectViaAPI(t *testing.T, srv *Server, name string) model.Project {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(`{"name":"`+name+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed project %q: expected 201, got %d (%s)", name, rec.Code, rec.Body.String())
	}
	var p model.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode seeded project: %v", err)
	}
	return p
}

func renameProjectViaAPI(srv *Server, id, name string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+id, strings.NewReader(`{"name":"`+name+`"}`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestRenameProjectReturns200AndTheNewName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")

	rec := renameProjectViaAPI(srv, p.ID, "Billing")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got model.Project
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Billing" || got.ID != p.ID {
		t.Fatalf("unexpected project: %+v", got)
	}
}

func TestRenameProjectReturns409ForATakenName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	rec := renameProjectViaAPI(srv, p.ID, "Default")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "a project with that name already exists") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestRenameProjectReturns404ForAnUnknownID(t *testing.T) {
	srv := newTestServer()
	rec := renameProjectViaAPI(srv, "no-such-id", "Billing")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "project not found") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestRenameProjectRejectsABlankName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	rec := renameProjectViaAPI(srv, p.ID, "   ")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestRenameProjectRejectsAnOverlongName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	rec := renameProjectViaAPI(srv, p.ID, strings.Repeat("a", 101))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name must be 100 characters or fewer") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestRenameProjectRejectsAMalformedBody(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+p.ID, strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// The trimmed name is what gets stored, matching create.
func TestRenameProjectStoresTheTrimmedName(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	rec := renameProjectViaAPI(srv, p.ID, "  Billing  ")
	var got model.Project
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Name != "Billing" {
		t.Fatalf("expected a trimmed name, got %q", got.Name)
	}
}

func deleteProjectViaAPI(srv *Server, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/projects/"+id, nil)
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	return rec
}

func TestDeleteProjectReturns204AndRemovesIt(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")

	if rec := deleteProjectViaAPI(srv, p.ID); rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var list []model.Project
	json.Unmarshal(listRec.Body.Bytes(), &list)
	for _, l := range list {
		if l.ID == p.ID {
			t.Fatal("expected the project to be gone from the list")
		}
	}
}

func TestDeleteProjectReturns404ForAnUnknownID(t *testing.T) {
	srv := newTestServer()
	rec := deleteProjectViaAPI(srv, "no-such-id")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "project not found") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

func TestDeleteProjectRefusesTheDefault(t *testing.T) {
	srv := newTestServer()
	listRec := httptest.NewRecorder()
	srv.Router().ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/api/projects", nil))
	var list []model.Project
	json.Unmarshal(listRec.Body.Bytes(), &list)
	var defaultID string
	for _, l := range list {
		if l.IsDefault {
			defaultID = l.ID
		}
	}
	if defaultID == "" {
		t.Fatal("expected a default project in the list")
	}

	rec := deleteProjectViaAPI(srv, defaultID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "default project cannot be deleted") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// The message names the project and the count, because "it still has tests" on
// its own does not tell the user how much work emptying it is.
func TestDeleteProjectRefusesANonEmptyProjectAndNamesTheCount(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	for i := 0; i < 2; i++ {
		body := `{"name":"t","target_url":"http://x","virtual_users":1,"duration_seconds":1,"project_id":"` + p.ID + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed test: expected 201, got %d (%s)", rec.Code, rec.Body.String())
		}
	}

	rec := deleteProjectViaAPI(srv, p.ID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Payments still has 2 tests") {
		t.Fatalf("unexpected message: %s", rec.Body.String())
	}
}

// One test, singular noun.
func TestDeleteProjectRefusalUsesTheSingularForOneTest(t *testing.T) {
	srv := newTestServer()
	p := createProjectViaAPI(t, srv, "Payments")
	body := `{"name":"t","target_url":"http://x","virtual_users":1,"duration_seconds":1,"project_id":"` + p.ID + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tests", strings.NewReader(body))
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	rec := deleteProjectViaAPI(srv, p.ID)
	if !strings.Contains(rec.Body.String(), "still has 1 test;") {
		t.Fatalf("expected the singular noun, got: %s", rec.Body.String())
	}
}
