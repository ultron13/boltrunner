package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

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

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	// Trimmed before the uniqueness check so " Payments " cannot become a
	// second project the user cannot tell apart from "Payments" in the menu.
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if len(name) > projectNameMaxLen {
		http.Error(w, "name must be 100 characters or fewer", http.StatusBadRequest)
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
