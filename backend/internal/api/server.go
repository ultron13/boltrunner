package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/client-go/kubernetes"

	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/store"
)

type Server struct {
	router       chi.Router
	testStore    store.TestStore
	runStore     store.RunStore
	projectStore store.ProjectStore
	k8sClient    kubernetes.Interface
	jobCfg       k8sjob.Config
}

func NewServer(testStore store.TestStore, runStore store.RunStore, projectStore store.ProjectStore, k8sClient kubernetes.Interface, jobCfg k8sjob.Config) *Server {
	s := &Server{
		router:       chi.NewRouter(),
		testStore:    testStore,
		runStore:     runStore,
		projectStore: projectStore,
		k8sClient:    k8sClient,
		jobCfg:       jobCfg,
	}
	s.router.Use(corsMiddleware)
	s.router.Get("/healthz", s.handleHealthz)
	s.router.Get("/api/projects", s.handleListProjects)
	s.router.Post("/api/projects", s.handleCreateProject)
	s.router.Put("/api/projects/{projectID}", s.handleRenameProject)
	s.router.Delete("/api/projects/{projectID}", s.handleDeleteProject)
	s.router.Post("/api/tests", s.handleCreateTest)
	s.router.Get("/api/tests", s.handleListTests)
	s.router.Put("/api/tests/{testID}", s.handleUpdateTest)
	s.router.Put("/api/tests/{testID}/project", s.handleMoveTest)
	s.router.Get("/api/tests/{testID}/versions", s.handleListTestVersions)
	s.router.Post("/api/tests/{testID}/runs", s.handleStartRun)
	s.router.Get("/api/tests/{testID}/runs", s.handleListRunsForTest)
	s.router.Post("/api/runs/{runID}/metrics", s.handlePostMetrics)
	s.router.Get("/api/runs/{runID}", s.handleGetRun)
	s.router.Post("/api/runs/{runID}/cancel", s.handleCancelRun)
	return s
}

func (s *Server) Router() http.Handler {
	return s.router
}

// corsMiddleware allows the frontend (served from a different origin in local
// dev, e.g. localhost:3000 vs the backend's localhost:8080) to call the API.
// No auth exists in this increment, so a permissive policy is acceptable.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
