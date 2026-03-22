package repository

import (
	"context"
	"time"

	"stageflow/internal/domain"
)

type FlowListFilter struct {
	WorkspaceID domain.WorkspaceID
	Statuses    []domain.FlowStatus
	NameLike    string
	Limit       int
	Offset      int
}

type WorkspaceListFilter struct {
	Statuses []domain.WorkspaceStatus
	NameLike string
	Limit    int
	Offset   int
}

type SavedRequestListFilter struct {
	WorkspaceID domain.WorkspaceID
	NameLike    string
	Limit       int
	Offset      int
}

type RunListFilter struct {
	WorkspaceID domain.WorkspaceID
	FlowID      domain.FlowID
	Statuses    []domain.RunStatus
	InitiatedBy string
	Limit       int
	Offset      int
}

type RunStatusTransition struct {
	Status       domain.RunStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	ErrorMessage string
}

// WorkspaceRepository owns top-level isolation boundaries for flows, requests, policies, and run history.
type WorkspaceRepository interface {
	Create(ctx context.Context, workspace domain.Workspace) error
	Update(ctx context.Context, workspace domain.Workspace) error
	GetByID(ctx context.Context, id domain.WorkspaceID) (domain.Workspace, error)
	GetBySlug(ctx context.Context, slug string) (domain.Workspace, error)
	List(ctx context.Context, filter WorkspaceListFilter) ([]domain.Workspace, error)
}

// SavedRequestRepository stores single reusable HTTP request definitions within a workspace boundary.
type SavedRequestRepository interface {
	Create(ctx context.Context, request domain.SavedRequest) error
	Update(ctx context.Context, request domain.SavedRequest) error
	GetByID(ctx context.Context, id domain.SavedRequestID) (domain.SavedRequest, error)
	List(ctx context.Context, filter SavedRequestListFilter) ([]domain.SavedRequest, error)
}

// WorkspaceVariableRepository persists workspace-scoped non-secret key/value pairs separately from workspace metadata.
type WorkspaceVariableRepository interface {
	ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, variables []domain.WorkspaceVariable) error
	ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceVariable, error)
}

// WorkspaceSecretRepository persists or resolves workspace-scoped secrets behind an isolated storage contract.
type WorkspaceSecretRepository interface {
	ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, secrets []domain.WorkspaceSecret) error
	ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceSecret, error)
}

// FlowRepository owns lifecycle and current-version metadata of flows.
type FlowRepository interface {
	Create(ctx context.Context, flow domain.Flow) error
	Update(ctx context.Context, flow domain.Flow) error
	GetByID(ctx context.Context, id domain.FlowID) (domain.Flow, error)
	List(ctx context.Context, filter FlowListFilter) ([]domain.Flow, error)
}

// FlowStepRepository stores versioned flow step definitions independently from flow metadata.
type FlowStepRepository interface {
	CreateMany(ctx context.Context, steps []domain.FlowStep) error
	ReplaceByFlowID(ctx context.Context, flowID domain.FlowID, steps []domain.FlowStep) error
	ListByFlowID(ctx context.Context, flowID domain.FlowID) ([]domain.FlowStep, error)
}

// RunRepository persists run-level state transitions and metadata.
type RunRepository interface {
	Create(ctx context.Context, run domain.FlowRun) error
	UpdateStatus(ctx context.Context, runID domain.RunID, status domain.RunStatus, startedAt, finishedAt *time.Time, errorMessage string) error
	TransitionStatus(ctx context.Context, runID domain.RunID, expected []domain.RunStatus, transition RunStatusTransition) error
	ClaimForExecution(ctx context.Context, runID domain.RunID, workerID string, now, staleBefore time.Time) (domain.FlowRun, error)
	Heartbeat(ctx context.Context, runID domain.RunID, workerID string, heartbeatAt time.Time) error
	FindByIdempotencyKey(ctx context.Context, workspaceID domain.WorkspaceID, flowID domain.FlowID, idempotencyKey string) (domain.FlowRun, error)
	GetByID(ctx context.Context, runID domain.RunID) (domain.FlowRun, error)
	List(ctx context.Context, filter RunListFilter) ([]domain.FlowRun, error)
}

// RunStepRepository persists per-step execution snapshots.
type RunStepRepository interface {
	Create(ctx context.Context, step domain.FlowRunStep) error
	Update(ctx context.Context, step domain.FlowRunStep) error
	ListByRunID(ctx context.Context, runID domain.RunID) ([]domain.FlowRunStep, error)
}

// RunEventRepository persists lightweight execution events used for live debugging and run history.
type RunEventRepository interface {
	Append(ctx context.Context, event domain.RunEvent) (domain.RunEvent, error)
	ListByRunID(ctx context.Context, runID domain.RunID, afterSequence int64) ([]domain.RunEvent, error)
}
