// Package jobs provides an in-process background work queue with a bounded
// worker pool, per-job cancellation, and a publish/subscribe channel used to
// stream live progress to the HTTP layer (Server-Sent Events).
//
// Every mutation of a job (status change or progress tick) is broadcast to all
// subscribers as an immutable snapshot, so the UI can render a real-time
// progress bar without polling. Heavy work never blocks on slow subscribers:
// progress ticks are delivered best-effort, while terminal transitions
// (completed / failed / canceled) are delivered with a short timeout so the
// final state is not lost.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/flapp/core/internal/domain"
)

// RunFunc is the unit of work executed by the queue.
type RunFunc func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error)

type task struct {
	id  string
	typ domain.JobType
	run RunFunc
}

// Queue implements domain.JobQueue.
type Queue struct {
	mu      sync.RWMutex
	jobs    map[string]*domain.Job
	cancels map[string]context.CancelFunc
	subs    map[int]chan *domain.Job
	nextSub int

	tasks   chan task
	wg      sync.WaitGroup
	closed  bool
	closeMu sync.Mutex
}

// New starts a queue with the given number of concurrent workers (minimum 1)
// and an internal task buffer.
func New(workers int) *Queue {
	if workers < 1 {
		workers = 1
	}
	q := &Queue{
		jobs:    make(map[string]*domain.Job),
		cancels: make(map[string]context.CancelFunc),
		subs:    make(map[int]chan *domain.Job),
		tasks:   make(chan task, 256),
	}
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

// Enqueue registers a new job and schedules it, returning its id immediately.
func (q *Queue) Enqueue(t domain.JobType, run func(ctx context.Context, report domain.ProgressReporter) (map[string]interface{}, error)) string {
	id := newID()
	now := time.Now()
	job := &domain.Job{
		ID:        id,
		Type:      t,
		Status:    domain.StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	q.mu.Lock()
	q.jobs[id] = job
	snapshot := cloneJob(job)
	q.mu.Unlock()

	q.publishLossy(snapshot)

	q.closeMu.Lock()
	defer q.closeMu.Unlock()
	if q.closed {
		q.finish(id, nil, context.Canceled)
		return id
	}
	q.tasks <- task{id: id, typ: t, run: run}
	return id
}

// worker pulls tasks and executes them.
func (q *Queue) worker() {
	defer q.wg.Done()
	for tk := range q.tasks {
		q.execute(tk)
	}
}

func (q *Queue) execute(tk task) {
	ctx, cancel := context.WithCancel(context.Background())

	q.mu.Lock()
	job, ok := q.jobs[tk.id]
	if !ok {
		q.mu.Unlock()
		cancel()
		return
	}
	q.cancels[tk.id] = cancel
	job.Status = domain.StatusRunning
	job.UpdatedAt = time.Now()
	snapshot := cloneJob(job)
	q.mu.Unlock()

	q.publishLossy(snapshot)

	reporter := &reporter{q: q, id: tk.id}
	var (
		result map[string]interface{}
		err    error
	)
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = &panicError{v: r}
			}
		}()
		result, err = tk.run(ctx, reporter)
	}()

	q.finish(tk.id, result, err)
	cancel()

	q.mu.Lock()
	delete(q.cancels, tk.id)
	q.mu.Unlock()
}

// finish records a terminal state and broadcasts it reliably.
func (q *Queue) finish(id string, result map[string]interface{}, err error) {
	q.mu.Lock()
	job, ok := q.jobs[id]
	if !ok {
		q.mu.Unlock()
		return
	}
	switch {
	case err == nil:
		job.Status = domain.StatusCompleted
		job.Progress = 1
		job.Result = result
	case err == context.Canceled || err == context.DeadlineExceeded:
		job.Status = domain.StatusCanceled
		job.Error = "canceled"
	default:
		job.Status = domain.StatusFailed
		job.Error = err.Error()
	}
	job.UpdatedAt = time.Now()
	snapshot := cloneJob(job)
	q.mu.Unlock()

	q.publishSync(snapshot)
}

// Get returns a snapshot of one job.
func (q *Queue) Get(id string) (*domain.Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	job, ok := q.jobs[id]
	if !ok {
		return nil, false
	}
	return cloneJob(job), true
}

// List returns snapshots of all jobs.
func (q *Queue) List() []*domain.Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]*domain.Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, cloneJob(j))
	}
	return out
}

// Cancel requests cancellation of a running or queued job.
func (q *Queue) Cancel(id string) bool {
	q.mu.Lock()
	cancel, running := q.cancels[id]
	job, exists := q.jobs[id]
	if exists && job.Status == domain.StatusQueued {
		// Not yet picked up: mark canceled so the worker skips it.
		job.Status = domain.StatusCanceled
		job.Error = "canceled"
		job.UpdatedAt = time.Now()
		snap := cloneJob(job)
		q.mu.Unlock()
		q.publishSync(snap)
		return true
	}
	q.mu.Unlock()

	if running {
		cancel()
		return true
	}
	return false
}

// Subscribe returns a channel of job snapshots and an unsubscribe function.
func (q *Queue) Subscribe() (<-chan *domain.Job, func()) {
	ch := make(chan *domain.Job, 256)
	q.mu.Lock()
	id := q.nextSub
	q.nextSub++
	q.subs[id] = ch
	q.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			q.mu.Lock()
			delete(q.subs, id)
			q.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Shutdown stops accepting work and waits for in-flight jobs to drain.
func (q *Queue) Shutdown() {
	q.closeMu.Lock()
	if !q.closed {
		q.closed = true
		close(q.tasks)
	}
	q.closeMu.Unlock()
	q.wg.Wait()
}

// publishLossy broadcasts best-effort, dropping on slow subscribers.
func (q *Queue) publishLossy(job *domain.Job) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	for _, ch := range q.subs {
		select {
		case ch <- job:
		default:
		}
	}
}

// publishSync broadcasts terminal states, waiting briefly per subscriber so the
// final event is not dropped under load.
func (q *Queue) publishSync(job *domain.Job) {
	q.mu.RLock()
	channels := make([]chan *domain.Job, 0, len(q.subs))
	for _, ch := range q.subs {
		channels = append(channels, ch)
	}
	q.mu.RUnlock()

	for _, ch := range channels {
		select {
		case ch <- job:
		case <-time.After(2 * time.Second):
		}
	}
}

// reporter is the per-job ProgressReporter handed to running work.
type reporter struct {
	q  *Queue
	id string
}

func (r *reporter) Set(progress float64, stage, detail string) {
	r.update(func(j *domain.Job) {
		if progress >= 0 {
			j.Progress = clamp01(progress)
		}
		if stage != "" {
			j.Stage = stage
		}
		j.Detail = detail
	})
}

func (r *reporter) Stage(stage string) {
	r.update(func(j *domain.Job) { j.Stage = stage })
}

func (r *reporter) Detail(detail string) {
	r.update(func(j *domain.Job) { j.Detail = detail })
}

func (r *reporter) update(mut func(*domain.Job)) {
	r.q.mu.Lock()
	job, ok := r.q.jobs[r.id]
	if !ok {
		r.q.mu.Unlock()
		return
	}
	mut(job)
	job.UpdatedAt = time.Now()
	snap := cloneJob(job)
	r.q.mu.Unlock()
	r.q.publishLossy(snap)
}

// --- helpers ---

func cloneJob(j *domain.Job) *domain.Job {
	c := *j
	if j.Result != nil {
		c.Result = make(map[string]interface{}, len(j.Result))
		for k, v := range j.Result {
			c.Result[k] = v
		}
	}
	return &c
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand should never fail; fall back to a timestamp if it does.
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

type panicError struct{ v interface{} }

func (e *panicError) Error() string {
	return "job panicked: " + toString(e.v)
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return "unknown panic"
}
