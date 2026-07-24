package watcher

import (
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"k8s.io/apimachinery/pkg/runtime"

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

func TestPollOnceMarksRunningWhenJobActive(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunPending}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": run.ID},
		},
		Status: batchv1.JobStatus{Active: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunRunning {
		t.Fatalf("expected running, got %s", got.Status)
	}
}

func TestPollOnceMarksUnschedulableAfterTimeout(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	run := &model.Run{Status: model.RunPending}
	_ = rs.CreateRun(ctx, run)

	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-" + run.ID, Namespace: "boltrunner",
			Labels:            map[string]string{"boltrunner.dev/run-id": run.ID},
			CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Hour)),
		},
		Status: batchv1.JobStatus{}, // no active/succeeded/failed pods: unschedulable
	})

	// A near-zero timeout guarantees the job is treated as stuck.
	w := New(client, rs, "boltrunner", time.Millisecond)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := rs.GetRun(ctx, run.ID)
	if got.Status != model.RunFailed {
		t.Fatalf("expected failed (unschedulable), got %s", got.Status)
	}
	if got.ErrorMessage != "unschedulable" {
		t.Fatalf("expected unschedulable error message, got %q", got.ErrorMessage)
	}
}

func TestPollOnceJobsListError(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	client := k8sfake.NewSimpleClientset()
	client.PrependReactor("list", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("boom")
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err == nil {
		t.Fatal("expected an error when the Jobs list call fails")
	}
}

func TestPollOnceSkipsRunNotInStore(t *testing.T) {
	ctx := context.Background()
	rs := memstore.NewRunStore()
	// No run created in the store; the job references a run-id the store
	// doesn't know about, so GetRun errors and PollOnce should just skip it
	// rather than failing the whole poll.
	client := k8sfake.NewSimpleClientset(&batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: "run-unknown", Namespace: "boltrunner",
			Labels: map[string]string{"boltrunner.dev/run-id": "unknown-run-id"},
		},
		Status: batchv1.JobStatus{Succeeded: 1},
	})

	w := New(client, rs, "boltrunner", 30*time.Second)
	if err := w.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}
}

func TestRunPollsUntilContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	rs := memstore.NewRunStore()
	client := k8sfake.NewSimpleClientset()
	w := New(client, rs, "boltrunner", 30*time.Second)

	done := make(chan struct{})
	go func() {
		w.Run(ctx, 5*time.Millisecond)
		close(done)
	}()

	// Let at least one tick fire (exercising the ticker.C branch) before
	// cancelling (exercising the ctx.Done() branch).
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected Run to return promptly after context cancellation")
	}
}
