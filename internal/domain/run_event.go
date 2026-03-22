package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type RunEventID string

type RunEventType string

type RunEventLevel string

const (
	RunEventTypeRunStarted         RunEventType = "run_started"
	RunEventTypeRunFinished        RunEventType = "run_finished"
	RunEventTypeRunFailed          RunEventType = "run_failed"
	RunEventTypeStepStarted        RunEventType = "step_started"
	RunEventTypeStepFinished       RunEventType = "step_finished"
	RunEventTypeStepFailed         RunEventType = "step_failed"
	RunEventTypeStepRetryScheduled RunEventType = "step_retry_scheduled"
	RunEventTypeStepRetryAttempt   RunEventType = "step_retry_attempt"
	RunEventTypeExtractionDone     RunEventType = "extraction_completed"
	RunEventTypeAssertionsPassed   RunEventType = "assertions_passed"
	RunEventTypeAssertionsFailed   RunEventType = "assertions_failed"
	RunEventTypeTransportError     RunEventType = "transport_error"
	RunEventTypeValidationError    RunEventType = "validation_error"
	RunEventTypePolicyViolation    RunEventType = "policy_violation"
)

const (
	RunEventLevelInfo  RunEventLevel = "info"
	RunEventLevelWarn  RunEventLevel = "warn"
	RunEventLevelError RunEventLevel = "error"
)

type RunEvent struct {
	ID             RunEventID
	Sequence       int64
	RunID          RunID
	WorkspaceID    WorkspaceID
	FlowID         FlowID
	SavedRequestID SavedRequestID
	EventType      RunEventType
	Level          RunEventLevel
	StepName       string
	StepOrder      *int
	Attempt        *int
	Message        string
	DetailsJSON    json.RawMessage
	CreatedAt      time.Time
}

func (e RunEvent) Validate() error {
	if e.RunID == "" {
		return &ValidationError{Message: "run event run_id is required"}
	}
	if e.WorkspaceID == "" {
		return &ValidationError{Message: "run event workspace_id is required"}
	}
	switch e.EventType {
	case RunEventTypeRunStarted, RunEventTypeRunFinished, RunEventTypeRunFailed,
		RunEventTypeStepStarted, RunEventTypeStepFinished, RunEventTypeStepFailed,
		RunEventTypeStepRetryScheduled, RunEventTypeStepRetryAttempt,
		RunEventTypeExtractionDone, RunEventTypeAssertionsPassed, RunEventTypeAssertionsFailed,
		RunEventTypeTransportError, RunEventTypeValidationError, RunEventTypePolicyViolation:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported run event type %q", e.EventType)}
	}
	switch e.Level {
	case RunEventLevelInfo, RunEventLevelWarn, RunEventLevelError:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported run event level %q", e.Level)}
	}
	if strings.TrimSpace(e.Message) == "" {
		return &ValidationError{Message: "run event message is required"}
	}
	if err := ValidateJSONPayload(e.DetailsJSON, "run event details_json"); err != nil {
		return err
	}
	return nil
}
