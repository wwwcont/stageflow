package usecase

import (
	"context"
	"fmt"

	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/repository"
	"stageflow/internal/runevents"
	"stageflow/pkg/clock"
)

type ListRunEventsQuery struct {
	WorkspaceID   domain.WorkspaceID
	RunID         domain.RunID
	AfterSequence int64
}

type RunEventUseCase interface {
	ListRunEvents(ctx context.Context, query ListRunEventsQuery) ([]domain.RunEvent, error)
	SubscribeRunEvents(ctx context.Context, query ListRunEventsQuery) (<-chan domain.RunEvent, func(), error)
}

type RunEventService struct {
	runs   repository.RunRepository
	events repository.RunEventRepository
	broker *runevents.Broker
	clock  clock.Clock
}

func NewRunEventService(runs repository.RunRepository, events repository.RunEventRepository, broker *runevents.Broker, clk clock.Clock) (*RunEventService, error) {
	switch {
	case runs == nil:
		return nil, fmt.Errorf("run repository is required")
	case events == nil:
		return nil, fmt.Errorf("run event repository is required")
	case broker == nil:
		return nil, fmt.Errorf("run event broker is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &RunEventService{runs: runs, events: events, broker: broker, clock: clk}, nil
}

func (s *RunEventService) PublishRunEvent(ctx context.Context, event domain.RunEvent) error {
	run, err := s.runs.GetByID(ctx, event.RunID)
	if err != nil {
		return fmt.Errorf("get run %q for event publish: %w", event.RunID, err)
	}
	event.WorkspaceID = run.WorkspaceID
	event.FlowID = run.FlowID
	event.SavedRequestID = run.SavedRequestID
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.clock.Now().UTC()
	}
	persisted, err := s.events.Append(ctx, event)
	if err != nil {
		return fmt.Errorf("append run event: %w", err)
	}
	s.broker.Publish(persisted)
	return nil
}

func (s *RunEventService) ListRunEvents(ctx context.Context, query ListRunEventsQuery) ([]domain.RunEvent, error) {
	if _, err := s.ensureRunScope(ctx, query.WorkspaceID, query.RunID); err != nil {
		return nil, err
	}
	items, err := s.events.ListByRunID(ctx, query.RunID, query.AfterSequence)
	if err != nil {
		return nil, fmt.Errorf("list run events: %w", err)
	}
	return items, nil
}

func (s *RunEventService) SubscribeRunEvents(ctx context.Context, query ListRunEventsQuery) (<-chan domain.RunEvent, func(), error) {
	if _, err := s.ensureRunScope(ctx, query.WorkspaceID, query.RunID); err != nil {
		return nil, nil, err
	}
	ch, cancel := s.broker.Subscribe(query.RunID, 64)
	return ch, cancel, nil
}

func (s *RunEventService) ensureRunScope(ctx context.Context, workspaceID domain.WorkspaceID, runID domain.RunID) (domain.FlowRun, error) {
	run, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get run %q: %w", runID, err)
	}
	if workspaceID != "" && run.WorkspaceID != workspaceID {
		return domain.FlowRun{}, &domain.NotFoundError{Entity: "run", ID: string(runID)}
	}
	return run, nil
}

var _ RunEventUseCase = (*RunEventService)(nil)
var _ execution.RunEventPublisher = (*RunEventService)(nil)
