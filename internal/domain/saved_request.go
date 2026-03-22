package domain

import (
	"strings"
	"time"
)

type SavedRequestID string

type SavedRequest struct {
	ID          SavedRequestID
	WorkspaceID WorkspaceID
	Name        string
	Description string
	RequestSpec RequestSpec
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r SavedRequest) Normalized() SavedRequest {
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	return r
}

func (r SavedRequest) Validate() error {
	normalized := r.Normalized()
	if normalized.ID == "" {
		return &ValidationError{Message: "saved request id is required"}
	}
	if normalized.WorkspaceID == "" {
		return &ValidationError{Message: "saved request workspace_id is required"}
	}
	if normalized.Name == "" {
		return &ValidationError{Message: "saved request name is required"}
	}
	if err := normalized.RequestSpec.Validate(); err != nil {
		return err
	}
	if !normalized.CreatedAt.IsZero() && !normalized.UpdatedAt.IsZero() && normalized.UpdatedAt.Before(normalized.CreatedAt) {
		return &ValidationError{Message: "saved request updated_at must not be before created_at"}
	}
	return nil
}
