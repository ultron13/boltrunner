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
