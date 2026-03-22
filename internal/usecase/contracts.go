package usecase

import (
	"context"
	"encoding/json"

	"stageflow/internal/domain"
)

type FlowStepDraft struct {
	ID                  domain.FlowStepID
	OrderIndex          int
	Name                string
	StepType            domain.FlowStepType
	SavedRequestID      domain.SavedRequestID
	RequestSpec         domain.RequestSpec
	RequestSpecOverride domain.RequestSpecOverride
	ExtractionSpec      domain.ExtractionSpec
	AssertionSpec       domain.AssertionSpec
}

type CreateSavedRequestCommand struct {
	WorkspaceID    domain.WorkspaceID
	SavedRequestID domain.SavedRequestID
	Name           string
	Description    string
	RequestSpec    domain.RequestSpec
}

type UpdateSavedRequestCommand struct {
	WorkspaceID    domain.WorkspaceID
	SavedRequestID domain.SavedRequestID
	Name           string
	Description    string
	RequestSpec    domain.RequestSpec
}

type GetSavedRequestQuery struct {
	WorkspaceID    domain.WorkspaceID
	SavedRequestID domain.SavedRequestID
}

type ListSavedRequestsQuery struct {
	WorkspaceID domain.WorkspaceID
	NameLike    string
	Limit       int
	Offset      int
}

type CreateFlowCommand struct {
	WorkspaceID domain.WorkspaceID
	FlowID      domain.FlowID
	Name        string
	Description string
	Status      domain.FlowStatus
	Steps       []FlowStepDraft
}

type UpdateFlowCommand struct {
	WorkspaceID domain.WorkspaceID
	FlowID      domain.FlowID
	Name        string
	Description string
	Status      domain.FlowStatus
	Steps       []FlowStepDraft
}

type ValidateFlowCommand struct {
	WorkspaceID domain.WorkspaceID
	FlowID      domain.FlowID
	Name        string
	Description string
	Status      domain.FlowStatus
	Steps       []FlowStepDraft
}

type GetFlowQuery struct {
	WorkspaceID domain.WorkspaceID
	FlowID      domain.FlowID
	Version     int
}

type ListFlowsQuery struct {
	WorkspaceID domain.WorkspaceID
	Statuses    []domain.FlowStatus
	NameLike    string
	Limit       int
	Offset      int
}

type FlowDefinitionView struct {
	Flow  domain.Flow
	Steps []domain.FlowStep
}

type FlowValidationResult struct {
	Valid  bool
	Issues []string
}

type ImportCurlInput struct {
	Command string
}

type ImportCurlResult struct {
	RequestSpec domain.RequestSpec
}

type SavedRequestView struct {
	SavedRequest domain.SavedRequest
}

type LaunchFlowInput struct {
	WorkspaceID    domain.WorkspaceID
	FlowID         domain.FlowID
	InitiatedBy    string
	InputJSON      json.RawMessage
	Queue          string
	IdempotencyKey string
}

type LaunchSavedRequestInput struct {
	WorkspaceID    domain.WorkspaceID
	SavedRequestID domain.SavedRequestID
	InitiatedBy    string
	InputJSON      json.RawMessage
	Queue          string
	IdempotencyKey string
}

type RunStatusView struct {
	Run   domain.FlowRun
	Steps []domain.FlowRunStep
}

type GetRunStatusQuery struct {
	WorkspaceID domain.WorkspaceID
	RunID       domain.RunID
}

type ListRunsQuery struct {
	WorkspaceID domain.WorkspaceID
	Statuses    []domain.RunStatus
	Limit       int
	Offset      int
}

type RerunInput struct {
	WorkspaceID    domain.WorkspaceID
	RunID          domain.RunID
	InitiatedBy    string
	OverrideJSON   json.RawMessage
	Queue          string
	IdempotencyKey string
}

type CreateWorkspaceCommand struct {
	WorkspaceID domain.WorkspaceID
	Name        string
	Slug        string
	Description string
	OwnerTeam   string
	Status      domain.WorkspaceStatus
	Policy      domain.WorkspacePolicy
}

type UpdateWorkspaceCommand struct {
	WorkspaceID domain.WorkspaceID
	Name        string
	Slug        string
	Description string
	OwnerTeam   string
}

type ArchiveWorkspaceCommand struct {
	WorkspaceID domain.WorkspaceID
}

type UnarchiveWorkspaceCommand struct {
	WorkspaceID domain.WorkspaceID
}

type UpdateWorkspacePolicyCommand struct {
	WorkspaceID domain.WorkspaceID
	Policy      domain.WorkspacePolicy
}

type UpdateWorkspaceVariablesCommand struct {
	WorkspaceID domain.WorkspaceID
	Variables   []domain.WorkspaceVariable
}

type PutWorkspaceSecretCommand struct {
	WorkspaceID domain.WorkspaceID
	SecretName  string
	SecretValue string
}

type ListWorkspaceSecretsQuery struct {
	WorkspaceID domain.WorkspaceID
}

type DeleteWorkspaceSecretCommand struct {
	WorkspaceID domain.WorkspaceID
	SecretName  string
}

type GetWorkspaceQuery struct {
	WorkspaceID domain.WorkspaceID
}

type ListWorkspacesQuery struct {
	Statuses []domain.WorkspaceStatus
	NameLike string
	Limit    int
	Offset   int
}

type WorkspaceView struct {
	Workspace domain.Workspace
}

// FlowManagementUseCase отвечает за создание, редактирование, получение и предварительную валидацию переиспользуемых определений флоу.
type FlowManagementUseCase interface {
	CreateFlow(ctx context.Context, cmd CreateFlowCommand) (FlowDefinitionView, error)
	UpdateFlow(ctx context.Context, cmd UpdateFlowCommand) (FlowDefinitionView, error)
	GetFlow(ctx context.Context, query GetFlowQuery) (FlowDefinitionView, error)
	ListFlows(ctx context.Context, query ListFlowsQuery) ([]domain.Flow, error)
	ValidateFlow(ctx context.Context, cmd ValidateFlowCommand) (FlowValidationResult, error)
}

// SavedRequestManagementUseCase отвечает за жизненный цикл и поиск переиспользуемых определений одиночных запросов.
type SavedRequestManagementUseCase interface {
	CreateSavedRequest(ctx context.Context, cmd CreateSavedRequestCommand) (SavedRequestView, error)
	UpdateSavedRequest(ctx context.Context, cmd UpdateSavedRequestCommand) (SavedRequestView, error)
	GetSavedRequest(ctx context.Context, query GetSavedRequestQuery) (SavedRequestView, error)
	ListSavedRequests(ctx context.Context, query ListSavedRequestsQuery) ([]domain.SavedRequest, error)
}

// FlowLaunchUseCase отвечает за асинхронные запросы на запуск и создание исходной записи запуска.
type FlowLaunchUseCase interface {
	LaunchFlow(ctx context.Context, input LaunchFlowInput) (domain.FlowRun, error)
}

// SavedRequestLaunchUseCase отвечает за отдельный сценарий выполнения сохранённого запроса.
type SavedRequestLaunchUseCase interface {
	LaunchSavedRequest(ctx context.Context, input LaunchSavedRequestInput) (domain.FlowRun, error)
	RunSavedRequest(ctx context.Context, input LaunchSavedRequestInput) (domain.FlowRun, error)
}

// RunStatusUseCase отвечает за доступ только на чтение к состоянию выполнения на уровне запуска и шагов.
type RunStatusUseCase interface {
	GetRunStatus(ctx context.Context, query GetRunStatusQuery) (RunStatusView, error)
	ListRuns(ctx context.Context, query ListRunsQuery) ([]domain.FlowRun, error)
}

// FlowRerunUseCase отвечает за семантику повторного запуска уже выполненных прогонов.
type FlowRerunUseCase interface {
	Rerun(ctx context.Context, input RerunInput) (domain.FlowRun, error)
}

// CurlImportUseCase отвечает за преобразование команд cURL в черновики определений флоу.
type CurlImportUseCase interface {
	ImportCurl(ctx context.Context, input ImportCurlInput) (ImportCurlResult, error)
}

// WorkspaceManagementUseCase отвечает за создание и получение командных границ изоляции.
type WorkspaceManagementUseCase interface {
	CreateWorkspace(ctx context.Context, cmd CreateWorkspaceCommand) (WorkspaceView, error)
	GetWorkspace(ctx context.Context, query GetWorkspaceQuery) (WorkspaceView, error)
	ListWorkspaces(ctx context.Context, query ListWorkspacesQuery) ([]domain.Workspace, error)
	UpdateWorkspace(ctx context.Context, cmd UpdateWorkspaceCommand) (WorkspaceView, error)
	ArchiveWorkspace(ctx context.Context, cmd ArchiveWorkspaceCommand) (WorkspaceView, error)
	UnarchiveWorkspace(ctx context.Context, cmd UnarchiveWorkspaceCommand) (WorkspaceView, error)
	UpdateWorkspacePolicy(ctx context.Context, cmd UpdateWorkspacePolicyCommand) (WorkspaceView, error)
	UpdateWorkspaceVariables(ctx context.Context, cmd UpdateWorkspaceVariablesCommand) (WorkspaceView, error)
	PutWorkspaceSecret(ctx context.Context, cmd PutWorkspaceSecretCommand) error
	ListWorkspaceSecrets(ctx context.Context, query ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error)
	DeleteWorkspaceSecret(ctx context.Context, cmd DeleteWorkspaceSecretCommand) error
}

// RunService объединяет уже реализованные сценарии, связанные с запусками, за единым контрактом для delivery-адаптеров.
type RunService interface {
	FlowLaunchUseCase
	SavedRequestLaunchUseCase
	RunStatusUseCase
	FlowRerunUseCase
}
