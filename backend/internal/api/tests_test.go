package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/boltrunner/backend/internal/model"
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
