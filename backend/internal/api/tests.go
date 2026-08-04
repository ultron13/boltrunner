package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

// testRequest is shared by create and update. ProjectID is only honoured on
// create -- moving a test between projects belongs to the project registry
// work (BOL-49), so an update inherits the family's existing project.
type testRequest struct {
	Name            string `json:"name"`
	TargetURL       string `json:"target_url"`
	VirtualUsers    int    `json:"virtual_users"`
	DurationSeconds int    `json:"duration_seconds"`
	ProjectID       string `json:"project_id"`
}

func (req testRequest) valid() bool {
	return req.Name != "" && req.TargetURL != "" && req.VirtualUsers > 0 && req.DurationSeconds > 0
}

const testValidationMessage = "name, target_url, virtual_users>0, duration_seconds>0 are required"

func (s *Server) handleCreateTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !req.valid() {
		http.Error(w, testValidationMessage, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ProjectID:       req.ProjectID,
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: req.DurationSeconds,
	}
	err := s.testStore.CreateTest(r.Context(), t)
	switch {
	case errors.Is(err, store.ErrInvalidReference):
		http.Error(w, "unknown project_id", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "failed to create test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (s *Server) handleListTests(w http.ResponseWriter, r *http.Request) {
	var (
		tests []model.Test
		err   error
	)
	// An absent project_id means "every project": existing callers, the Go
	// integration test and three e2e specs all depend on that.
	if projectID := r.URL.Query().Get("project_id"); projectID != "" {
		tests, err = s.testStore.ListTestsForProject(r.Context(), projectID)
	} else {
		tests, err = s.testStore.ListTests(r.Context())
	}
	if err != nil {
		http.Error(w, "failed to list tests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tests)
}

// handleUpdateTest records an edit as a new immutable version rather than
// mutating the current one, so runs of earlier versions keep their exact
// configuration.
func (s *Server) handleUpdateTest(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if !req.valid() {
		http.Error(w, testValidationMessage, http.StatusBadRequest)
		return
	}
	t := &model.Test{
		ID:              chi.URLParam(r, "testID"),
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: req.DurationSeconds,
	}
	err := s.testStore.UpdateTest(r.Context(), t)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "test not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		// A concurrent edit already claimed the next version number. The
		// request was valid; the client may retry against the new latest.
		http.Error(w, "test was modified concurrently; reload and retry", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to update test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func (s *Server) handleListTestVersions(w http.ResponseWriter, r *http.Request) {
	testID := chi.URLParam(r, "testID")
	if _, err := s.testStore.GetTest(r.Context(), testID); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "test not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}
	versions, err := s.testStore.ListTestVersions(r.Context(), testID)
	if err != nil {
		http.Error(w, "failed to list versions", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(versions)
}

type moveTestRequest struct {
	ProjectID string `json:"project_id"`
}

// handleMoveTest refiles a whole test family. It is a separate route from
// handleUpdateTest because an edit cuts a new version and a move does not --
// sharing one request would make it mean two different things.
func (s *Server) handleMoveTest(w http.ResponseWriter, r *http.Request) {
	var req moveTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.ProjectID == "" {
		http.Error(w, "project_id is required", http.StatusBadRequest)
		return
	}
	testID := chi.URLParam(r, "testID")
	err := s.testStore.MoveTest(r.Context(), testID, req.ProjectID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "test not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrInvalidReference):
		http.Error(w, "unknown project_id", http.StatusBadRequest)
		return
	case err != nil:
		http.Error(w, "failed to move test", http.StatusInternalServerError)
		return
	}
	moved, err := s.testStore.GetTest(r.Context(), testID)
	if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(moved)
}
