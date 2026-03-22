package execution

import (
	"context"

	"go.uber.org/zap"

	"stageflow/internal/observability"
)

// LoggingDispatcher is a safe bootstrap implementation used until a real queue-backed dispatcher is introduced.
type LoggingDispatcher struct {
	logger *zap.Logger
}

func NewLoggingDispatcher(logger *zap.Logger) *LoggingDispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &LoggingDispatcher{logger: logger}
}

func (d *LoggingDispatcher) Dispatch(_ context.Context, job RunJob) error {
	fields := append(observability.RunContextLogFields(observability.RunContext{
		WorkspaceID:    job.WorkspaceID,
		RunID:          job.RunID,
		FlowID:         job.FlowID,
		SavedRequestID: job.SavedRequestID,
		TargetType:     job.TargetType,
	}), zap.String("queue", job.Queue))
	d.logger.Info("run dispatched", fields...)
	return nil
}
