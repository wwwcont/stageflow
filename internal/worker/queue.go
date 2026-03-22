package worker

import (
	"context"
	"time"

	"stageflow/internal/domain"
)

type Job struct {
	ID             string                `json:"id"`
	RunID          domain.RunID          `json:"run_id"`
	WorkspaceID    domain.WorkspaceID    `json:"workspace_id,omitempty"`
	TargetType     domain.RunTargetType  `json:"target_type,omitempty"`
	FlowID         domain.FlowID         `json:"flow_id,omitempty"`
	SavedRequestID domain.SavedRequestID `json:"saved_request_id,omitempty"`
	Queue          string                `json:"queue"`
	EnqueuedAt     time.Time             `json:"enqueued_at"`
}

type Lease struct {
	Job            Job
	WorkerID       string
	LeaseExpiresAt time.Time
}

type Queue interface {
	Enqueue(ctx context.Context, job Job) error
	Reserve(ctx context.Context, queueName, workerID string, now time.Time, leaseDuration time.Duration) (Lease, bool, error)
	Ack(ctx context.Context, lease Lease) error
	Heartbeat(ctx context.Context, lease Lease, now time.Time, leaseDuration time.Duration) (Lease, error)
	Release(ctx context.Context, lease Lease, availableAt time.Time) error
	RequeueExpired(ctx context.Context, queueName string, now time.Time, limit int) (int, error)
}
