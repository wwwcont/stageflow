package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/repository"
)

type RunCoordinator struct {
	workspaces    repository.WorkspaceRepository
	savedRequests repository.SavedRequestRepository
	flows         repository.FlowRepository
	flowSteps     repository.FlowStepRepository
	runs          repository.RunRepository
	runSteps      repository.RunStepRepository
	dispatcher    execution.Dispatcher
	idFactory     IDFactory
	defaultQ      string
}

type IDFactory interface {
	NewRunID() domain.RunID
}

func NewRunCoordinator(workspaces repository.WorkspaceRepository, savedRequests repository.SavedRequestRepository, flows repository.FlowRepository, flowSteps repository.FlowStepRepository, runs repository.RunRepository, runSteps repository.RunStepRepository, dispatcher execution.Dispatcher, idFactory IDFactory, defaultQueue string) (*RunCoordinator, error) {
	switch {
	case workspaces == nil:
		return nil, fmt.Errorf("workspace repository is required")
	case savedRequests == nil:
		return nil, fmt.Errorf("saved request repository is required")
	case flows == nil:
		return nil, fmt.Errorf("flow repository is required")
	case flowSteps == nil:
		return nil, fmt.Errorf("flow step repository is required")
	case runs == nil:
		return nil, fmt.Errorf("run repository is required")
	case runSteps == nil:
		return nil, fmt.Errorf("run step repository is required")
	case dispatcher == nil:
		return nil, fmt.Errorf("dispatcher is required")
	case idFactory == nil:
		return nil, fmt.Errorf("id factory is required")
	case defaultQueue == "":
		return nil, fmt.Errorf("default queue is required")
	}

	return &RunCoordinator{workspaces: workspaces, savedRequests: savedRequests, flows: flows, flowSteps: flowSteps, runs: runs, runSteps: runSteps, dispatcher: dispatcher, idFactory: idFactory, defaultQ: defaultQueue}, nil
}

func (s *RunCoordinator) LaunchFlow(ctx context.Context, input LaunchFlowInput) (domain.FlowRun, error) {
	workspace, err := s.workspaces.GetByID(ctx, input.WorkspaceID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get workspace %q: %w", input.WorkspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	flow, err := s.flows.GetByID(ctx, input.FlowID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get flow %q: %w", input.FlowID, err)
	}
	if flow.WorkspaceID != workspace.ID {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "flow", Field: "workspace_id", Value: string(flow.WorkspaceID)}
	}
	if flow.Status != domain.FlowStatusActive {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "flow", Field: "status", Value: string(flow.Status)}
	}
	if err := s.validateLaunchableFlow(ctx, workspace, flow); err != nil {
		return domain.FlowRun{}, err
	}
	return s.enqueueFlowRun(ctx, workspace, flow.ID, flow.Version, input.InitiatedBy, input.InputJSON, input.Queue, input.IdempotencyKey)
}

func (s *RunCoordinator) enqueueFlowRun(ctx context.Context, workspace domain.Workspace, flowID domain.FlowID, flowVersion int, initiatedBy string, payload json.RawMessage, queue string, idempotencyKey string) (domain.FlowRun, error) {
	if err := domain.ValidateJSONPayload(payload, "launch input_json"); err != nil {
		return domain.FlowRun{}, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.runs.FindByIdempotencyKey(ctx, workspace.ID, flowID, idempotencyKey)
		if err == nil {
			return existing, nil
		}
		var notFoundErr *domain.NotFoundError
		if err != nil && !errors.As(err, &notFoundErr) {
			return domain.FlowRun{}, fmt.Errorf("lookup run by idempotency key %q: %w", idempotencyKey, err)
		}
	}
	if queue == "" {
		queue = s.defaultQ
	}

	run := domain.FlowRun{
		ID:             s.idFactory.NewRunID(),
		WorkspaceID:    workspace.ID,
		TargetType:     domain.RunTargetTypeFlow,
		FlowID:         flowID,
		FlowVersion:    flowVersion,
		Status:         domain.RunStatusQueued,
		InputJSON:      cloneJSON(payload),
		InitiatedBy:    initiatedBy,
		QueueName:      queue,
		IdempotencyKey: idempotencyKey,
	}
	if err := run.Validate(); err != nil {
		return domain.FlowRun{}, err
	}
	if err := s.runs.Create(ctx, run); err != nil {
		return domain.FlowRun{}, fmt.Errorf("create run: %w", err)
	}
	if err := s.dispatcher.Dispatch(ctx, execution.RunJob{
		RunID:       run.ID,
		WorkspaceID: run.WorkspaceID,
		TargetType:  run.Target(),
		FlowID:      run.FlowID,
		Queue:       queue,
	}); err != nil {
		_ = s.runs.UpdateStatus(ctx, run.ID, domain.RunStatusFailed, nil, nil, "dispatch run")
		return domain.FlowRun{}, fmt.Errorf("dispatch run: %w", err)
	}
	return run, nil
}

func (s *RunCoordinator) LaunchSavedRequest(ctx context.Context, input LaunchSavedRequestInput) (domain.FlowRun, error) {
	workspace, err := s.workspaces.GetByID(ctx, input.WorkspaceID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get workspace %q: %w", input.WorkspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	request, err := s.savedRequests.GetByID(ctx, input.SavedRequestID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get saved request %q: %w", input.SavedRequestID, err)
	}
	if request.WorkspaceID != workspace.ID {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "saved request", Field: "workspace_id", Value: string(request.WorkspaceID)}
	}
	if err := s.validateLaunchableSavedRequest(workspace, request); err != nil {
		return domain.FlowRun{}, err
	}
	if err := domain.ValidateJSONPayload(input.InputJSON, "launch input_json"); err != nil {
		return domain.FlowRun{}, err
	}
	queue := strings.TrimSpace(input.Queue)
	if queue == "" {
		queue = s.defaultQ
	}
	run := domain.FlowRun{
		ID:             s.idFactory.NewRunID(),
		WorkspaceID:    workspace.ID,
		TargetType:     domain.RunTargetTypeSavedRequest,
		SavedRequestID: request.ID,
		Status:         domain.RunStatusQueued,
		InputJSON:      cloneJSON(input.InputJSON),
		InitiatedBy:    input.InitiatedBy,
		QueueName:      queue,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
	}
	if err := run.Validate(); err != nil {
		return domain.FlowRun{}, err
	}
	if err := s.runs.Create(ctx, run); err != nil {
		return domain.FlowRun{}, fmt.Errorf("create saved request run: %w", err)
	}
	if err := s.dispatcher.Dispatch(ctx, execution.RunJob{
		RunID:          run.ID,
		WorkspaceID:    run.WorkspaceID,
		TargetType:     run.Target(),
		SavedRequestID: run.SavedRequestID,
		Queue:          queue,
	}); err != nil {
		_ = s.runs.UpdateStatus(ctx, run.ID, domain.RunStatusFailed, nil, nil, "dispatch saved request run")
		return domain.FlowRun{}, fmt.Errorf("dispatch saved request run: %w", err)
	}
	return run, nil
}

func (s *RunCoordinator) RunSavedRequest(ctx context.Context, input LaunchSavedRequestInput) (domain.FlowRun, error) {
	return s.LaunchSavedRequest(ctx, input)
}

func (s *RunCoordinator) ListRuns(ctx context.Context, query ListRunsQuery) ([]domain.FlowRun, error) {
	runs, err := s.runs.List(ctx, repository.RunListFilter{
		WorkspaceID: query.WorkspaceID,
		Statuses:    query.Statuses,
		Limit:       query.Limit,
		Offset:      query.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}
func (s *RunCoordinator) GetRunStatus(ctx context.Context, query GetRunStatusQuery) (RunStatusView, error) {
	run, err := s.runs.GetByID(ctx, query.RunID)
	if err != nil {
		return RunStatusView{}, err
	}
	if query.WorkspaceID != "" && run.WorkspaceID != query.WorkspaceID {
		return RunStatusView{}, &domain.NotFoundError{Entity: "flow run", ID: string(query.RunID)}
	}
	steps, err := s.runSteps.ListByRunID(ctx, query.RunID)
	if err != nil {
		return RunStatusView{}, err
	}
	return RunStatusView{Run: run, Steps: steps}, nil
}

func (s *RunCoordinator) Rerun(ctx context.Context, input RerunInput) (domain.FlowRun, error) {
	previous, err := s.runs.GetByID(ctx, input.RunID)
	if err != nil {
		return domain.FlowRun{}, err
	}
	if input.WorkspaceID != "" && input.WorkspaceID != previous.WorkspaceID {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "flow run", Field: "workspace_id", Value: string(previous.WorkspaceID)}
	}
	workspace, err := s.workspaces.GetByID(ctx, previous.WorkspaceID)
	if err != nil {
		return domain.FlowRun{}, fmt.Errorf("get workspace %q: %w", previous.WorkspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.FlowRun{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	payload := cloneJSON(previous.InputJSON)
	if len(input.OverrideJSON) > 0 {
		if err := domain.ValidateJSONPayload(input.OverrideJSON, "rerun override_json"); err != nil {
			return domain.FlowRun{}, err
		}
		payload = cloneJSON(input.OverrideJSON)
	}
	switch previous.Target() {
	case domain.RunTargetTypeFlow:
		flow, err := s.flows.GetByID(ctx, previous.FlowID)
		if err != nil {
			return domain.FlowRun{}, fmt.Errorf("get flow %q: %w", previous.FlowID, err)
		}
		if flow.WorkspaceID != workspace.ID {
			return domain.FlowRun{}, &domain.ConflictError{Entity: "flow", Field: "workspace_id", Value: string(flow.WorkspaceID)}
		}
		if _, err := s.flows.GetVersion(ctx, previous.FlowID, previous.FlowVersion); err != nil {
			return domain.FlowRun{}, fmt.Errorf("get flow %q version %d: %w", previous.FlowID, previous.FlowVersion, err)
		}
		if err := s.validateLaunchableFlowVersion(ctx, workspace, previous.FlowID, previous.FlowVersion); err != nil {
			return domain.FlowRun{}, err
		}
		return s.enqueueFlowRun(ctx, workspace, previous.FlowID, previous.FlowVersion, input.InitiatedBy, payload, input.Queue, input.IdempotencyKey)
	case domain.RunTargetTypeSavedRequest:
		return s.LaunchSavedRequest(ctx, LaunchSavedRequestInput{
			WorkspaceID:    previous.WorkspaceID,
			SavedRequestID: previous.SavedRequestID,
			InitiatedBy:    input.InitiatedBy,
			InputJSON:      payload,
			Queue:          input.Queue,
			IdempotencyKey: input.IdempotencyKey,
		})
	default:
		return domain.FlowRun{}, &domain.ValidationError{Message: fmt.Sprintf("unsupported rerun target_type %q", previous.Target())}
	}
}

func cloneJSON(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return nil
	}
	cloned := make(json.RawMessage, len(payload))
	copy(cloned, payload)
	return cloned
}

func (s *RunCoordinator) validateLaunchableFlow(ctx context.Context, workspace domain.Workspace, flow domain.Flow) error {
	steps, err := s.flowSteps.ListByFlowID(ctx, flow.ID)
	if err != nil {
		return fmt.Errorf("list flow %q steps: %w", flow.ID, err)
	}
	return s.validateResolvedLaunchableFlow(ctx, workspace, steps)
}

func (s *RunCoordinator) validateLaunchableFlowVersion(ctx context.Context, workspace domain.Workspace, flowID domain.FlowID, version int) error {
	steps, err := s.flowSteps.ListByFlowVersion(ctx, flowID, version)
	if err != nil {
		return fmt.Errorf("list flow %q version %d steps: %w", flowID, version, err)
	}
	return s.validateResolvedLaunchableFlow(ctx, workspace, steps)
}

func (s *RunCoordinator) validateResolvedLaunchableFlow(ctx context.Context, workspace domain.Workspace, steps []domain.FlowStep) error {
	resolved := make([]domain.FlowStep, 0, len(steps))
	for _, step := range steps {
		switch step.Type() {
		case domain.FlowStepTypeInlineRequest:
			step.RequestSpec = applyWorkspacePolicyDefaults(step.RequestSpec, workspace.Policy)
			resolved = append(resolved, step)
		case domain.FlowStepTypeSavedRequestRef:
			request, err := s.savedRequests.GetByID(ctx, step.SavedRequestID)
			if err != nil {
				return fmt.Errorf("get saved request %q: %w", step.SavedRequestID, err)
			}
			if request.WorkspaceID != workspace.ID {
				return &domain.ConflictError{Entity: "saved request", Field: "workspace_id", Value: string(request.WorkspaceID)}
			}
			effective, err := step.RequestSpecOverride.Apply(request.RequestSpec)
			if err != nil {
				return &domain.ValidationError{Message: fmt.Sprintf("apply request override for step %q: %s", step.Name, err.Error())}
			}
			step.RequestSpec = applyWorkspacePolicyDefaults(effective, workspace.Policy)
			resolved = append(resolved, step)
		default:
			return &domain.ValidationError{Message: fmt.Sprintf("unsupported flow step_type %q", step.Type())}
		}
	}
	if issues := validateWorkspacePolicy(workspace.Policy, resolved); len(issues) > 0 {
		return &domain.ValidationError{Message: strings.Join(issues, "; ")}
	}
	return nil
}

func (s *RunCoordinator) validateLaunchableSavedRequest(workspace domain.Workspace, request domain.SavedRequest) error {
	spec := applyWorkspacePolicyDefaults(request.RequestSpec, workspace.Policy)
	step := domain.FlowStep{Name: request.Name, RequestSpec: spec}
	if issues := validateWorkspacePolicy(workspace.Policy, []domain.FlowStep{step}); len(issues) > 0 {
		return &domain.ValidationError{Message: strings.Join(issues, "; ")}
	}
	return nil
}

var _ RunService = (*RunCoordinator)(nil)
