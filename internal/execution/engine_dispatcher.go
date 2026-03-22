package execution

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"stageflow/internal/observability"
)

// EngineDispatcher связывает асинхронные запросы на запуск с движком выполнения.
type EngineDispatcher struct {
	engine Engine
	logger *zap.Logger
}

func NewEngineDispatcher(engine Engine, logger *zap.Logger) (*EngineDispatcher, error) {
	switch {
	case engine == nil:
		return nil, fmt.Errorf("engine is required")
	case logger == nil:
		return nil, fmt.Errorf("logger is required")
	}
	return &EngineDispatcher{engine: engine, logger: logger}, nil
}

func (d *EngineDispatcher) Dispatch(_ context.Context, job RunJob) error {
	fields := append(observability.RunContextLogFields(observability.RunContext{
		WorkspaceID:    job.WorkspaceID,
		RunID:          job.RunID,
		FlowID:         job.FlowID,
		SavedRequestID: job.SavedRequestID,
		TargetType:     job.TargetType,
	}), zap.String("queue", job.Queue))
	d.logger.Info("run dispatched", fields...)
	go func() {
		runCtx := context.Background()
		if err := d.engine.ExecuteRun(runCtx, job.RunID); err != nil {
			errorFields := append(fields, zap.Error(err))
			d.logger.Error("run execution failed", errorFields...)
			return
		}
		d.logger.Info("run execution finished", fields...)
	}()
	return nil
}

var _ Dispatcher = (*EngineDispatcher)(nil)
