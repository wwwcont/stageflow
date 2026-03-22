package execution

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"stageflow/internal/domain"
	"stageflow/internal/observability"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type FailureKind string

const (
	FailureKindValidation FailureKind = "validation_error"
	FailureKindTransport  FailureKind = "transport_error"
	FailureKindAssertion  FailureKind = "assertion_failure"
	FailureKindInternal   FailureKind = "internal_execution_error"
)

type StepAttemptRecord struct {
	Attempt          int                    `json:"attempt"`
	StartedAt        time.Time              `json:"started_at"`
	FinishedAt       time.Time              `json:"finished_at"`
	DurationMillis   int64                  `json:"duration_ms"`
	RequestSnapshot  map[string]any         `json:"request_snapshot,omitempty"`
	ResponseSnapshot map[string]any         `json:"response_snapshot,omitempty"`
	ExtractedValues  map[string]any         `json:"extracted_values,omitempty"`
	ErrorKind        FailureKind            `json:"error_kind,omitempty"`
	ErrorMessage     string                 `json:"error_message,omitempty"`
	Retryable        bool                   `json:"retryable"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
}

type StepIDFactory interface {
	NewRunStepID(runID domain.RunID, step domain.FlowStep) domain.RunStepID
}

type DeterministicStepIDFactory struct{}

func (DeterministicStepIDFactory) NewRunStepID(runID domain.RunID, step domain.FlowStep) domain.RunStepID {
	name := strings.ToLower(strings.TrimSpace(step.Name))
	name = strings.ReplaceAll(name, " ", "-")
	return domain.RunStepID(fmt.Sprintf("%s-step-%02d-%s", runID, step.OrderIndex, name))
}

type systemSleeper struct{}

func (systemSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type SequentialEngine struct {
	workspaces    repository.WorkspaceRepository
	savedRequests repository.SavedRequestRepository
	flows         repository.FlowRepository
	flowSteps     repository.FlowStepRepository
	runs          repository.RunRepository
	runSteps      repository.RunStepRepository
	renderer      TemplateResolver
	http          HTTPExecutor
	extractor     Extractor
	asserter      Asserter
	events        RunEventPublisher
	clock         clock.Clock
	sleeper       Sleeper
	stepIDs       StepIDFactory
	logger        *zap.Logger
	metrics       *observability.Metrics
	tracer        trace.Tracer
}

type executionPlan struct {
	run       domain.FlowRun
	workspace domain.Workspace
	steps     []domain.FlowStep
}

type nopRunEventPublisher struct{}

func (nopRunEventPublisher) PublishRunEvent(context.Context, domain.RunEvent) error { return nil }

func NewSequentialEngine(
	workspaces repository.WorkspaceRepository,
	savedRequests repository.SavedRequestRepository,
	flows repository.FlowRepository,
	flowSteps repository.FlowStepRepository,
	runs repository.RunRepository,
	runSteps repository.RunStepRepository,
	renderer TemplateResolver,
	httpExecutor HTTPExecutor,
	extractor Extractor,
	asserter Asserter,
	clk clock.Clock,
) (*SequentialEngine, error) {
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
	case renderer == nil:
		return nil, fmt.Errorf("template resolver is required")
	case httpExecutor == nil:
		return nil, fmt.Errorf("http executor is required")
	case extractor == nil:
		return nil, fmt.Errorf("extractor is required")
	case asserter == nil:
		return nil, fmt.Errorf("asserter is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &SequentialEngine{
		workspaces:    workspaces,
		savedRequests: savedRequests,
		flows:         flows,
		flowSteps:     flowSteps,
		runs:          runs,
		runSteps:      runSteps,
		renderer:      renderer,
		http:          httpExecutor,
		extractor:     extractor,
		asserter:      asserter,
		events:        nopRunEventPublisher{},
		clock:         clk,
		sleeper:       systemSleeper{},
		stepIDs:       DeterministicStepIDFactory{},
		logger:        zap.NewNop(),
		tracer:        otel.Tracer("stageflow/execution"),
	}, nil
}

func (e *SequentialEngine) SetObservability(logger *zap.Logger, metrics *observability.Metrics) {
	if logger != nil {
		e.logger = logger
	}
	e.metrics = metrics
}

func (e *SequentialEngine) SetEventPublisher(publisher RunEventPublisher) {
	if publisher == nil {
		e.events = nopRunEventPublisher{}
		return
	}
	e.events = publisher
}

func (e *SequentialEngine) ExecuteRun(ctx context.Context, runID domain.RunID) (err error) {
	plan, err := e.loadDefinition(ctx, runID)
	if err != nil {
		return err
	}
	run := plan.run
	workspace := plan.workspace
	steps := plan.steps
	ctx = observability.WithRunContext(ctx, observability.RunContext{
		WorkspaceID:    workspace.ID,
		RunID:          run.ID,
		FlowID:         run.FlowID,
		SavedRequestID: run.SavedRequestID,
		TargetType:     run.Target(),
	})
	attrs := observability.RunContextAttributes(observability.RunContext{
		WorkspaceID:    workspace.ID,
		RunID:          run.ID,
		FlowID:         run.FlowID,
		SavedRequestID: run.SavedRequestID,
		TargetType:     run.Target(),
	})
	if run.IsFlowRun() {
		attrs = append(attrs, attribute.Int("flow.version", run.FlowVersion))
	}
	ctx, span := e.tracer.Start(ctx, "flow.run", trace.WithAttributes(attrs...))
	defer func() {
		logFields := observability.RunContextLogFields(observability.RunContext{
			WorkspaceID:    workspace.ID,
			RunID:          run.ID,
			FlowID:         run.FlowID,
			SavedRequestID: run.SavedRequestID,
			TargetType:     run.Target(),
		})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			logFields = append(logFields, zap.String("error", sanitizeErrorMessage(err.Error(), workspace.SecretMap())))
			e.logger.Error("run execution failed", logFields...)
		} else {
			span.SetStatus(codes.Ok, "")
			e.logger.Info("run execution finished", logFields...)
		}
		span.End()
	}()
	runCtx := ctx
	if workspace.Policy.MaxRunDurationSeconds > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(workspace.Policy.MaxRunDurationSeconds)*time.Second)
		defer cancel()
	}
	if run.IsTerminal() {
		return nil
	}
	startFields := append(observability.RunContextLogFields(observability.RunContext{
		WorkspaceID:    workspace.ID,
		RunID:          run.ID,
		FlowID:         run.FlowID,
		SavedRequestID: run.SavedRequestID,
		TargetType:     run.Target(),
	}), zap.Int("steps_count", len(steps)))
	e.logger.Info("run execution started", startFields...)
	e.publishRunEvent(runCtx, domain.RunEvent{
		RunID:     run.ID,
		EventType: domain.RunEventTypeRunStarted,
		Level:     domain.RunEventLevelInfo,
		Message:   fmt.Sprintf("Run started with %d step(s).", len(steps)),
		DetailsJSON: mustJSON(map[string]any{
			"target_type": run.Target(),
			"steps_count": len(steps),
		}),
	})

	if run.Status != domain.RunStatusRunning {
		now := e.clock.Now().UTC()
		startedRun, transitionErr := run.MarkRunning(now)
		if transitionErr != nil {
			return transitionErr
		}
		transition := repository.RunStatusTransition{
			Status:       startedRun.Status,
			StartedAt:    startedRun.StartedAt,
			FinishedAt:   startedRun.FinishedAt,
			ErrorMessage: startedRun.ErrorMessage,
		}
		if err := e.runs.TransitionStatus(runCtx, run.ID, []domain.RunStatus{domain.RunStatusPending, domain.RunStatusQueued}, transition); err != nil {
			var conflictErr *domain.ConflictError
			if errors.As(err, &conflictErr) {
				refreshed, getErr := e.runs.GetByID(runCtx, run.ID)
				if getErr != nil {
					return fmt.Errorf("refresh run after transition conflict: %w", getErr)
				}
				if refreshed.IsTerminal() || refreshed.Status == domain.RunStatusRunning {
					run = refreshed
				} else {
					return fmt.Errorf("transition run %q to running: %w", run.ID, err)
				}
			} else {
				return fmt.Errorf("transition run %q to running: %w", run.ID, err)
			}
		} else {
			run = startedRun
		}
		if run.IsTerminal() {
			return nil
		}
	}

	stepState, err := e.hydrateRunSteps(runCtx, run.ID)
	if err != nil {
		return err
	}

	for _, step := range steps {
		if runCtx.Err() != nil {
			return e.failRun(ctx, run, FailureKindTransport, &TimeoutError{Operation: "run execution"})
		}
		if stepRecord, ok := stepState.byOrder[step.OrderIndex]; ok && stepRecord.Status == domain.RunStepStatusSucceeded {
			if err := stepState.loadExtractedValues(step, stepRecord); err != nil {
				return e.failRun(runCtx, run, FailureKindInternal, fmt.Errorf("hydrate step %q outputs: %w", step.Name, err))
			}
			continue
		}
		if err := e.executeStep(runCtx, run, workspace, step, stepState); err != nil {
			return e.failRun(runCtx, run, classifyFailure(err), err)
		}
	}

	finishedRun, err := run.MarkSucceeded(e.clock.Now().UTC())
	if err != nil {
		return e.failRun(runCtx, run, FailureKindInternal, err)
	}
	if err := e.runs.TransitionStatus(runCtx, run.ID, []domain.RunStatus{domain.RunStatusRunning}, repository.RunStatusTransition{
		Status:       finishedRun.Status,
		StartedAt:    finishedRun.StartedAt,
		FinishedAt:   finishedRun.FinishedAt,
		ErrorMessage: finishedRun.ErrorMessage,
	}); err != nil {
		return fmt.Errorf("mark run %q succeeded: %w", run.ID, err)
	}
	e.publishRunEvent(runCtx, domain.RunEvent{
		RunID:     run.ID,
		EventType: domain.RunEventTypeRunFinished,
		Level:     domain.RunEventLevelInfo,
		Message:   "Run finished successfully.",
		DetailsJSON: mustJSON(map[string]any{
			"duration_ms": durationMillis(run.StartedAt, finishedRun.FinishedAt),
		}),
	})
	return nil
}

func (e *SequentialEngine) loadDefinition(ctx context.Context, runID domain.RunID) (executionPlan, error) {
	run, err := e.runs.GetByID(ctx, runID)
	if err != nil {
		return executionPlan{}, fmt.Errorf("get run %q: %w", runID, err)
	}
	if err := run.Validate(); err != nil {
		return executionPlan{}, err
	}
	workspace, err := e.workspaces.GetByID(ctx, run.WorkspaceID)
	if err != nil {
		return executionPlan{}, fmt.Errorf("get workspace %q: %w", run.WorkspaceID, err)
	}
	if err := workspace.Validate(); err != nil {
		return executionPlan{}, err
	}
	steps, err := e.loadExecutionSteps(ctx, run, workspace)
	if err != nil {
		return executionPlan{}, err
	}
	if err := validateExecutionPlan(steps); err != nil {
		return executionPlan{}, err
	}
	return executionPlan{run: run, workspace: workspace, steps: steps}, nil
}

func (e *SequentialEngine) loadExecutionSteps(ctx context.Context, run domain.FlowRun, workspace domain.Workspace) ([]domain.FlowStep, error) {
	switch run.Target() {
	case domain.RunTargetTypeFlow:
		flow, err := e.flows.GetVersion(ctx, run.FlowID, run.FlowVersion)
		if err != nil {
			return nil, fmt.Errorf("get flow %q version %d: %w", run.FlowID, run.FlowVersion, err)
		}
		if err := flow.Validate(); err != nil {
			return nil, err
		}
		steps, err := e.flowSteps.ListByFlowVersion(ctx, run.FlowID, run.FlowVersion)
		if err != nil {
			return nil, fmt.Errorf("list flow %q version %d steps: %w", run.FlowID, run.FlowVersion, err)
		}
		return e.resolveExecutionSteps(ctx, workspace, steps)
	case domain.RunTargetTypeSavedRequest:
		request, err := e.savedRequests.GetByID(ctx, run.SavedRequestID)
		if err != nil {
			return nil, fmt.Errorf("get saved request %q: %w", run.SavedRequestID, err)
		}
		if request.WorkspaceID != workspace.ID {
			return nil, &domain.ExecutionError{Operation: fmt.Sprintf("load saved request %q", request.ID), Cause: fmt.Errorf("saved request workspace %q does not match run workspace %q", request.WorkspaceID, workspace.ID)}
		}
		spec := applyExecutionWorkspacePolicy(request.RequestSpec, workspace.Policy)
		now := e.clock.Now().UTC()
		return []domain.FlowStep{{
			ID:             domain.FlowStepID(fmt.Sprintf("%s-step-0", run.SavedRequestID)),
			FlowID:         domain.FlowID(run.SavedRequestID),
			OrderIndex:     0,
			Name:           request.Name,
			StepType:       domain.FlowStepTypeInlineRequest,
			RequestSpec:    spec,
			ExtractionSpec: domain.ExtractionSpec{},
			AssertionSpec:  domain.AssertionSpec{},
			CreatedAt:      now,
			UpdatedAt:      now,
		}}, nil
	default:
		return nil, &domain.ValidationError{Message: fmt.Sprintf("unsupported run target_type %q", run.Target())}
	}
}

func (e *SequentialEngine) resolveExecutionSteps(ctx context.Context, workspace domain.Workspace, steps []domain.FlowStep) ([]domain.FlowStep, error) {
	resolved := make([]domain.FlowStep, 0, len(steps))
	for _, step := range steps {
		switch step.Type() {
		case domain.FlowStepTypeInlineRequest:
			step.RequestSpec = applyExecutionWorkspacePolicy(step.RequestSpec, workspace.Policy)
			resolved = append(resolved, step)
		case domain.FlowStepTypeSavedRequestRef:
			request, err := e.savedRequests.GetByID(ctx, step.SavedRequestID)
			if err != nil {
				return nil, &domain.ExecutionError{Operation: fmt.Sprintf("resolve saved request %q for step %q", step.SavedRequestID, step.Name), Cause: err}
			}
			if request.WorkspaceID != workspace.ID {
				return nil, &domain.ExecutionError{Operation: fmt.Sprintf("resolve saved request %q for step %q", step.SavedRequestID, step.Name), Cause: fmt.Errorf("saved request workspace %q does not match flow workspace %q", request.WorkspaceID, workspace.ID)}
			}
			effective, err := step.RequestSpecOverride.Apply(request.RequestSpec)
			if err != nil {
				return nil, &domain.ExecutionError{Operation: fmt.Sprintf("apply request override for step %q", step.Name), Cause: err}
			}
			resolvedStep := step
			resolvedStep.StepType = domain.FlowStepTypeInlineRequest
			resolvedStep.SavedRequestID = ""
			resolvedStep.RequestSpecOverride = domain.RequestSpecOverride{}
			resolvedStep.RequestSpec = applyExecutionWorkspacePolicy(effective, workspace.Policy)
			resolved = append(resolved, resolvedStep)
		default:
			return nil, &domain.ExecutionError{Operation: fmt.Sprintf("resolve step %q", step.Name), Cause: fmt.Errorf("unsupported step_type %q", step.StepType)}
		}
	}
	return resolved, nil
}

type hydratedRunSteps struct {
	byOrder         map[int]domain.FlowRunStep
	extractedByStep map[string]map[string]any
	runtimeVars     map[string]any
}

func (e *SequentialEngine) hydrateRunSteps(ctx context.Context, runID domain.RunID) (*hydratedRunSteps, error) {
	persisted, err := e.runSteps.ListByRunID(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list run %q steps: %w", runID, err)
	}
	state := &hydratedRunSteps{byOrder: make(map[int]domain.FlowRunStep, len(persisted)), extractedByStep: map[string]map[string]any{}, runtimeVars: map[string]any{}}
	for _, step := range persisted {
		state.byOrder[step.StepOrder] = step
	}
	return state, nil
}

func (s *hydratedRunSteps) loadExtractedValues(step domain.FlowStep, record domain.FlowRunStep) error {
	if len(record.ExtractedValuesJSON) == 0 {
		s.extractedByStep[step.Name] = map[string]any{}
		return nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(record.ExtractedValuesJSON, &decoded); err != nil {
		return err
	}
	s.extractedByStep[step.Name] = decoded
	for key, value := range decoded {
		s.runtimeVars[key] = value
	}
	return nil
}

func (e *SequentialEngine) executeStep(ctx context.Context, run domain.FlowRun, workspace domain.Workspace, step domain.FlowStep, state *hydratedRunSteps) error {
	ctx, span := e.tracer.Start(ctx, "flow.step", trace.WithAttributes(
		attribute.String("run.id", string(run.ID)),
		attribute.String("workspace.id", string(workspace.ID)),
		attribute.String("run.target_type", string(run.Target())),
		attribute.String("step.name", step.Name),
		attribute.Int("step.order", step.OrderIndex),
	))
	if run.FlowID != "" {
		span.SetAttributes(attribute.String("flow.id", string(run.FlowID)))
	}
	if run.SavedRequestID != "" {
		span.SetAttributes(attribute.String("saved_request.id", string(run.SavedRequestID)))
	}
	defer span.End()

	renderData, err := NewRenderData(run, workspace, state.runtimeVars, state.extractedByStep)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &domain.ExecutionError{Operation: fmt.Sprintf("build render data for step %q", step.Name), Cause: err}
	}

	record, exists := state.byOrder[step.OrderIndex]
	if !exists {
		now := e.clock.Now().UTC()
		record = domain.FlowRunStep{
			ID:        e.stepIDs.NewRunStepID(run.ID, step),
			RunID:     run.ID,
			StepName:  step.Name,
			StepOrder: step.OrderIndex,
			Status:    domain.RunStepStatusPending,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := e.runSteps.Create(ctx, record); err != nil {
			return &domain.ExecutionError{Operation: fmt.Sprintf("create run step %q", step.Name), Cause: err}
		}
		state.byOrder[step.OrderIndex] = record
	}

	attemptHistory, err := decodeAttemptHistory(record.AttemptHistoryJSON)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return &domain.ExecutionError{Operation: fmt.Sprintf("decode attempt history for step %q", step.Name), Cause: err}
	}
	attemptsLimit := retryAttempts(step.RequestSpec.RetryPolicy)
	for attempt := record.RetryCount + 1; attempt <= attemptsLimit; attempt++ {
		attemptStartedAt := e.clock.Now().UTC()
		startFields := append(observability.RunContextLogFields(observability.RunContext{
			WorkspaceID:    workspace.ID,
			RunID:          run.ID,
			FlowID:         run.FlowID,
			SavedRequestID: run.SavedRequestID,
			TargetType:     run.Target(),
		}), zap.String("step_name", step.Name), zap.Int("step_order", step.OrderIndex), zap.Int("attempt", attempt))
		e.logger.Info("step attempt started", startFields...)
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepStarted, domain.RunEventLevelInfo, fmt.Sprintf("Step %q started.", step.Name), nil)
		if attempt > 1 {
			e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepRetryAttempt, domain.RunEventLevelWarn, fmt.Sprintf("Retry attempt %d for step %q started.", attempt, step.Name), nil)
		}
		record = record.MarkRunning(attemptStartedAt)
		record.RetryCount = attempt - 1
		if err := e.runSteps.Update(ctx, record); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &domain.ExecutionError{Operation: fmt.Sprintf("mark step %q running", step.Name), Cause: err}
		}

		attemptCtx, cancel := stepAttemptContext(ctx, step.RequestSpec.Timeout)
		attemptRecord, completedStep, execErr := e.performStepAttempt(attemptCtx, run, workspace, step, renderData, record, attempt, attemptStartedAt)
		cancel()
		attemptHistory = append(attemptHistory, attemptRecord)
		encodedHistory, err := encodeJSON(attemptHistory)
		if err != nil {
			return &domain.ExecutionError{Operation: fmt.Sprintf("encode attempt history for step %q", step.Name), Cause: err}
		}
		completedStep.AttemptHistoryJSON = encodedHistory
		completedStep.RetryCount = attempt - 1
		e.observeStepAttempt(step.Name, attemptRecord, execErr)
		if execErr == nil {
			completedStep = completedStep.MarkSucceeded(e.clock.Now().UTC())
			if err := e.runSteps.Update(ctx, completedStep); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return &domain.ExecutionError{Operation: fmt.Sprintf("persist successful step %q", step.Name), Cause: err}
			}
			state.byOrder[step.OrderIndex] = completedStep
			if err := state.loadExtractedValues(step, completedStep); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
				return &domain.ExecutionError{Operation: fmt.Sprintf("hydrate successful step %q", step.Name), Cause: err}
			}
			successFields := append(observability.RunContextLogFields(observability.RunContext{
				WorkspaceID:    workspace.ID,
				RunID:          run.ID,
				FlowID:         run.FlowID,
				SavedRequestID: run.SavedRequestID,
				TargetType:     run.Target(),
			}), zap.String("step_name", step.Name), zap.Int("step_order", step.OrderIndex), zap.Int("attempt", attempt), zap.Duration("duration", attemptRecord.FinishedAt.Sub(attemptRecord.StartedAt)))
			e.logger.Info("step attempt succeeded", successFields...)
			e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepFinished, domain.RunEventLevelInfo, fmt.Sprintf("Step %q finished successfully.", step.Name), map[string]any{
				"duration_ms": attemptRecord.DurationMillis,
				"status_code": retryableStatusFromAttempt(attemptRecord),
				"target":      targetSummaryFromAttempt(attemptRecord),
			})
			span.SetStatus(codes.Ok, "")
			return nil
		}

		failureKind := classifyFailure(execErr)
		completedStep = completedStep.MarkFailed(e.clock.Now().UTC(), formatFailureMessage(failureKind, execErr))
		retryable := shouldRetry(step.RequestSpec.RetryPolicy, attemptRecord, execErr, attempt, attemptsLimit)
		if retryable {
			completedStep.Status = domain.RunStepStatusRunning
			completedStep.FinishedAt = nil
			completedStep.ErrorMessage = formatFailureMessage(failureKind, execErr)
		}
		if err := e.runSteps.Update(ctx, completedStep); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &domain.ExecutionError{Operation: fmt.Sprintf("persist failed step %q", step.Name), Cause: err}
		}
		state.byOrder[step.OrderIndex] = completedStep
		failFields := append(observability.RunContextLogFields(observability.RunContext{
			WorkspaceID:    workspace.ID,
			RunID:          run.ID,
			FlowID:         run.FlowID,
			SavedRequestID: run.SavedRequestID,
			TargetType:     run.Target(),
		}), zap.String("step_name", step.Name), zap.Int("step_order", step.OrderIndex), zap.Int("attempt", attempt), zap.String("failure_kind", string(failureKind)), zap.Bool("retryable", retryable), zap.String("error", sanitizeErrorMessage(execErr.Error(), renderData.Secrets)))
		e.logger.Warn("step attempt failed", failFields...)
		e.publishFailureEvent(ctx, run, step, attempt, failureKind, execErr, attemptRecord.ErrorMessage, retryable, retryDelay(step.RequestSpec.RetryPolicy, attempt))
		if !retryable {
			span.RecordError(execErr)
			span.SetStatus(codes.Error, execErr.Error())
			return execErr
		}
		delay := retryDelay(step.RequestSpec.RetryPolicy, attempt)
		if err := e.sleeper.Sleep(ctx, delay); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return &domain.ExecutionError{Operation: fmt.Sprintf("sleep before retrying step %q", step.Name), Cause: err}
		}
		record = completedStep
	}
	span.SetStatus(codes.Error, "step exhausted attempts without terminal result")
	return &domain.ExecutionError{Operation: fmt.Sprintf("execute step %q", step.Name), Cause: fmt.Errorf("step exhausted attempts without terminal result")}
}

func (e *SequentialEngine) performStepAttempt(ctx context.Context, run domain.FlowRun, workspace domain.Workspace, step domain.FlowStep, data RenderData, stepRecord domain.FlowRunStep, attempt int, attemptStartedAt time.Time) (StepAttemptRecord, domain.FlowRunStep, error) {
	attemptRecord := StepAttemptRecord{Attempt: attempt, StartedAt: attemptStartedAt, Metadata: map[string]interface{}{"step_name": step.Name, "step_order": step.OrderIndex}}
	resolved, err := e.renderer.Resolve(ctx, step.RequestSpec, data)
	if err != nil {
		attemptRecord.FinishedAt = e.clock.Now().UTC()
		attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
		attemptRecord.ErrorKind = classifyFailure(err)
		attemptRecord.ErrorMessage = err.Error()
		attemptRecord.Retryable = false
		return attemptRecord, stepRecord, err
	}

	executedSpec := domain.RequestSpec{
		Method:       resolved.Method,
		URLTemplate:  resolved.URL,
		Headers:      resolved.Headers,
		BodyTemplate: resolved.Body,
		Timeout:      step.RequestSpec.Timeout,
		RetryPolicy:  step.RequestSpec.RetryPolicy,
	}
	response, err := e.http.Execute(ctx, executedSpec, HTTPExecutionPolicy{
		AllowedHosts:        workspace.Policy.AllowedHosts,
		MaxRequestBodyBytes: int64(workspace.Policy.MaxRequestBodyBytes),
		DefaultTimeout:      time.Duration(workspace.Policy.DefaultTimeoutMS) * time.Millisecond,
		SecretValues:        data.Secrets,
	})
	sanitizedRequestSnapshot := sanitizeSecretValue(response.RequestSnapshot, data.Secrets).(HTTPRequestSnapshot)
	requestSnapshot, marshalErr := encodeJSON(sanitizedRequestSnapshot)
	if marshalErr != nil {
		return attemptRecord, stepRecord, &domain.ExecutionError{Operation: fmt.Sprintf("encode request snapshot for step %q", step.Name), Cause: marshalErr}
	}
	stepRecord.RequestSnapshotJSON = requestSnapshot
	if err == nil {
		sanitizedResponseSnapshot := sanitizeSecretValue(response.ResponseSnapshot, data.Secrets).(HTTPResponseSnapshot)
		responseSnapshot, responseMarshalErr := encodeJSON(sanitizedResponseSnapshot)
		if responseMarshalErr != nil {
			return attemptRecord, stepRecord, &domain.ExecutionError{Operation: fmt.Sprintf("encode response snapshot for step %q", step.Name), Cause: responseMarshalErr}
		}
		stepRecord.ResponseSnapshotJSON = responseSnapshot
	}
	if err != nil {
		attemptRecord.FinishedAt = e.clock.Now().UTC()
		attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
		attemptRecord.RequestSnapshot = map[string]any{"method": sanitizedRequestSnapshot.Method, "url": sanitizedRequestSnapshot.URL, "headers": sanitizedRequestSnapshot.Headers, "body": sanitizedRequestSnapshot.Body}
		attemptRecord.ErrorKind = classifyFailure(err)
		attemptRecord.ErrorMessage = sanitizeErrorMessage(err.Error(), data.Secrets)
		return attemptRecord, stepRecord, err
	}
	sanitizedResponseSnapshot := sanitizeSecretValue(response.ResponseSnapshot, data.Secrets).(HTTPResponseSnapshot)
	attemptRecord.RequestSnapshot = map[string]any{"method": sanitizedRequestSnapshot.Method, "url": sanitizedRequestSnapshot.URL, "headers": sanitizedRequestSnapshot.Headers, "body": sanitizedRequestSnapshot.Body}
	attemptRecord.ResponseSnapshot = map[string]any{"status_code": sanitizedResponseSnapshot.StatusCode, "headers": sanitizedResponseSnapshot.Headers, "body": sanitizedResponseSnapshot.Body}
	if isRetryableStatus(step.RequestSpec.RetryPolicy, response.Response.StatusCode) {
		retryErr := &domain.TransportError{Operation: fmt.Sprintf("received retryable status %d", response.Response.StatusCode), Cause: nil}
		attemptRecord.FinishedAt = e.clock.Now().UTC()
		attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
		attemptRecord.ErrorKind = FailureKindTransport
		attemptRecord.ErrorMessage = sanitizeErrorMessage(retryErr.Error(), data.Secrets)
		attemptRecord.Retryable = true
		return attemptRecord, stepRecord, retryErr
	}

	extractedValues, err := e.extractor.Extract(ctx, step.ExtractionSpec, response.Response)
	if err != nil {
		attemptRecord.FinishedAt = e.clock.Now().UTC()
		attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
		attemptRecord.ErrorKind = classifyFailure(err)
		attemptRecord.ErrorMessage = err.Error()
		return attemptRecord, stepRecord, err
	}
	encodedExtracted, marshalErr := encodeJSON(extractedValues)
	if marshalErr != nil {
		return attemptRecord, stepRecord, &domain.ExecutionError{Operation: fmt.Sprintf("encode extracted values for step %q", step.Name), Cause: marshalErr}
	}
	stepRecord.ExtractedValuesJSON = encodedExtracted
	attemptRecord.ExtractedValues = extractedValues
	if len(extractedValues) > 0 {
		keys := make([]string, 0, len(extractedValues))
		for key := range extractedValues {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeExtractionDone, domain.RunEventLevelInfo, fmt.Sprintf("Extraction completed for step %q.", step.Name), map[string]any{
			"variables": keys,
			"count":     len(keys),
		})
	}

	if err := e.asserter.Assert(ctx, step.AssertionSpec, response.Response); err != nil {
		attemptRecord.FinishedAt = e.clock.Now().UTC()
		attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
		attemptRecord.ErrorKind = classifyFailure(err)
		attemptRecord.ErrorMessage = sanitizeErrorMessage(err.Error(), data.Secrets)
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeAssertionsFailed, domain.RunEventLevelError, fmt.Sprintf("Assertions failed for step %q.", step.Name), map[string]any{
			"error": sanitizeErrorMessage(err.Error(), data.Secrets),
		})
		return attemptRecord, stepRecord, err
	}
	if len(step.AssertionSpec.Rules) > 0 {
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeAssertionsPassed, domain.RunEventLevelInfo, fmt.Sprintf("Assertions passed for step %q.", step.Name), map[string]any{
			"rules_count": len(step.AssertionSpec.Rules),
		})
	}
	attemptRecord.FinishedAt = e.clock.Now().UTC()
	attemptRecord.DurationMillis = attemptRecord.FinishedAt.Sub(attemptStartedAt).Milliseconds()
	return attemptRecord, stepRecord, nil
}

func sanitizeErrorMessage(message string, secrets map[string]string) string {
	sanitized, ok := sanitizeSecretValue(message, secrets).(string)
	if !ok {
		return message
	}
	return sanitized
}

func sanitizeSecretValue(value any, secrets map[string]string) any {
	switch typed := value.(type) {
	case string:
		return redactSecretString(typed, secrets)
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for key, item := range typed {
			cloned[key] = redactSecretString(item, secrets)
		}
		return cloned
	case map[string][]string:
		cloned := make(map[string][]string, len(typed))
		for key, values := range typed {
			next := make([]string, len(values))
			for idx, item := range values {
				next[idx] = redactSecretString(item, secrets)
			}
			cloned[key] = next
		}
		return cloned
	case HTTPRequestSnapshot:
		typed.URL = redactSecretString(typed.URL, secrets)
		typed.Headers = sanitizeSecretValue(typed.Headers, secrets).(map[string]string)
		typed.Body = redactSecretString(typed.Body, secrets)
		return typed
	case HTTPResponseSnapshot:
		typed.Headers = sanitizeSecretValue(typed.Headers, secrets).(map[string][]string)
		typed.Body = redactSecretString(typed.Body, secrets)
		return typed
	default:
		return value
	}
}

func redactSecretString(raw string, secrets map[string]string) string {
	sanitized := raw
	for _, secret := range secrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		sanitized = strings.ReplaceAll(sanitized, secret, "[REDACTED]")
	}
	return sanitized
}

func (e *SequentialEngine) failRun(ctx context.Context, run domain.FlowRun, kind FailureKind, cause error) error {
	failedRun, markErr := run.MarkFailed(e.clock.Now().UTC(), formatFailureMessage(kind, cause))
	if markErr != nil {
		return &domain.ExecutionError{Operation: fmt.Sprintf("mark run %q failed", run.ID), Cause: errors.Join(markErr, cause)}
	}
	if err := e.runs.UpdateStatus(ctx, run.ID, failedRun.Status, failedRun.StartedAt, failedRun.FinishedAt, failedRun.ErrorMessage); err != nil {
		return &domain.ExecutionError{Operation: fmt.Sprintf("persist failed run %q", run.ID), Cause: errors.Join(cause, err)}
	}
	e.publishRunEvent(ctx, domain.RunEvent{
		RunID:     run.ID,
		EventType: domain.RunEventTypeRunFailed,
		Level:     domain.RunEventLevelError,
		Message:   "Run failed.",
		DetailsJSON: mustJSON(map[string]any{
			"failure_kind":  kind,
			"error":         sanitizeErrorMessage(cause.Error(), map[string]string{}),
			"duration_ms":   durationMillis(run.StartedAt, failedRun.FinishedAt),
			"final_message": failedRun.ErrorMessage,
		}),
	})
	return cause
}

func (e *SequentialEngine) publishRunEvent(ctx context.Context, event domain.RunEvent) {
	if err := e.events.PublishRunEvent(ctx, event); err != nil {
		e.logger.Debug("publish run event failed", zap.Error(err))
	}
}

func (e *SequentialEngine) publishStepAttemptEvent(ctx context.Context, run domain.FlowRun, step domain.FlowStep, attempt int, eventType domain.RunEventType, level domain.RunEventLevel, message string, details map[string]any) {
	stepOrder := step.OrderIndex
	eventAttempt := attempt
	event := domain.RunEvent{
		RunID:     run.ID,
		EventType: eventType,
		Level:     level,
		StepName:  step.Name,
		StepOrder: &stepOrder,
		Attempt:   &eventAttempt,
		Message:   message,
	}
	if len(details) > 0 {
		event.DetailsJSON = mustJSON(details)
	}
	e.publishRunEvent(ctx, event)
}

func (e *SequentialEngine) publishFailureEvent(ctx context.Context, run domain.FlowRun, step domain.FlowStep, attempt int, kind FailureKind, execErr error, safeMessage string, retryable bool, delay time.Duration) {
	message := strings.TrimSpace(safeMessage)
	if message == "" {
		message = execErr.Error()
	}
	baseDetails := map[string]any{
		"failure_kind": kind,
		"error":        message,
		"retryable":    retryable,
		"backoff_ms":   delay.Milliseconds(),
	}
	switch {
	case errors.Is(execErr, domain.ErrAssertionFailure):
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeAssertionsFailed, domain.RunEventLevelError, fmt.Sprintf("Assertions failed for step %q.", step.Name), baseDetails)
	case kind == FailureKindTransport:
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeTransportError, chooseLevel(retryable), fmt.Sprintf("Transport error on step %q.", step.Name), baseDetails)
	case kind == FailureKindValidation:
		var policyErr *PolicyViolationError
		if errors.As(execErr, &policyErr) {
			e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypePolicyViolation, domain.RunEventLevelError, fmt.Sprintf("Policy violation on step %q.", step.Name), baseDetails)
		} else {
			e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeValidationError, domain.RunEventLevelError, fmt.Sprintf("Validation error on step %q.", step.Name), baseDetails)
		}
	default:
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepFailed, chooseLevel(retryable), fmt.Sprintf("Step %q failed.", step.Name), baseDetails)
	}
	if retryable {
		e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepRetryScheduled, domain.RunEventLevelWarn, fmt.Sprintf("Retry scheduled for step %q.", step.Name), map[string]any{
			"backoff_ms": delay.Milliseconds(),
			"attempt":    attempt + 1,
		})
		return
	}
	e.publishStepAttemptEvent(ctx, run, step, attempt, domain.RunEventTypeStepFailed, domain.RunEventLevelError, fmt.Sprintf("Step %q failed permanently.", step.Name), baseDetails)
}

func chooseLevel(retryable bool) domain.RunEventLevel {
	if retryable {
		return domain.RunEventLevelWarn
	}
	return domain.RunEventLevelError
}

func mustJSON(value map[string]any) json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}

func durationMillis(startedAt, finishedAt *time.Time) int64 {
	if startedAt == nil || finishedAt == nil {
		return 0
	}
	return finishedAt.Sub(*startedAt).Milliseconds()
}

func targetSummaryFromAttempt(attempt StepAttemptRecord) string {
	if attempt.RequestSnapshot == nil {
		return ""
	}
	method, _ := attempt.RequestSnapshot["method"].(string)
	rawURL, _ := attempt.RequestSnapshot["url"].(string)
	if rawURL == "" {
		return strings.TrimSpace(method)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return strings.TrimSpace(method + " " + rawURL)
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s%s", method, parsed.Hostname(), parsed.EscapedPath()))
}

func (e *SequentialEngine) observeStepAttempt(stepName string, attempt StepAttemptRecord, err error) {
	if e.metrics == nil {
		return
	}
	duration := attempt.FinishedAt.Sub(attempt.StartedAt)
	e.metrics.RecordStepAttempt(stepName, duration, err, string(attempt.ErrorKind))
}

func retryAttempts(policy domain.RetryPolicy) int {
	if !policy.Enabled {
		return 1
	}
	if policy.MaxAttempts <= 0 {
		return 1
	}
	return policy.MaxAttempts
}

func retryDelay(policy domain.RetryPolicy, attempt int) time.Duration {
	if !policy.Enabled || attempt <= 0 {
		return 0
	}
	delay := policy.InitialInterval
	if delay <= 0 {
		return 0
	}
	if policy.BackoffStrategy == domain.BackoffStrategyExponential {
		for i := 1; i < attempt; i++ {
			delay *= 2
			if policy.MaxInterval > 0 && delay >= policy.MaxInterval {
				return policy.MaxInterval
			}
		}
	}
	if policy.MaxInterval > 0 && delay > policy.MaxInterval {
		return policy.MaxInterval
	}
	return delay
}

func shouldRetry(policy domain.RetryPolicy, attempt StepAttemptRecord, err error, currentAttempt, totalAttempts int) bool {
	if currentAttempt >= totalAttempts {
		return false
	}
	if isRetryableStatus(policy, retryableStatusFromAttempt(attempt)) {
		return true
	}
	if !policy.Enabled {
		return false
	}
	kind := classifyFailure(err)
	return kind == FailureKindTransport
}

func retryableStatusFromAttempt(attempt StepAttemptRecord) int {
	if attempt.ResponseSnapshot == nil {
		return 0
	}
	status, _ := attempt.ResponseSnapshot["status_code"].(int)
	if status != 0 {
		return status
	}
	if value, ok := attempt.ResponseSnapshot["status_code"].(float64); ok {
		return int(value)
	}
	return 0
}

func isRetryableStatus(policy domain.RetryPolicy, statusCode int) bool {
	if !policy.Enabled || statusCode == 0 {
		return false
	}
	for _, candidate := range policy.RetryableStatusCodes {
		if candidate == statusCode {
			return true
		}
	}
	return false
}

func classifyFailure(err error) FailureKind {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, domain.ErrAssertionFailure):
		return FailureKindAssertion
	case errors.Is(err, domain.ErrTransport):
		return FailureKindTransport
	}
	var validationErr *domain.ValidationError
	var templateErr *TemplateError
	var policyErr *PolicyViolationError
	var timeoutErr *TimeoutError
	if errors.As(err, &timeoutErr) || errors.Is(err, context.DeadlineExceeded) {
		return FailureKindTransport
	}
	if errors.As(err, &validationErr) || errors.As(err, &templateErr) || errors.As(err, &policyErr) {
		return FailureKindValidation
	}
	return FailureKindInternal
}

func formatFailureMessage(kind FailureKind, err error) string {
	if err == nil {
		return ""
	}
	if kind == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s: %s", kind, err.Error())
}

func validateExecutionPlan(steps []domain.FlowStep) error {
	if len(steps) == 0 {
		return &domain.ValidationError{Message: "flow must contain at least one step"}
	}
	sorted := make([]domain.FlowStep, len(steps))
	copy(sorted, steps)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].OrderIndex < sorted[j].OrderIndex })
	for idx, step := range sorted {
		if err := step.Validate(); err != nil {
			return err
		}
		if step.OrderIndex != idx {
			return &domain.ValidationError{Message: fmt.Sprintf("step %q order_index must be contiguous and expected %d, got %d", step.Name, idx, step.OrderIndex)}
		}
	}
	return nil
}

func decodeAttemptHistory(payload json.RawMessage) ([]StepAttemptRecord, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var history []StepAttemptRecord
	if err := json.Unmarshal(payload, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func encodeJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func stepAttemptContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func applyExecutionWorkspacePolicy(spec domain.RequestSpec, policy domain.WorkspacePolicy) domain.RequestSpec {
	if spec.Timeout <= 0 && policy.DefaultTimeoutMS > 0 {
		spec.Timeout = time.Duration(policy.DefaultTimeoutMS) * time.Millisecond
	}
	if !spec.RetryPolicy.Enabled {
		spec.RetryPolicy = policy.DefaultRetryPolicy
	}
	return spec
}

var _ Engine = (*SequentialEngine)(nil)
