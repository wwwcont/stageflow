package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

func WrapFlowRepository(next repository.FlowRepository) repository.FlowRepository {
	return &observedFlowRepository{next: next}
}

func WrapFlowStepRepository(next repository.FlowStepRepository) repository.FlowStepRepository {
	return &observedFlowStepRepository{next: next}
}

func WrapRunRepository(next repository.RunRepository, metrics *Metrics) repository.RunRepository {
	return &observedRunRepository{next: next, metrics: metrics}
}

func WrapRunStepRepository(next repository.RunStepRepository) repository.RunStepRepository {
	return &observedRunStepRepository{next: next}
}

type observedFlowRepository struct{ next repository.FlowRepository }

type observedFlowStepRepository struct{ next repository.FlowStepRepository }

type observedRunRepository struct {
	next    repository.RunRepository
	metrics *Metrics
}

type observedRunStepRepository struct{ next repository.RunStepRepository }

func (r *observedFlowRepository) Create(ctx context.Context, flow domain.Flow) error {
	ctx, span := startRepoSpan(ctx, "flow.create", attribute.String("flow.id", string(flow.ID)))
	err := r.next.Create(ctx, flow)
	endRepoSpan(span, err)
	return err
}

func (r *observedFlowRepository) Update(ctx context.Context, flow domain.Flow) error {
	ctx, span := startRepoSpan(ctx, "flow.update", attribute.String("flow.id", string(flow.ID)))
	err := r.next.Update(ctx, flow)
	endRepoSpan(span, err)
	return err
}

func (r *observedFlowRepository) GetByID(ctx context.Context, id domain.FlowID) (domain.Flow, error) {
	ctx, span := startRepoSpan(ctx, "flow.get_by_id", attribute.String("flow.id", string(id)))
	flow, err := r.next.GetByID(ctx, id)
	endRepoSpan(span, err)
	return flow, err
}

func (r *observedFlowRepository) GetVersion(ctx context.Context, id domain.FlowID, version int) (domain.Flow, error) {
	ctx, span := startRepoSpan(ctx, "flow.get_version", attribute.String("flow.id", string(id)), attribute.Int("flow.version", version))
	flow, err := r.next.GetVersion(ctx, id, version)
	endRepoSpan(span, err)
	return flow, err
}

func (r *observedFlowRepository) ListVersions(ctx context.Context, id domain.FlowID) ([]domain.Flow, error) {
	ctx, span := startRepoSpan(ctx, "flow.list_versions", attribute.String("flow.id", string(id)))
	flows, err := r.next.ListVersions(ctx, id)
	endRepoSpan(span, err)
	return flows, err
}

func (r *observedFlowRepository) List(ctx context.Context, filter repository.FlowListFilter) ([]domain.Flow, error) {
	ctx, span := startRepoSpan(ctx, "flow.list")
	flows, err := r.next.List(ctx, filter)
	endRepoSpan(span, err)
	return flows, err
}

func (r *observedFlowStepRepository) CreateMany(ctx context.Context, steps []domain.FlowStep) error {
	ctx, span := startRepoSpan(ctx, "flow_step.create_many", attribute.Int("steps.count", len(steps)))
	err := r.next.CreateMany(ctx, steps)
	endRepoSpan(span, err)
	return err
}

func (r *observedFlowStepRepository) ReplaceByFlowID(ctx context.Context, flowID domain.FlowID, steps []domain.FlowStep) error {
	ctx, span := startRepoSpan(ctx, "flow_step.replace_by_flow_id", attribute.String("flow.id", string(flowID)), attribute.Int("steps.count", len(steps)))
	err := r.next.ReplaceByFlowID(ctx, flowID, steps)
	endRepoSpan(span, err)
	return err
}

func (r *observedFlowStepRepository) ListByFlowID(ctx context.Context, flowID domain.FlowID) ([]domain.FlowStep, error) {
	ctx, span := startRepoSpan(ctx, "flow_step.list_by_flow_id", attribute.String("flow.id", string(flowID)))
	steps, err := r.next.ListByFlowID(ctx, flowID)
	endRepoSpan(span, err)
	return steps, err
}

func (r *observedFlowStepRepository) ReplaceByFlowVersion(ctx context.Context, flowID domain.FlowID, version int, steps []domain.FlowStep) error {
	ctx, span := startRepoSpan(ctx, "flow_step.replace_by_flow_version", attribute.String("flow.id", string(flowID)), attribute.Int("flow.version", version), attribute.Int("steps.count", len(steps)))
	err := r.next.ReplaceByFlowVersion(ctx, flowID, version, steps)
	endRepoSpan(span, err)
	return err
}

func (r *observedFlowStepRepository) ListByFlowVersion(ctx context.Context, flowID domain.FlowID, version int) ([]domain.FlowStep, error) {
	ctx, span := startRepoSpan(ctx, "flow_step.list_by_flow_version", attribute.String("flow.id", string(flowID)), attribute.Int("flow.version", version))
	steps, err := r.next.ListByFlowVersion(ctx, flowID, version)
	endRepoSpan(span, err)
	return steps, err
}

func (r *observedRunRepository) Create(ctx context.Context, run domain.FlowRun) error {
	ctx, span := startRepoSpan(ctx, "run.create", attribute.String("run.id", string(run.ID)), attribute.String("flow.id", string(run.FlowID)))
	err := r.next.Create(ctx, run)
	endRepoSpan(span, err)
	if err == nil && r.metrics != nil {
		r.metrics.TrackRunCreated(run)
	}
	return err
}

func (r *observedRunRepository) UpdateStatus(ctx context.Context, runID domain.RunID, status domain.RunStatus, startedAt, finishedAt *time.Time, errorMessage string) error {
	ctx, span := startRepoSpan(ctx, "run.update_status", attribute.String("run.id", string(runID)), attribute.String("status", string(status)))
	before, _ := r.next.GetByID(ctx, runID)
	err := r.next.UpdateStatus(ctx, runID, status, startedAt, finishedAt, errorMessage)
	endRepoSpan(span, err)
	if err == nil && r.metrics != nil {
		after, getErr := r.next.GetByID(ctx, runID)
		if getErr == nil {
			r.metrics.TrackRunTransition(before, after)
		}
	}
	return err
}

func (r *observedRunRepository) TransitionStatus(ctx context.Context, runID domain.RunID, expected []domain.RunStatus, transition repository.RunStatusTransition) error {
	ctx, span := startRepoSpan(ctx, "run.transition_status", attribute.String("run.id", string(runID)), attribute.String("status", string(transition.Status)))
	before, _ := r.next.GetByID(ctx, runID)
	err := r.next.TransitionStatus(ctx, runID, expected, transition)
	endRepoSpan(span, err)
	if err == nil && r.metrics != nil {
		after, getErr := r.next.GetByID(ctx, runID)
		if getErr == nil {
			r.metrics.TrackRunTransition(before, after)
		}
	}
	return err
}

func (r *observedRunRepository) ClaimForExecution(ctx context.Context, runID domain.RunID, workerID string, now, staleBefore time.Time) (domain.FlowRun, error) {
	ctx, span := startRepoSpan(ctx, "run.claim_for_execution", attribute.String("run.id", string(runID)), attribute.String("worker.id", workerID))
	before, _ := r.next.GetByID(ctx, runID)
	run, err := r.next.ClaimForExecution(ctx, runID, workerID, now, staleBefore)
	endRepoSpan(span, err)
	if err == nil && r.metrics != nil {
		r.metrics.TrackRunTransition(before, run)
	}
	return run, err
}

func (r *observedRunRepository) Heartbeat(ctx context.Context, runID domain.RunID, workerID string, heartbeatAt time.Time) error {
	ctx, span := startRepoSpan(ctx, "run.heartbeat", attribute.String("run.id", string(runID)), attribute.String("worker.id", workerID))
	err := r.next.Heartbeat(ctx, runID, workerID, heartbeatAt)
	endRepoSpan(span, err)
	return err
}

func (r *observedRunRepository) FindByIdempotencyKey(ctx context.Context, workspaceID domain.WorkspaceID, flowID domain.FlowID, idempotencyKey string) (domain.FlowRun, error) {
	ctx, span := startRepoSpan(ctx, "run.find_by_idempotency_key", attribute.String("workspace.id", string(workspaceID)), attribute.String("flow.id", string(flowID)))
	run, err := r.next.FindByIdempotencyKey(ctx, workspaceID, flowID, idempotencyKey)
	endRepoSpan(span, err)
	return run, err
}

func (r *observedRunRepository) GetByID(ctx context.Context, runID domain.RunID) (domain.FlowRun, error) {
	ctx, span := startRepoSpan(ctx, "run.get_by_id", attribute.String("run.id", string(runID)))
	run, err := r.next.GetByID(ctx, runID)
	endRepoSpan(span, err)
	return run, err
}

func (r *observedRunRepository) List(ctx context.Context, filter repository.RunListFilter) ([]domain.FlowRun, error) {
	ctx, span := startRepoSpan(ctx, "run.list")
	runs, err := r.next.List(ctx, filter)
	endRepoSpan(span, err)
	return runs, err
}

func (r *observedRunStepRepository) Create(ctx context.Context, step domain.FlowRunStep) error {
	ctx, span := startRepoSpan(ctx, "run_step.create", attribute.String("run.id", string(step.RunID)), attribute.String("step.name", step.StepName))
	err := r.next.Create(ctx, step)
	endRepoSpan(span, err)
	return err
}

func (r *observedRunStepRepository) Update(ctx context.Context, step domain.FlowRunStep) error {
	ctx, span := startRepoSpan(ctx, "run_step.update", attribute.String("run.id", string(step.RunID)), attribute.String("step.name", step.StepName), attribute.String("status", string(step.Status)))
	err := r.next.Update(ctx, step)
	endRepoSpan(span, err)
	return err
}

func (r *observedRunStepRepository) ListByRunID(ctx context.Context, runID domain.RunID) ([]domain.FlowRunStep, error) {
	ctx, span := startRepoSpan(ctx, "run_step.list_by_run_id", attribute.String("run.id", string(runID)))
	steps, err := r.next.ListByRunID(ctx, runID)
	endRepoSpan(span, err)
	return steps, err
}

func startRepoSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := otel.Tracer("stageflow/repository").Start(ctx, "repository."+name)
	span.SetAttributes(attrs...)
	return ctx, span
}

func endRepoSpan(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}
