package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/boltrunner/backend/internal/jmx"
	"github.com/boltrunner/backend/internal/k8sjob"
	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

func (s *Server) handleStartRun(w http.ResponseWriter, r *http.Request) {
	testID := chi.URLParam(r, "testID")
	test, err := s.testStore.GetTest(r.Context(), testID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "test not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load test", http.StatusInternalServerError)
		return
	}

	run := &model.Run{TestID: test.ID, Status: model.RunPending}
	if err := s.runStore.CreateRun(r.Context(), run); err != nil {
		http.Error(w, "failed to create run", http.StatusInternalServerError)
		return
	}

	plan, err := jmx.Generate(jmx.Params{TargetURL: test.TargetURL, VirtualUsers: test.VirtualUsers, DurationSeconds: test.DurationSeconds})
	if err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to generate test plan: "+err.Error())
		http.Error(w, "failed to generate test plan", http.StatusInternalServerError)
		return
	}

	cm, job := k8sjob.Build(s.jobCfg, run.ID, plan)
	if _, err := s.k8sClient.CoreV1().ConfigMaps(s.jobCfg.Namespace).Create(r.Context(), cm, metav1.CreateOptions{}); err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to create configmap: "+err.Error())
		http.Error(w, "failed to submit run", http.StatusInternalServerError)
		return
	}
	if _, err := s.k8sClient.BatchV1().Jobs(s.jobCfg.Namespace).Create(r.Context(), job, metav1.CreateOptions{}); err != nil {
		s.runStore.UpdateRunStatus(r.Context(), run.ID, model.RunFailed, "failed to create job: "+err.Error())
		http.Error(w, "failed to submit run", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(run)
}

type postMetricsRequest struct {
	ElapsedSeconds    int     `json:"elapsed_seconds"`
	ThroughputRPS     float64 `json:"throughput_rps"`
	AvgResponseTimeMs float64 `json:"avg_response_time_ms"`
	ErrorRatePct      float64 `json:"error_rate_pct"`
	SampleCount       int     `json:"sample_count"`
}

func (s *Server) handlePostMetrics(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	var req postMetricsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	snap := &model.RunMetricSnapshot{
		RunID: runID, ElapsedSeconds: req.ElapsedSeconds, ThroughputRPS: req.ThroughputRPS,
		AvgResponseTimeMs: req.AvgResponseTimeMs, ErrorRatePct: req.ErrorRatePct, SampleCount: req.SampleCount,
	}
	if err := s.runStore.AppendMetricSnapshot(r.Context(), snap); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to store snapshot", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

type getRunResponse struct {
	Run     model.Run                 `json:"run"`
	Latest  *model.RunMetricSnapshot  `json:"latest,omitempty"`
	History []model.RunMetricSnapshot `json:"history"`
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	run, err := s.runStore.GetRun(r.Context(), runID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "run not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "failed to load run", http.StatusInternalServerError)
		return
	}
	history, err := s.runStore.ListSnapshots(r.Context(), runID)
	if err != nil {
		http.Error(w, "failed to load history", http.StatusInternalServerError)
		return
	}
	resp := getRunResponse{Run: *run, History: history}
	if latest, err := s.runStore.LatestSnapshot(r.Context(), runID); err == nil {
		resp.Latest = latest
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
