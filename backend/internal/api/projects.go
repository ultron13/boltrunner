package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.projectStore.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "failed to list projects", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(projects)
}

type createProjectRequest struct {
	Name string `json:"name"`
}

// projectNameMaxLen bounds a column that is plain TEXT. The switcher menu is a
// fixed-width popover, so an unbounded name is a layout bug waiting to happen.
const projectNameMaxLen = 100

// validProjectName trims and checks a submitted name, writing the error
// response itself. It returns the trimmed name and whether to continue.
// Trimming happens before the uniqueness check so " Payments " cannot become a
// second project the user cannot tell apart from "Payments" in the menu.
func validProjectName(w http.ResponseWriter, raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return "", false
	}
	if len(name) > projectNameMaxLen {
		http.Error(w, "name must be 100 characters or fewer", http.StatusBadRequest)
		return "", false
	}
	return name, true
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name, ok := validProjectName(w, req.Name)
	if !ok {
		return
	}
	p := &model.Project{Name: name}
	err := s.projectStore.CreateProject(r.Context(), p)
	switch {
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "a project with that name already exists", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to create project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name, ok := validProjectName(w, req.Name)
	if !ok {
		return
	}
	p, err := s.projectStore.RenameProject(r.Context(), chi.URLParam(r, "projectID"), name)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "project not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrConflict):
		http.Error(w, "a project with that name already exists", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to rename project", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// handleDeleteProject counts the project's tests before deleting, so a refusal
// can say how many are in the way. The store deliberately does not do this: it
// would need a reference to the test store, and memstore's test store already
// holds one to the project store, which would close the loop into a cycle.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectID")

	// ListProjects doubles as the existence and is_default lookup, and gives us
	// the name for the message -- no extra store method needed for either.
	projects, err := s.projectStore.ListProjects(r.Context())
	if err != nil {
		http.Error(w, "failed to load projects", http.StatusInternalServerError)
		return
	}
	var target *model.Project
	for i := range projects {
		if projects[i].ID == id {
			target = &projects[i]
			break
		}
	}
	if target == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if target.IsDefault {
		http.Error(w, "the default project cannot be deleted", http.StatusConflict)
		return
	}

	tests, err := s.testStore.ListTestsForProject(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to count tests", http.StatusInternalServerError)
		return
	}
	if len(tests) > 0 {
		http.Error(w, notEmptyMessage(target.Name, len(tests)), http.StatusConflict)
		return
	}

	err = s.projectStore.DeleteProject(r.Context(), id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "project not found", http.StatusNotFound)
		return
	case errors.Is(err, store.ErrProtected):
		http.Error(w, "the default project cannot be deleted", http.StatusConflict)
		return
	case errors.Is(err, store.ErrNotEmpty):
		// A test was filed here between the count and the delete. No count in
		// the message: re-reading it now would report a number already stale.
		http.Error(w, target.Name+" still has tests; move or delete them first", http.StatusConflict)
		return
	case err != nil:
		http.Error(w, "failed to delete project", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func notEmptyMessage(name string, n int) string {
	noun := "tests"
	if n == 1 {
		noun = "test"
	}
	return fmt.Sprintf("%s still has %d %s; move or delete them first", name, n, noun)
}
