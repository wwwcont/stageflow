package worker

import (
	"context"
	"fmt"

	"stageflow/internal/execution"
	"stageflow/pkg/clock"
)

type Dispatcher struct {
	queue Queue
	clock clock.Clock
}

func NewDispatcher(queue Queue, clk clock.Clock) (*Dispatcher, error) {
	switch {
	case queue == nil:
		return nil, fmt.Errorf("queue is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &Dispatcher{queue: queue, clock: clk}, nil
}

func (d *Dispatcher) Dispatch(ctx context.Context, job execution.RunJob) error {
	return d.queue.Enqueue(ctx, Job{
		ID:             string(job.RunID),
		RunID:          job.RunID,
		WorkspaceID:    job.WorkspaceID,
		TargetType:     job.TargetType,
		FlowID:         job.FlowID,
		SavedRequestID: job.SavedRequestID,
		Queue:          job.Queue,
		EnqueuedAt:     d.clock.Now().UTC(),
	})
}

var _ execution.Dispatcher = (*Dispatcher)(nil)
