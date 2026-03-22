package domain

import (
	"fmt"
	"time"
)

type FlowID string

type FlowStepID string

type FlowStepType string

type FlowStatus string

const (
	FlowStatusDraft    FlowStatus = "draft"
	FlowStatusActive   FlowStatus = "active"
	FlowStatusDisabled FlowStatus = "disabled"
	FlowStatusArchived FlowStatus = "archived"
)

const (
	FlowStepTypeInlineRequest   FlowStepType = "inline_request"
	FlowStepTypeSavedRequestRef FlowStepType = "saved_request_ref"
)

type Flow struct {
	WorkspaceID WorkspaceID
	ID          FlowID
	Name        string
	Description string
	Version     int
	Status      FlowStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (f Flow) Validate() error {
	if f.WorkspaceID == "" {
		return &ValidationError{Message: "flow workspace_id is required"}
	}
	if f.ID == "" {
		return &ValidationError{Message: "flow id is required"}
	}
	if f.Name == "" {
		return &ValidationError{Message: "flow name is required"}
	}
	if f.Version < 1 {
		return &ValidationError{Message: "flow version must be >= 1"}
	}
	switch f.Status {
	case FlowStatusDraft, FlowStatusActive, FlowStatusDisabled, FlowStatusArchived:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported flow status %q", f.Status)}
	}
	if !f.CreatedAt.IsZero() && !f.UpdatedAt.IsZero() && f.UpdatedAt.Before(f.CreatedAt) {
		return &ValidationError{Message: "flow updated_at must not be before created_at"}
	}
	return nil
}

type FlowStep struct {
	ID                  FlowStepID
	FlowID              FlowID
	OrderIndex          int
	Name                string
	StepType            FlowStepType
	SavedRequestID      SavedRequestID
	RequestSpec         RequestSpec
	RequestSpecOverride RequestSpecOverride
	ExtractionSpec      ExtractionSpec
	AssertionSpec       AssertionSpec
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (s FlowStep) Validate() error {
	if s.ID == "" {
		return &ValidationError{Message: "flow step id is required"}
	}
	if s.FlowID == "" {
		return &ValidationError{Message: "flow step flow_id is required"}
	}
	if s.OrderIndex < 0 {
		return &ValidationError{Message: "flow step order_index must be >= 0"}
	}
	if s.Name == "" {
		return &ValidationError{Message: "flow step name is required"}
	}
	switch s.Type() {
	case FlowStepTypeInlineRequest:
		if err := s.RequestSpec.Validate(); err != nil {
			return err
		}
		if s.SavedRequestID != "" {
			return &ValidationError{Message: "flow step saved_request_id must be empty for inline_request"}
		}
		if !s.RequestSpecOverride.IsZero() {
			return &ValidationError{Message: "flow step request_spec_override must be empty for inline_request"}
		}
	case FlowStepTypeSavedRequestRef:
		if s.SavedRequestID == "" {
			return &ValidationError{Message: "flow step saved_request_id is required for saved_request_ref"}
		}
		if !s.isZeroRequestSpec() {
			return &ValidationError{Message: "flow step request_spec must be empty for saved_request_ref"}
		}
		if err := s.RequestSpecOverride.Validate(); err != nil {
			return err
		}
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported flow step type %q", s.StepType)}
	}
	if err := s.ExtractionSpec.Validate(); err != nil {
		return err
	}
	if err := s.AssertionSpec.Validate(); err != nil {
		return err
	}
	if !s.CreatedAt.IsZero() && !s.UpdatedAt.IsZero() && s.UpdatedAt.Before(s.CreatedAt) {
		return &ValidationError{Message: "flow step updated_at must not be before created_at"}
	}
	return nil
}

func (s FlowStep) Type() FlowStepType {
	switch {
	case s.StepType != "":
		return s.StepType
	case s.SavedRequestID != "":
		return FlowStepTypeSavedRequestRef
	default:
		return FlowStepTypeInlineRequest
	}
}

func (s FlowStep) isZeroRequestSpec() bool {
	return s.RequestSpec.Method == "" &&
		s.RequestSpec.URLTemplate == "" &&
		len(s.RequestSpec.Headers) == 0 &&
		len(s.RequestSpec.Query) == 0 &&
		s.RequestSpec.BodyTemplate == "" &&
		s.RequestSpec.Timeout == 0 &&
		!s.RequestSpec.FollowRedirects &&
		!s.RequestSpec.RetryPolicy.Enabled &&
		s.RequestSpec.RetryPolicy.MaxAttempts == 0 &&
		s.RequestSpec.RetryPolicy.BackoffStrategy == "" &&
		s.RequestSpec.RetryPolicy.InitialInterval == 0 &&
		s.RequestSpec.RetryPolicy.MaxInterval == 0 &&
		len(s.RequestSpec.RetryPolicy.RetryableStatusCodes) == 0
}
