package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/flapp/core/internal/domain"
)

func waitFor(t *testing.T, q *Queue, id string, status domain.JobStatus, timeout time.Duration) *domain.Job {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if j, ok := q.Get(id); ok && j.Status == status {
			return j
		}
		time.Sleep(2 * time.Millisecond)
	}
	if j, ok := q.Get(id); ok {
		t.Fatalf("job %s did not reach %s in time (last status %s)", id, status, j.Status)
	}
	t.Fatalf("job %s vanished", id)
	return nil
}

func TestJobCompletesWithResult(t *testing.T) {
	q := New(2)
	defer q.Shutdown()

	id := q.Enqueue(domain.JobHarvest, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
		r.Set(0.5, "working", "half")
		return map[string]interface{}{"count": 42}, nil
	})

	job := waitFor(t, q, id, domain.StatusCompleted, time.Second)
	if job.Progress != 1 {
		t.Errorf("progress = %v, want 1 on completion", job.Progress)
	}
	if job.Result["count"] != 42 {
		t.Errorf("result = %v", job.Result)
	}
}

func TestJobFailureCapturesError(t *testing.T) {
	q := New(1)
	defer q.Shutdown()

	id := q.Enqueue(domain.JobRename, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
		return nil, errors.New("disk on fire")
	})
	job := waitFor(t, q, id, domain.StatusFailed, time.Second)
	if job.Error != "disk on fire" {
		t.Errorf("error = %q", job.Error)
	}
}

func TestJobPanicBecomesFailure(t *testing.T) {
	q := New(1)
	defer q.Shutdown()

	id := q.Enqueue(domain.JobReanalyze, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
		panic("boom")
	})
	job := waitFor(t, q, id, domain.StatusFailed, time.Second)
	if job.Error == "" {
		t.Error("expected non-empty error after panic")
	}
}

func TestJobCancellation(t *testing.T) {
	q := New(1)
	defer q.Shutdown()

	started := make(chan struct{})
	id := q.Enqueue(domain.JobHarvest, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
		close(started)
		<-ctx.Done() // block until canceled
		return nil, ctx.Err()
	})
	<-started
	if !q.Cancel(id) {
		t.Fatal("Cancel returned false for running job")
	}
	job := waitFor(t, q, id, domain.StatusCanceled, time.Second)
	if job.Status != domain.StatusCanceled {
		t.Errorf("status = %s", job.Status)
	}
}

func TestSubscribeReceivesProgressAndTerminal(t *testing.T) {
	q := New(1)
	defer q.Shutdown()

	ch, cancel := q.Subscribe()
	defer cancel()

	id := q.Enqueue(domain.JobHarvest, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
		r.Stage("scanning")
		r.Set(0.25, "scanning", "file1")
		r.Set(0.75, "hashing", "file2")
		return map[string]interface{}{"ok": true}, nil
	})

	var (
		sawRunning   bool
		sawCompleted bool
		mu           sync.Mutex
	)
	done := make(chan struct{})
	go func() {
		for j := range ch {
			if j.ID != id {
				continue
			}
			mu.Lock()
			if j.Status == domain.StatusRunning {
				sawRunning = true
			}
			if j.Status == domain.StatusCompleted {
				sawCompleted = true
				mu.Unlock()
				close(done)
				return
			}
			mu.Unlock()
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive completed event")
	}
	mu.Lock()
	defer mu.Unlock()
	if !sawRunning {
		t.Error("never saw running status")
	}
	if !sawCompleted {
		t.Error("never saw completed status")
	}
}

func TestConcurrentJobs(t *testing.T) {
	q := New(4)
	defer q.Shutdown()

	const n = 20
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = q.Enqueue(domain.JobHarvest, func(ctx context.Context, r domain.ProgressReporter) (map[string]interface{}, error) {
			time.Sleep(5 * time.Millisecond)
			return nil, nil
		})
	}
	for _, id := range ids {
		waitFor(t, q, id, domain.StatusCompleted, 3*time.Second)
	}
	if len(q.List()) != n {
		t.Errorf("List = %d jobs, want %d", len(q.List()), n)
	}
}
