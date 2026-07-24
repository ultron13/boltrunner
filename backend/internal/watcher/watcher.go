package watcher

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store"
)

type Watcher struct {
	k8sClient            kubernetes.Interface
	runStore             store.RunStore
	namespace            string
	unschedulableTimeout time.Duration
}

func New(k8sClient kubernetes.Interface, runStore store.RunStore, namespace string, unschedulableTimeout time.Duration) *Watcher {
	return &Watcher{k8sClient: k8sClient, runStore: runStore, namespace: namespace, unschedulableTimeout: unschedulableTimeout}
}

// PollOnce reconciles every Job labeled with a run-id against the run's stored status.
// Completion/failure is always derived from the Job's real status, never from the
// sidecar's last metrics POST alone (see spec's error-handling section).
func (w *Watcher) PollOnce(ctx context.Context) error {
	jobs, err := w.k8sClient.BatchV1().Jobs(w.namespace).List(ctx, metav1.ListOptions{LabelSelector: "boltrunner.dev/run-id"})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		runID := job.Labels["boltrunner.dev/run-id"]
		run, err := w.runStore.GetRun(ctx, runID)
		if err != nil {
			continue
		}
		switch {
		case job.Status.Succeeded > 0 && run.Status != model.RunCompleted:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunCompleted, "")
		case job.Status.Failed > 0 && run.Status != model.RunFailed:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunFailed, "job failed")
		case job.Status.Active > 0 && run.Status == model.RunPending:
			w.runStore.UpdateRunStatus(ctx, runID, model.RunRunning, "")
		case run.Status == model.RunPending && job.Status.Active == 0 && job.Status.Succeeded == 0 && job.Status.Failed == 0:
			if run.StartedAt == nil && time.Since(job.CreationTimestamp.Time) > w.unschedulableTimeout {
				w.runStore.UpdateRunStatus(ctx, runID, model.RunFailed, "unschedulable")
			}
		}
	}
	return nil
}

// Run polls on the given interval until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = w.PollOnce(ctx)
		}
	}
}
