package watcher

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/boltrunner/backend/internal/model"
	"github.com/boltrunner/backend/internal/store/memstore"
)

func TestPollOnceMarksCompleted(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunRunning}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": run.ID},
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunCompleted {
		t.Fatalf("expected completed, got %s", got.Status)
	}
}

func TestPollOnceMarksFailed(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunRunning}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": run.ID},
		},
		Status: batchv1.JobStatus{Failed: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunFailed {
		t.Fatalf("expected failed, got %s", got.Status)
	}
}
