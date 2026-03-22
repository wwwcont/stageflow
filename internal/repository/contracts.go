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

// WorkspaceRepository отвечает за верхнеуровневые границы изоляции для флоу, запросов, политик и истории запусков.
type WorkspaceRepository interface {
	Create(ctx context.Context, workspace domain.Workspace) error
	Update(ctx context.Context, workspace domain.Workspace) error
	GetByID(ctx context.Context, id domain.WorkspaceID) (domain.Workspace, error)
	GetBySlug(ctx context.Context, slug string) (domain.Workspace, error)
	List(ctx context.Context, filter WorkspaceListFilter) ([]domain.Workspace, error)
}

// SavedRequestRepository хранит отдельные переиспользуемые определения HTTP-запросов в границах рабочего пространства.
type SavedRequestRepository interface {
	Create(ctx context.Context, request domain.SavedRequest) error
	Update(ctx context.Context, request domain.SavedRequest) error
	GetByID(ctx context.Context, id domain.SavedRequestID) (domain.SavedRequest, error)
	List(ctx context.Context, filter SavedRequestListFilter) ([]domain.SavedRequest, error)
}

// WorkspaceVariableRepository сохраняет неприватные пары ключ/значение уровня рабочего пространства отдельно от его метаданных.
type WorkspaceVariableRepository interface {
	ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, variables []domain.WorkspaceVariable) error
	ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceVariable, error)
}

// WorkspaceSecretRepository сохраняет и извлекает секреты рабочего пространства через изолированный контракт хранилища.
type WorkspaceSecretRepository interface {
	ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, secrets []domain.WorkspaceSecret) error
	ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceSecret, error)
}

// FlowRepository отвечает за жизненный цикл флоу и метаданные их текущих версий.
type FlowRepository interface {
	Create(ctx context.Context, flow domain.Flow) error
	Update(ctx context.Context, flow domain.Flow) error
	GetByID(ctx context.Context, id domain.FlowID) (domain.Flow, error)
	List(ctx context.Context, filter FlowListFilter) ([]domain.Flow, error)
}

// FlowStepRepository хранит версионируемые определения шагов флоу отдельно от метаданных самих флоу.
type FlowStepRepository interface {
	CreateMany(ctx context.Context, steps []domain.FlowStep) error
	ReplaceByFlowID(ctx context.Context, flowID domain.FlowID, steps []domain.FlowStep) error
	ListByFlowID(ctx context.Context, flowID domain.FlowID) ([]domain.FlowStep, error)
}

// RunRepository сохраняет переходы состояний запуска и связанные с запуском метаданные.
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

// RunStepRepository сохраняет снимки выполнения для каждого шага.
type RunStepRepository interface {
	Create(ctx context.Context, step domain.FlowRunStep) error
	Update(ctx context.Context, step domain.FlowRunStep) error
	ListByRunID(ctx context.Context, runID domain.RunID) ([]domain.FlowRunStep, error)
}

// RunEventRepository сохраняет лёгкие события выполнения для live-отладки и истории запусков.
type RunEventRepository interface {
	Append(ctx context.Context, event domain.RunEvent) (domain.RunEvent, error)
	ListByRunID(ctx context.Context, runID domain.RunID, afterSequence int64) ([]domain.RunEvent, error)
}
