package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

type RunID string

type RunStepID string

type RunStatus string

type RunStepStatus string

type RunTargetType string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusQueued    RunStatus = "queued"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

const (
	RunStepStatusPending   RunStepStatus = "pending"
	RunStepStatusRunning   RunStepStatus = "running"
	RunStepStatusSucceeded RunStepStatus = "succeeded"
	RunStepStatusFailed    RunStepStatus = "failed"
	RunStepStatusSkipped   RunStepStatus = "skipped"
	RunStepStatusCanceled  RunStepStatus = "canceled"
)

const (
	RunTargetTypeFlow         RunTargetType = "flow"
	RunTargetTypeSavedRequest RunTargetType = "saved_request"
)

type FlowRun struct {
	ID             RunID
	WorkspaceID    WorkspaceID
	TargetType     RunTargetType
	FlowID         FlowID
	FlowVersion    int
	SavedRequestID SavedRequestID
	Status         RunStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	InputJSON      json.RawMessage
	InitiatedBy    string
	QueueName      string
	IdempotencyKey string
	ClaimedBy      string
	HeartbeatAt    *time.Time
	ErrorMessage   string
}

func (r FlowRun) Validate() error {
	if r.ID == "" {
		return &ValidationError{Message: "flow run id is required"}
	}
	if r.WorkspaceID == "" {
		return &ValidationError{Message: "flow run workspace_id is required"}
	}
	switch r.Target() {
	case RunTargetTypeFlow:
		if r.FlowID == "" {
			return &ValidationError{Message: "flow run flow_id is required"}
		}
		if r.FlowVersion < 1 {
			return &ValidationError{Message: "flow run flow_version must be >= 1"}
		}
		if r.SavedRequestID != "" {
			return &ValidationError{Message: "flow run saved_request_id must be empty for flow target"}
		}
	case RunTargetTypeSavedRequest:
		if r.SavedRequestID == "" {
			return &ValidationError{Message: "flow run saved_request_id is required"}
		}
		if r.FlowID != "" || r.FlowVersion != 0 {
			return &ValidationError{Message: "flow run flow fields must be empty for saved request target"}
		}
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported flow run target_type %q", r.TargetType)}
	}
	switch r.Status {
	case RunStatusPending, RunStatusQueued, RunStatusRunning, RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported flow run status %q", r.Status)}
	}
	if err := ValidateJSONPayload(r.InputJSON, "flow run input_json"); err != nil {
		return err
	}
	if !r.CreatedAt.IsZero() && !r.UpdatedAt.IsZero() && r.UpdatedAt.Before(r.CreatedAt) {
		return &ValidationError{Message: "flow run updated_at must not be before created_at"}
	}
	if r.StartedAt != nil && r.FinishedAt != nil && r.FinishedAt.Before(*r.StartedAt) {
		return &ValidationError{Message: "flow run finished_at must not be before started_at"}
	}
	if r.HeartbeatAt != nil && r.StartedAt == nil {
		return &ValidationError{Message: "flow run heartbeat_at requires started_at"}
	}
	if r.InitiatedBy == "" {
		return &ValidationError{Message: "flow run initiated_by is required"}
	}
	return nil
}

func (r FlowRun) Target() RunTargetType {
	switch {
	case r.TargetType != "":
		return r.TargetType
	case r.SavedRequestID != "" && r.FlowID == "":
		return RunTargetTypeSavedRequest
	default:
		return RunTargetTypeFlow
	}
}

func (r FlowRun) IsFlowRun() bool {
	return r.Target() == RunTargetTypeFlow
}

func (r FlowRun) IsSavedRequestRun() bool {
	return r.Target() == RunTargetTypeSavedRequest
}

func (r FlowRun) IsTerminal() bool {
	switch r.Status {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

func (r FlowRun) MarkRunning(now time.Time) (FlowRun, error) {
	switch r.Status {
	case RunStatusPending, RunStatusQueued:
		r.Status = RunStatusRunning
		if r.StartedAt == nil {
			startedAt := now.UTC()
			r.StartedAt = &startedAt
		}
		r.FinishedAt = nil
		r.ErrorMessage = ""
		r.ClaimedBy = ""
		r.HeartbeatAt = nil
		r.UpdatedAt = now.UTC()
		return r, nil
	case RunStatusRunning:
		return r, nil
	default:
		return r, &ValidationError{Message: fmt.Sprintf("cannot transition run from %q to %q", r.Status, RunStatusRunning)}
	}
}

func (r FlowRun) MarkSucceeded(now time.Time) (FlowRun, error) {
	if r.Status != RunStatusRunning {
		return r, &ValidationError{Message: fmt.Sprintf("cannot transition run from %q to %q", r.Status, RunStatusSucceeded)}
	}
	if r.StartedAt == nil {
		startedAt := now.UTC()
		r.StartedAt = &startedAt
	}
	finishedAt := now.UTC()
	r.Status = RunStatusSucceeded
	r.FinishedAt = &finishedAt
	r.UpdatedAt = finishedAt
	r.ClaimedBy = ""
	r.HeartbeatAt = nil
	r.ErrorMessage = ""
	return r, nil
}

func (r FlowRun) MarkFailed(now time.Time, message string) (FlowRun, error) {
	switch r.Status {
	case RunStatusRunning, RunStatusPending, RunStatusQueued:
		if r.StartedAt == nil {
			startedAt := now.UTC()
			r.StartedAt = &startedAt
		}
		finishedAt := now.UTC()
		r.Status = RunStatusFailed
		r.FinishedAt = &finishedAt
		r.UpdatedAt = finishedAt
		r.ClaimedBy = ""
		r.HeartbeatAt = nil
		r.ErrorMessage = message
		return r, nil
	default:
		return r, &ValidationError{Message: fmt.Sprintf("cannot transition run from %q to %q", r.Status, RunStatusFailed)}
	}
}

func (r FlowRun) Claim(workerID string, now time.Time) FlowRun {
	r.Status = RunStatusRunning
	if r.StartedAt == nil {
		startedAt := now.UTC()
		r.StartedAt = &startedAt
	}
	heartbeatAt := now.UTC()
	r.HeartbeatAt = &heartbeatAt
	r.ClaimedBy = workerID
	r.FinishedAt = nil
	r.UpdatedAt = now.UTC()
	r.ErrorMessage = ""
	return r
}

type FlowRunStep struct {
	ID                   RunStepID
	RunID                RunID
	StepName             string
	StepOrder            int
	Status               RunStepStatus
	RequestSnapshotJSON  json.RawMessage
	ResponseSnapshotJSON json.RawMessage
	ExtractedValuesJSON  json.RawMessage
	AttemptHistoryJSON   json.RawMessage
	RetryCount           int
	CreatedAt            time.Time
	UpdatedAt            time.Time
	StartedAt            *time.Time
	FinishedAt           *time.Time
	ErrorMessage         string
}

func (s FlowRunStep) Validate() error {
	if s.ID == "" {
		return &ValidationError{Message: "flow run step id is required"}
	}
	if s.RunID == "" {
		return &ValidationError{Message: "flow run step run_id is required"}
	}
	if s.StepName == "" {
		return &ValidationError{Message: "flow run step step_name is required"}
	}
	if s.StepOrder < 0 {
		return &ValidationError{Message: "flow run step step_order must be >= 0"}
	}
	switch s.Status {
	case RunStepStatusPending, RunStepStatusRunning, RunStepStatusSucceeded, RunStepStatusFailed, RunStepStatusSkipped, RunStepStatusCanceled:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported flow run step status %q", s.Status)}
	}
	if err := ValidateJSONPayload(s.RequestSnapshotJSON, "flow run step request_snapshot_json"); err != nil {
		return err
	}
	if err := ValidateJSONPayload(s.ResponseSnapshotJSON, "flow run step response_snapshot_json"); err != nil {
		return err
	}
	if err := ValidateJSONPayload(s.ExtractedValuesJSON, "flow run step extracted_values_json"); err != nil {
		return err
	}
	if err := ValidateJSONPayload(s.AttemptHistoryJSON, "flow run step attempt_history_json"); err != nil {
		return err
	}
	if !s.CreatedAt.IsZero() && !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(s.CreatedAt) {
		return &ValidationError{Message: "flow run step updated_at must not be before created_at"}
	}
	if s.RetryCount < 0 {
		return &ValidationError{Message: "flow run step retry_count must be >= 0"}
	}
	if s.StartedAt != nil && s.FinishedAt != nil && s.FinishedAt.Before(*s.StartedAt) {
		return &ValidationError{Message: "flow run step finished_at must not be before started_at"}
	}
	return nil
}

func (s FlowRunStep) MarkRunning(now time.Time) FlowRunStep {
	startedAt := now.UTC()
	if s.StartedAt != nil {
		startedAt = s.StartedAt.UTC()
	}
	s.Status = RunStepStatusRunning
	if s.StartedAt == nil {
		s.StartedAt = &startedAt
	}
	s.FinishedAt = nil
	s.UpdatedAt = now.UTC()
	s.ErrorMessage = ""
	return s
}

func (s FlowRunStep) MarkSucceeded(now time.Time) FlowRunStep {
	if s.StartedAt == nil {
		startedAt := now.UTC()
		s.StartedAt = &startedAt
	}
	finishedAt := now.UTC()
	s.Status = RunStepStatusSucceeded
	s.FinishedAt = &finishedAt
	s.UpdatedAt = finishedAt
	s.ErrorMessage = ""
	return s
}

func (s FlowRunStep) MarkFailed(now time.Time, message string) FlowRunStep {
	if s.StartedAt == nil {
		startedAt := now.UTC()
		s.StartedAt = &startedAt
	}
	finishedAt := now.UTC()
	s.Status = RunStepStatusFailed
	s.FinishedAt = &finishedAt
	s.UpdatedAt = finishedAt
	s.ErrorMessage = message
	return s
}
