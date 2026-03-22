package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/repository"
)

type staticClock struct {
	now time.Time
}

func (c staticClock) Now() time.Time { return c.now }

type engineStub struct {
	run func(ctx context.Context, runID domain.RunID) error
}

func (e *engineStub) ExecuteRun(ctx context.Context, runID domain.RunID) error {
	return e.run(ctx, runID)
}

type fakeQueue struct {
	lease    Lease
	hasLease bool
	acked    []string
	released []string
}

func (q *fakeQueue) Enqueue(context.Context, Job) error { return nil }
func (q *fakeQueue) Reserve(context.Context, string, string, time.Time, time.Duration) (Lease, bool, error) {
	if !q.hasLease {
		return Lease{}, false, nil
	}
	lease := q.lease
	q.hasLease = false
	return lease, true, nil
}
func (q *fakeQueue) Ack(_ context.Context, lease Lease) error {
	q.acked = append(q.acked, lease.Job.ID)
	return nil
}
func (q *fakeQueue) Heartbeat(_ context.Context, lease Lease, now time.Time, leaseDuration time.Duration) (Lease, error) {
	lease.LeaseExpiresAt = now.Add(leaseDuration)
	return lease, nil
}
func (q *fakeQueue) Release(_ context.Context, lease Lease, _ time.Time) error {
	q.released = append(q.released, lease.Job.ID)
	return nil
}
func (q *fakeQueue) RequeueExpired(context.Context, string, time.Time, int) (int, error) {
	return 0, nil
}

func TestService_RunOnce_SucceedsAndAcksJob(t *testing.T) {
	ctx := context.Background()
	runRepo := repository.NewInMemoryRunRepository()
	run := domain.FlowRun{ID: "run-success", WorkspaceID: "workspace-1", FlowID: "flow-1", FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: []byte(`{}`), InitiatedBy: "alice", QueueName: "default"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	queue := &fakeQueue{hasLease: true, lease: Lease{Job: Job{ID: string(run.ID), RunID: run.ID, FlowID: run.FlowID, Queue: "default"}, WorkerID: "worker-a"}}
	engine := &engineStub{run: func(_ context.Context, runID domain.RunID) error {
		persisted, err := runRepo.GetByID(ctx, runID)
		if err != nil {
			return err
		}
		finishedAt := time.Unix(5, 0).UTC()
		return runRepo.UpdateStatus(ctx, runID, domain.RunStatusSucceeded, persisted.StartedAt, &finishedAt, "")
	}}
	service, err := NewService(queue, runRepo, engine, zap.NewNop(), staticClock{now: time.Unix(2, 0).UTC()}, Config{
		WorkerID:          "worker-a",
		QueueName:         "default",
		PollInterval:      time.Second,
		LeaseDuration:     30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		StaleRunTimeout:   45 * time.Second,
		RequeueDelay:      time.Second,
		RecoveryBatchSize: 10,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	processed, err := service.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if !processed {
		t.Fatalf("RunOnce() processed = false, want true")
	}
	persisted, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persisted.Status != domain.RunStatusSucceeded {
		t.Fatalf("run status = %q, want %q", persisted.Status, domain.RunStatusSucceeded)
	}
	if len(queue.acked) != 1 || queue.acked[0] != string(run.ID) {
		t.Fatalf("acked = %#v", queue.acked)
	}
}

func TestService_RunOnce_FailedEngineMarksRunFailed(t *testing.T) {
	ctx := context.Background()
	runRepo := repository.NewInMemoryRunRepository()
	run := domain.FlowRun{ID: "run-failed", WorkspaceID: "workspace-2", FlowID: "flow-2", FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: []byte(`{}`), InitiatedBy: "alice", QueueName: "default"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	queue := &fakeQueue{hasLease: true, lease: Lease{Job: Job{ID: string(run.ID), RunID: run.ID, FlowID: run.FlowID, Queue: "default"}, WorkerID: "worker-a"}}
	service, err := NewService(queue, runRepo, &engineStub{run: func(context.Context, domain.RunID) error { return errors.New("boom") }}, zap.NewNop(), staticClock{now: time.Unix(2, 0).UTC()}, Config{
		WorkerID:          "worker-a",
		QueueName:         "default",
		PollInterval:      time.Second,
		LeaseDuration:     30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
		StaleRunTimeout:   45 * time.Second,
		RequeueDelay:      time.Second,
		RecoveryBatchSize: 10,
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	persisted, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persisted.Status != domain.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", persisted.Status, domain.RunStatusFailed)
	}
	if persisted.ErrorMessage == "" {
		t.Fatalf("ErrorMessage = empty")
	}
	if len(queue.acked) != 1 {
		t.Fatalf("acked = %#v", queue.acked)
	}
}

var _ execution.Engine = (*engineStub)(nil)
