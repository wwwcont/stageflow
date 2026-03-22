package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

type RunRepository struct {
	db *sql.DB
}

type RunStepRepository struct {
	db *sql.DB
}

func NewRunRepository(db *sql.DB) *RunRepository {
	return &RunRepository{db: db}
}

func NewRunStepRepository(db *sql.DB) *RunStepRepository {
	return &RunStepRepository{db: db}
}

func (r *RunRepository) Create(ctx context.Context, run domain.FlowRun) error {
	run = normalizeRunTimestamps(run)
	if err := run.Validate(); err != nil {
		return err
	}
	const query = `
		INSERT INTO flow_runs (
			id, workspace_id, target_type, flow_id, flow_version, saved_request_id, status, created_at, updated_at, started_at, finished_at,
			input_json, initiated_by, queue_name, idempotency_key, claimed_by, heartbeat_at, error_message
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`
	_, err := r.db.ExecContext(ctx, query,
		run.ID, run.WorkspaceID, run.Target(), nullableString(run.FlowID), nullableInt(run.FlowVersion), nullableString(run.SavedRequestID), run.Status,
		run.CreatedAt, run.UpdatedAt, run.StartedAt, run.FinishedAt,
		[]byte(run.InputJSON), run.InitiatedBy, run.QueueName, run.IdempotencyKey, run.ClaimedBy, run.HeartbeatAt, run.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert run %q: %w", run.ID, mapDBError(err, "flow run"))
	}
	return nil
}

func (r *RunRepository) UpdateStatus(ctx context.Context, runID domain.RunID, status domain.RunStatus, startedAt, finishedAt *time.Time, errorMessage string) error {
	const query = `
		UPDATE flow_runs
		SET status = $2,
		    updated_at = NOW(),
		    started_at = COALESCE($3, started_at),
		    finished_at = COALESCE($4, finished_at),
		    claimed_by = CASE WHEN $2 = 'running' THEN claimed_by ELSE '' END,
		    heartbeat_at = CASE WHEN $2 = 'running' THEN heartbeat_at ELSE NULL END,
		    error_message = $5
		WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, runID, status, startedAt, finishedAt, errorMessage)
	if err != nil {
		return fmt.Errorf("update run status %q: %w", runID, mapDBError(err, "flow run"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	return nil
}

func (r *RunRepository) TransitionStatus(ctx context.Context, runID domain.RunID, expected []domain.RunStatus, transition repository.RunStatusTransition) error {
	query := strings.Builder{}
	query.WriteString(`
		UPDATE flow_runs
		SET status = $2,
		    updated_at = NOW(),
		    started_at = COALESCE($3, started_at),
		    finished_at = COALESCE($4, finished_at),
		    claimed_by = CASE WHEN $2 = 'running' THEN claimed_by ELSE '' END,
		    heartbeat_at = CASE WHEN $2 = 'running' THEN heartbeat_at ELSE NULL END,
		    error_message = $5
		WHERE id = $1`)
	args := []any{runID, transition.Status, transition.StartedAt, transition.FinishedAt, transition.ErrorMessage}
	if len(expected) > 0 {
		query.WriteString(" AND status IN (")
		for idx, status := range expected {
			if idx > 0 {
				query.WriteString(", ")
			}
			args = append(args, status)
			query.WriteString(fmt.Sprintf("$%d", len(args)))
		}
		query.WriteString(")")
	}
	result, err := r.db.ExecContext(ctx, query.String(), args...)
	if err != nil {
		return fmt.Errorf("transition run status %q: %w", runID, mapDBError(err, "flow run"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.ConflictError{Entity: "flow run", Field: "status", Value: string(runID)}
	}
	return nil
}

func (r *RunRepository) ClaimForExecution(ctx context.Context, runID domain.RunID, workerID string, now, staleBefore time.Time) (domain.FlowRun, error) {
	const query = `
		UPDATE flow_runs
		SET status = 'running',
		    updated_at = $3,
		    started_at = COALESCE(started_at, $3),
		    finished_at = NULL,
		    claimed_by = $2,
		    heartbeat_at = $3,
		    error_message = ''
		WHERE id = $1
		  AND (
			status IN ('pending', 'queued')
			OR (status = 'running' AND (claimed_by = '' OR claimed_by = $2 OR heartbeat_at IS NULL OR heartbeat_at <= $4))
		  )
		RETURNING id, workspace_id, target_type, flow_id, flow_version, saved_request_id, status, created_at, updated_at, started_at, finished_at, input_json, initiated_by, queue_name, idempotency_key, claimed_by, heartbeat_at, error_message`
	var (
		run domain.FlowRun
	)
	if err := scanFlowRunRow(r.db.QueryRowContext(ctx, query, runID, workerID, now.UTC(), staleBefore.UTC()), &run); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			existing, getErr := r.GetByID(ctx, runID)
			if getErr != nil {
				return domain.FlowRun{}, getErr
			}
			if existing.IsTerminal() {
				return existing, nil
			}
			return domain.FlowRun{}, &domain.ConflictError{Entity: "flow run", Field: "claimed_by", Value: existing.ClaimedBy}
		}
		return domain.FlowRun{}, fmt.Errorf("claim run %q: %w", runID, mapDBError(err, "flow run"))
	}
	return run, nil
}

func (r *RunRepository) Heartbeat(ctx context.Context, runID domain.RunID, workerID string, heartbeatAt time.Time) error {
	const query = `
		UPDATE flow_runs
		SET heartbeat_at = $3,
		    updated_at = $3
		WHERE id = $1
		  AND status = 'running'
		  AND claimed_by = $2`
	result, err := r.db.ExecContext(ctx, query, runID, workerID, heartbeatAt.UTC())
	if err != nil {
		return fmt.Errorf("heartbeat run %q: %w", runID, mapDBError(err, "flow run"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.ConflictError{Entity: "flow run", Field: "claimed_by", Value: workerID}
	}
	return nil
}

func (r *RunRepository) FindByIdempotencyKey(ctx context.Context, workspaceID domain.WorkspaceID, flowID domain.FlowID, idempotencyKey string) (domain.FlowRun, error) {
	const query = `
		SELECT id, workspace_id, target_type, flow_id, flow_version, saved_request_id, status, created_at, updated_at, started_at, finished_at, input_json, initiated_by, queue_name, idempotency_key, claimed_by, heartbeat_at, error_message
		FROM flow_runs
		WHERE workspace_id = $1 AND target_type = 'flow' AND flow_id = $2 AND idempotency_key = $3
		ORDER BY created_at DESC
		LIMIT 1`
	row := r.db.QueryRowContext(ctx, query, workspaceID, flowID, idempotencyKey)
	var run domain.FlowRun
	if err := scanFlowRunRow(row, &run); err != nil {
		return domain.FlowRun{}, fmt.Errorf("select run by idempotency key %q: %w", idempotencyKey, mapDBError(err, "flow run"))
	}
	return run, nil
}

func (r *RunRepository) GetByID(ctx context.Context, runID domain.RunID) (domain.FlowRun, error) {
	const query = `
		SELECT id, workspace_id, target_type, flow_id, flow_version, saved_request_id, status, created_at, updated_at, started_at, finished_at, input_json, initiated_by, queue_name, idempotency_key, claimed_by, heartbeat_at, error_message
		FROM flow_runs
		WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, runID)
	var run domain.FlowRun
	if err := scanFlowRunRow(row, &run); err != nil {
		return domain.FlowRun{}, fmt.Errorf("select run %q: %w", runID, mapDBError(err, "flow run"))
	}
	return run, nil
}

func (r *RunRepository) List(ctx context.Context, filter repository.RunListFilter) ([]domain.FlowRun, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, workspace_id, target_type, flow_id, flow_version, saved_request_id, status, created_at, updated_at, started_at, finished_at, input_json, initiated_by, queue_name, idempotency_key, claimed_by, heartbeat_at, error_message
		FROM flow_runs
		WHERE 1=1`)
	args := make([]any, 0, 5)
	if filter.WorkspaceID != "" {
		args = append(args, filter.WorkspaceID)
		query.WriteString(fmt.Sprintf(" AND workspace_id = $%d", len(args)))
	}
	if filter.FlowID != "" {
		args = append(args, filter.FlowID)
		query.WriteString(fmt.Sprintf(" AND flow_id = $%d", len(args)))
	}
	if filter.InitiatedBy != "" {
		args = append(args, filter.InitiatedBy)
		query.WriteString(fmt.Sprintf(" AND initiated_by = $%d", len(args)))
	}
	if len(filter.Statuses) > 0 {
		query.WriteString(" AND status IN (")
		for idx, status := range filter.Statuses {
			if idx > 0 {
				query.WriteString(", ")
			}
			args = append(args, string(status))
			query.WriteString(fmt.Sprintf("$%d", len(args)))
		}
		query.WriteString(")")
	}
	query.WriteString(" ORDER BY created_at DESC")
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query.WriteString(fmt.Sprintf(" LIMIT $%d", len(args)))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query.WriteString(fmt.Sprintf(" OFFSET $%d", len(args)))
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	runs := make([]domain.FlowRun, 0)
	for rows.Next() {
		var run domain.FlowRun
		if err := scanFlowRunRow(rows, &run); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

func scanFlowRunRow(row scanner, run *domain.FlowRun) error {
	var (
		flowID         sql.NullString
		flowVersion    sql.NullInt64
		savedRequestID sql.NullString
		inputRaw       []byte
	)
	if err := row.Scan(
		&run.ID, &run.WorkspaceID, &run.TargetType, &flowID, &flowVersion, &savedRequestID, &run.Status,
		&run.CreatedAt, &run.UpdatedAt, &run.StartedAt, &run.FinishedAt, &inputRaw, &run.InitiatedBy, &run.QueueName,
		&run.IdempotencyKey, &run.ClaimedBy, &run.HeartbeatAt, &run.ErrorMessage,
	); err != nil {
		return err
	}
	run.FlowID = domain.FlowID(flowID.String)
	if flowVersion.Valid {
		run.FlowVersion = int(flowVersion.Int64)
	}
	run.SavedRequestID = domain.SavedRequestID(savedRequestID.String)
	run.InputJSON = cloneBytes(inputRaw)
	return nil
}

func (r *RunStepRepository) Create(ctx context.Context, step domain.FlowRunStep) error {
	step = normalizeRunStepTimestamps(step)
	if err := step.Validate(); err != nil {
		return err
	}
	const query = `
		INSERT INTO flow_run_steps (
			id, run_id, step_name, step_order, status,
			request_snapshot_json, response_snapshot_json, extracted_values_json, attempt_history_json,
			retry_count, created_at, updated_at, started_at, finished_at, error_message
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	_, err := r.db.ExecContext(ctx, query,
		step.ID, step.RunID, step.StepName, step.StepOrder, step.Status,
		nullableJSON(step.RequestSnapshotJSON), nullableJSON(step.ResponseSnapshotJSON), nullableJSON(step.ExtractedValuesJSON), nullableJSON(step.AttemptHistoryJSON),
		step.RetryCount, step.CreatedAt, step.UpdatedAt, step.StartedAt, step.FinishedAt, step.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("insert run step %q: %w", step.ID, mapDBError(err, "flow run step"))
	}
	return nil
}

func (r *RunStepRepository) Update(ctx context.Context, step domain.FlowRunStep) error {
	step = normalizeRunStepTimestamps(step)
	if err := step.Validate(); err != nil {
		return err
	}
	const query = `
		UPDATE flow_run_steps
		SET step_name = $2,
		    step_order = $3,
		    status = $4,
		    request_snapshot_json = $5,
		    response_snapshot_json = $6,
		    extracted_values_json = $7,
		    attempt_history_json = $8,
		    retry_count = $9,
		    updated_at = $10,
		    started_at = $11,
		    finished_at = $12,
		    error_message = $13
		WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query,
		step.ID, step.StepName, step.StepOrder, step.Status,
		nullableJSON(step.RequestSnapshotJSON), nullableJSON(step.ResponseSnapshotJSON), nullableJSON(step.ExtractedValuesJSON), nullableJSON(step.AttemptHistoryJSON),
		step.RetryCount, time.Now().UTC(), step.StartedAt, step.FinishedAt, step.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("update run step %q: %w", step.ID, mapDBError(err, "flow run step"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.NotFoundError{Entity: "flow run step", ID: string(step.ID)}
	}
	return nil
}

func (r *RunStepRepository) ListByRunID(ctx context.Context, runID domain.RunID) ([]domain.FlowRunStep, error) {
	const query = `
		SELECT id, run_id, step_name, step_order, status,
		       request_snapshot_json, response_snapshot_json, extracted_values_json, attempt_history_json,
		       retry_count, created_at, updated_at, started_at, finished_at, error_message
		FROM flow_run_steps
		WHERE run_id = $1
		ORDER BY step_order ASC`
	rows, err := r.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("select run steps for %q: %w", runID, err)
	}
	defer rows.Close()

	steps := make([]domain.FlowRunStep, 0)
	for rows.Next() {
		var (
			step         domain.FlowRunStep
			requestRaw   []byte
			responseRaw  []byte
			extractedRaw []byte
			attemptsRaw  []byte
		)
		if err := rows.Scan(
			&step.ID, &step.RunID, &step.StepName, &step.StepOrder, &step.Status,
			&requestRaw, &responseRaw, &extractedRaw, &attemptsRaw,
			&step.RetryCount, &step.CreatedAt, &step.UpdatedAt, &step.StartedAt, &step.FinishedAt, &step.ErrorMessage,
		); err != nil {
			return nil, fmt.Errorf("scan run step: %w", err)
		}
		step.RequestSnapshotJSON = cloneBytes(requestRaw)
		step.ResponseSnapshotJSON = cloneBytes(responseRaw)
		step.ExtractedValuesJSON = cloneBytes(extractedRaw)
		step.AttemptHistoryJSON = cloneBytes(attemptsRaw)
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run steps: %w", err)
	}
	return steps, nil
}

func nullableJSON(payload []byte) any {
	if len(payload) == 0 {
		return nil
	}
	return payload
}

func cloneBytes(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	cloned := make([]byte, len(payload))
	copy(cloned, payload)
	return cloned
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

var _ repository.RunRepository = (*RunRepository)(nil)
var _ repository.RunStepRepository = (*RunStepRepository)(nil)
var _ = sql.ErrNoRows
