package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/kubernetes"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store"
)

type Server struct {
	router    chi.Router
	testStore store.TestStore
	runStore  store.RunStore
	k8sClient kubernetes.Interface
	jobCfg    k8sjob.Config
}

func NewServer(testStore store.TestStore, runStore store.RunStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *Server {
	s := &Server{
		router:    chi.NewRouter(),
		testStore: testStore,
		runStore:  runStore,
		k8sClient: k8sClient,
		jobCfg:    jobCfg,
	}
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	s.router.Post("/api/tests/{testID}/runs", s.handleStartRun)
	s.router.Post("/api/runs/{runID}/metrics", s.handlePostMetrics)
	s.router.Get("/api/runs/{runID}", s.handleGetRun)
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
