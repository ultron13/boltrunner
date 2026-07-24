package api

import (
	"encoding/json"
	"net/http"

	"github.com/boltrunner/backend/internal/model"
)

type createTestRequest struct {
	Name            string `json:"name"`
	TargetURL       string `json:"target_url"`
	VirtualUsers    int    `json:"virtual_users"`
	DurationSeconds int    `json:"duration_seconds"`
}

func (s *Server) handleCreateTest(w http.ResponseWriter, r *http.Request) {
	var req createTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.TargetURL == "" || req.VirtualUsers <= 0 || req.DurationSeconds <= 0 {
		http.Error(w, "name, target_url, virtual_users>0, duration_seconds>0 are required", http.StatusBadRequest)
		return
	}
	t := &model.Test{
		Name:            req.Name,
		TargetURL:       req.TargetURL,
		VirtualUsers:    req.VirtualUsers,
		DurationSeconds: req.DurationSeconds,
	}
	if err := s.testStore.CreateTest(r.Context(), t); err != nil {
		http.Error(w, "failed to create test", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (s *Server) handleListTests(w http.ResponseWriter, r *http.Request) {
	tests, err := s.testStore.ListTests(r.Context())
	if err != nil {
		http.Error(w, "failed to list tests", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tests)
}
