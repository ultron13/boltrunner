package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/boltrunner/backend/internal/store/memstore"
)

func TestHealthz(t *testing.T) {
	s := NewServer(memstore.NewTestStore())
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != `{"status":"ok"}` {
		t.Fatalf("unexpected body: %s", body)
	}
}
