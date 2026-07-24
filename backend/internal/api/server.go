package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/boltrunner/backend/internal/store"
)

type Server struct {
	router    chi.Router
	testStore store.TestStore
}

func NewServer(testStore store.TestStore) *Server {
	s := &Server{router: chi.NewRouter(), testStore: testStore}
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
