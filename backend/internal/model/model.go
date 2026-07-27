package model

import "time"

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunStopped   RunStatus = "stopped"
)

type Test struct {
	ID              string    `json:"id"`         // catalog id: stable across versions
	VersionID       string    `json:"version_id"` // primary key of this version row
	Version         int       `json:"version"`
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	TargetURL       string    `json:"target_url"`
	VirtualUsers    int       `json:"virtual_users"`
	DurationSeconds int       `json:"duration_seconds"`
	CreatedAt       time.Time `json:"created_at"` // when the test was first created
	UpdatedAt       time.Time `json:"updated_at"` // when this version was cut
}

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Run struct {
	ID            string     `json:"id"`
	TestID        string     `json:"test_id"`
	TestCatalogID string     `json:"test_catalog_id"`
	Status        RunStatus  `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
}

type RunMetricSnapshot struct {
	ID                string    `json:"id"`
	RunID             string    `json:"run_id"`
	Ts                time.Time `json:"ts"`
	ElapsedSeconds    int       `json:"elapsed_seconds"`
	ThroughputRPS     float64   `json:"throughput_rps"`
	AvgResponseTimeMs float64   `json:"avg_response_time_ms"`
	ErrorRatePct      float64   `json:"error_rate_pct"`
	SampleCount       int       `json:"sample_count"`
}
