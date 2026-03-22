package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/observability"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type Config struct {
	WorkerID          string
	QueueName         string
	PollInterval      time.Duration
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	StaleRunTimeout   time.Duration
	RequeueDelay      time.Duration
	RecoveryBatchSize int
}

type Service struct {
	queue   Queue
	runs    repository.RunRepository
	engine  execution.Engine
	logger  *zap.Logger
	clock   clock.Clock
	cfg     Config
	metrics *observability.Metrics
}

func NewService(queue Queue, runs repository.RunRepository, engine execution.Engine, logger *zap.Logger, clk clock.Clock, cfg Config) (*Service, error) {
	switch {
	case queue == nil:
		return nil, fmt.Errorf("queue is required")
	case runs == nil:
		return nil, fmt.Errorf("run repository is required")
	case engine == nil:
		return nil, fmt.Errorf("engine is required")
	case logger == nil:
		return nil, fmt.Errorf("logger is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	case cfg.WorkerID == "":
		return nil, fmt.Errorf("worker id is required")
	case cfg.QueueName == "":
		return nil, fmt.Errorf("queue name is required")
	case cfg.PollInterval <= 0:
		return nil, fmt.Errorf("poll interval must be positive")
	case cfg.LeaseDuration <= 0:
		return nil, fmt.Errorf("lease duration must be positive")
	case cfg.HeartbeatInterval <= 0 || cfg.HeartbeatInterval >= cfg.LeaseDuration:
		return nil, fmt.Errorf("heartbeat interval must be positive and less than lease duration")
	case cfg.StaleRunTimeout <= 0:
		return nil, fmt.Errorf("stale run timeout must be positive")
	case cfg.RequeueDelay < 0:
		return nil, fmt.Errorf("requeue delay must be non-negative")
	}
	if cfg.RecoveryBatchSize <= 0 {
		cfg.RecoveryBatchSize = 10
	}
	return &Service{queue: queue, runs: runs, engine: engine, logger: logger, clock: clk, cfg: cfg}, nil
}

func (s *Service) SetMetrics(metrics *observability.Metrics) {
	s.metrics = metrics
}

func (s *Service) Run(ctx context.Context) error {
	for {
		if err := s.recoverExpired(ctx); err != nil {
			s.logger.Error("worker recovery failed", zap.String("queue", s.cfg.QueueName), zap.Error(err))
		}
		processed, err := s.RunOnce(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			s.logger.Error("worker iteration failed", zap.String("queue", s.cfg.QueueName), zap.String("worker_id", s.cfg.WorkerID), zap.Error(err))
			if waitErr := sleepContext(ctx, s.cfg.PollInterval); waitErr != nil {
				return nil
			}
			continue
		}
		if processed {
			continue
		}
		if err := sleepContext(ctx, s.cfg.PollInterval); err != nil {
			return nil
		}
	}
}

func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	now := s.clock.Now().UTC()
	lease, ok, err := s.queue.Reserve(ctx, s.cfg.QueueName, s.cfg.WorkerID, now, s.cfg.LeaseDuration)
	if err != nil || !ok {
		return false, err
	}
	s.recordJob("reserved")
	if err := s.handleLease(ctx, lease); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Service) recoverExpired(ctx context.Context) error {
	requeued, err := s.queue.RequeueExpired(ctx, s.cfg.QueueName, s.clock.Now().UTC(), s.cfg.RecoveryBatchSize)
	if err == nil && requeued > 0 {
		for i := 0; i < requeued; i++ {
			s.recordJob("recovered")
		}
	}
	return err
}

func (s *Service) handleLease(ctx context.Context, lease Lease) error {
	run, err := s.runs.GetByID(ctx, lease.Job.RunID)
	if err != nil {
		var notFoundErr *domain.NotFoundError
		if errors.As(err, &notFoundErr) {
			s.recordJob("acked")
			return s.queue.Ack(ctx, lease)
		}
		s.recordJob("released")
		_ = s.queue.Release(ctx, lease, s.clock.Now().UTC().Add(s.cfg.RequeueDelay))
		return fmt.Errorf("get run %q: %w", lease.Job.RunID, err)
	}
	if run.IsTerminal() {
		s.recordJob("acked")
		return s.queue.Ack(ctx, lease)
	}
	claimedRun, err := s.runs.ClaimForExecution(ctx, lease.Job.RunID, s.cfg.WorkerID, s.clock.Now().UTC(), s.clock.Now().UTC().Add(-s.cfg.StaleRunTimeout))
	if err != nil {
		var conflictErr *domain.ConflictError
		if errors.As(err, &conflictErr) {
			fields := append(observability.RunContextLogFields(observability.RunContext{
				WorkspaceID:    lease.Job.WorkspaceID,
				RunID:          lease.Job.RunID,
				FlowID:         lease.Job.FlowID,
				SavedRequestID: lease.Job.SavedRequestID,
				TargetType:     lease.Job.TargetType,
			}), zap.String("worker_id", s.cfg.WorkerID), zap.String("queue", s.cfg.QueueName), zap.Error(err))
			s.logger.Warn("run is already owned by another worker", fields...)
			s.recordJob("released")
			return s.queue.Release(ctx, lease, s.clock.Now().UTC().Add(s.cfg.RequeueDelay))
		}
		s.recordJob("released")
		_ = s.queue.Release(ctx, lease, s.clock.Now().UTC().Add(s.cfg.RequeueDelay))
		return fmt.Errorf("claim run %q: %w", lease.Job.RunID, err)
	}
	if claimedRun.IsTerminal() {
		s.recordJob("acked")
		return s.queue.Ack(ctx, lease)
	}

	heartbeatDone := make(chan struct{})
	stopHeartbeat := make(chan struct{})
	go s.heartbeatLoop(ctx, lease, stopHeartbeat, heartbeatDone)

	execErr := s.engine.ExecuteRun(ctx, lease.Job.RunID)
	close(stopHeartbeat)
	<-heartbeatDone
	if execErr != nil {
		s.recordJob("failed")
		fields := append(observability.RunContextLogFields(observability.RunContext{
			WorkspaceID:    lease.Job.WorkspaceID,
			RunID:          lease.Job.RunID,
			FlowID:         lease.Job.FlowID,
			SavedRequestID: lease.Job.SavedRequestID,
			TargetType:     lease.Job.TargetType,
		}), zap.String("worker_id", s.cfg.WorkerID), zap.Error(execErr))
		s.logger.Error("run execution failed", fields...)
		if markErr := s.failRunIfStillRunning(ctx, lease.Job.RunID, execErr); markErr != nil {
			s.logger.Error("mark run failed after engine error", zap.String("run_id", string(lease.Job.RunID)), zap.Error(markErr))
		}
	} else {
		s.recordJob("completed")
	}
	if err := s.queue.Ack(ctx, lease); err != nil {
		return fmt.Errorf("ack job %q: %w", lease.Job.ID, err)
	}
	s.recordJob("acked")
	return nil
}

func (s *Service) heartbeatLoop(ctx context.Context, lease Lease, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(s.cfg.HeartbeatInterval)
	defer ticker.Stop()
	currentLease := lease
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			now := s.clock.Now().UTC()
			updatedLease, err := s.queue.Heartbeat(ctx, currentLease, now, s.cfg.LeaseDuration)
			if err != nil {
				s.recordJob("heartbeat_error")
				fields := append(observability.RunContextLogFields(observability.RunContext{
					WorkspaceID:    currentLease.Job.WorkspaceID,
					RunID:          currentLease.Job.RunID,
					FlowID:         currentLease.Job.FlowID,
					SavedRequestID: currentLease.Job.SavedRequestID,
					TargetType:     currentLease.Job.TargetType,
				}), zap.String("job_id", currentLease.Job.ID), zap.String("worker_id", s.cfg.WorkerID), zap.Error(err))
				s.logger.Error("queue heartbeat failed", fields...)
			} else {
				currentLease = updatedLease
			}
			if err := s.runs.Heartbeat(ctx, currentLease.Job.RunID, s.cfg.WorkerID, now); err != nil {
				s.recordJob("heartbeat_error")
				fields := append(observability.RunContextLogFields(observability.RunContext{
					WorkspaceID:    currentLease.Job.WorkspaceID,
					RunID:          currentLease.Job.RunID,
					FlowID:         currentLease.Job.FlowID,
					SavedRequestID: currentLease.Job.SavedRequestID,
					TargetType:     currentLease.Job.TargetType,
				}), zap.String("worker_id", s.cfg.WorkerID), zap.Error(err))
				s.logger.Error("run heartbeat failed", fields...)
			}
		}
	}
}

func (s *Service) failRunIfStillRunning(ctx context.Context, runID domain.RunID, cause error) error {
	run, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != domain.RunStatusRunning {
		return nil
	}
	finishedAt := s.clock.Now().UTC()
	message := fmt.Sprintf("worker execution failed: %s", cause.Error())
	return s.runs.UpdateStatus(ctx, runID, domain.RunStatusFailed, run.StartedAt, &finishedAt, message)
}

func (s *Service) recordJob(event string) {
	if s.metrics != nil {
		s.metrics.RecordWorkerJob(s.cfg.QueueName, event)
	}
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
