package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

func TestSavedRequestManagementService_CreateSavedRequest_RejectsArchivedWorkspace(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	workspaces := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-archived",
		Name:        "Archived",
		Slug:        "archived",
		Description: "archived",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusArchived,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewSavedRequestManagementService(workspaces, repository.NewInMemorySavedRequestRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewSavedRequestManagementService() error = %v", err)
	}

	_, err = service.CreateSavedRequest(context.Background(), CreateSavedRequestCommand{
		WorkspaceID:    "workspace-archived",
		SavedRequestID: "saved-1",
		Name:           "Health",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
	})
	if err == nil {
		t.Fatal("CreateSavedRequest() error = nil, want conflict")
	}
	var conflictErr *domain.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("CreateSavedRequest() error = %T, want ConflictError", err)
	}
}

func TestSavedRequestManagementService_CreateAndListByWorkspace(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 5, 0, 0, time.UTC)
	workspaces := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-a",
		Name:        "Workspace A",
		Slug:        "workspace-a",
		Description: "active",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewSavedRequestManagementService(workspaces, repository.NewInMemorySavedRequestRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewSavedRequestManagementService() error = %v", err)
	}

	view, err := service.CreateSavedRequest(context.Background(), CreateSavedRequestCommand{
		WorkspaceID:    "workspace-a",
		SavedRequestID: "saved-1",
		Name:           " Payments Health ",
		Description:    " health endpoint ",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
	})
	if err != nil {
		t.Fatalf("CreateSavedRequest() error = %v", err)
	}
	if view.SavedRequest.Name != "Payments Health" {
		t.Fatalf("Name = %q", view.SavedRequest.Name)
	}

	items, err := service.ListSavedRequests(context.Background(), ListSavedRequestsQuery{WorkspaceID: "workspace-a", NameLike: "health"})
	if err != nil {
		t.Fatalf("ListSavedRequests() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != "saved-1" {
		t.Fatalf("items = %#v", items)
	}
}

func TestRunCoordinator_LaunchSavedRequest_CreatesStandaloneRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 12, 10, 0, 0, time.UTC)
	workspaces := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-a",
		Name:        "Workspace A",
		Slug:        "workspace-a",
		Description: "active",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	savedRequests := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-1",
		WorkspaceID: "workspace-a",
		Name:        "Health",
		Description: "health",
		RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	runs := repository.NewInMemoryRunRepository()
	dispatcher := &recordingDispatcher{}
	service, err := NewRunCoordinator(workspaces, savedRequests, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), runs, repository.NewInMemoryRunStepRepository(), dispatcher, NewMonotonicRunIDFactory(30), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}

	run, err := service.LaunchSavedRequest(ctx, LaunchSavedRequestInput{
		WorkspaceID:    "workspace-a",
		SavedRequestID: "saved-1",
		InitiatedBy:    "alice",
		InputJSON:      json.RawMessage(`{"env":"staging"}`),
		IdempotencyKey: "saved-idem-1",
	})
	if err != nil {
		t.Fatalf("LaunchSavedRequest() error = %v", err)
	}
	if run.Target() != domain.RunTargetTypeSavedRequest {
		t.Fatalf("Target() = %q", run.Target())
	}
	if run.SavedRequestID != "saved-1" {
		t.Fatalf("SavedRequestID = %q", run.SavedRequestID)
	}
	if run.Status != domain.RunStatusQueued {
		t.Fatalf("Status = %q", run.Status)
	}
	if len(dispatcher.jobs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.jobs))
	}
	if dispatcher.jobs[0].SavedRequestID != "saved-1" {
		t.Fatalf("dispatched SavedRequestID = %q", dispatcher.jobs[0].SavedRequestID)
	}
	persisted, err := runs.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persisted.Target() != domain.RunTargetTypeSavedRequest {
		t.Fatalf("persisted.Target() = %q", persisted.Target())
	}
}

func TestRunCoordinator_RunSavedRequest_AppliesWorkspacePolicyBeforeCreatingRun(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 12, 12, 0, 0, time.UTC)
	workspaces := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-bad-request",
		Name:        "Workspace A",
		Slug:        "workspace-bad-request",
		Description: "active",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	savedRequests := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-bad",
		WorkspaceID: "workspace-bad-request",
		Name:        "Blocked",
		Description: "blocked",
		RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://evil.internal/health"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	runs := repository.NewInMemoryRunRepository()
	service, err := NewRunCoordinator(workspaces, savedRequests, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), runs, repository.NewInMemoryRunStepRepository(), &recordingDispatcher{}, NewMonotonicRunIDFactory(31), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}

	_, err = service.RunSavedRequest(ctx, LaunchSavedRequestInput{
		WorkspaceID:    "workspace-bad-request",
		SavedRequestID: "saved-bad",
		InitiatedBy:    "alice",
		InputJSON:      json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("RunSavedRequest() error = nil, want validation error")
	}
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("RunSavedRequest() error = %T, want ValidationError", err)
	}
	items, err := runs.List(ctx, repository.RunListFilter{WorkspaceID: "workspace-bad-request"})
	if err != nil {
		t.Fatalf("List(runs) error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("run count = %d, want 0", len(items))
	}
}

func TestRunCoordinator_Rerun_SavedRequestUsesSavedRequestLaunchPath(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 12, 15, 0, 0, time.UTC)
	workspaces := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-a",
		Name:        "Workspace A",
		Slug:        "workspace-a",
		Description: "active",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	savedRequests := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-1",
		WorkspaceID: "workspace-a",
		Name:        "Health",
		Description: "health",
		RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	runs := repository.NewInMemoryRunRepository()
	if err := runs.Create(ctx, domain.FlowRun{
		ID:             "run-saved-original",
		WorkspaceID:    "workspace-a",
		TargetType:     domain.RunTargetTypeSavedRequest,
		SavedRequestID: "saved-1",
		Status:         domain.RunStatusSucceeded,
		InputJSON:      json.RawMessage(`{"env":"prod"}`),
		InitiatedBy:    "alice",
	}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	service, err := NewRunCoordinator(workspaces, savedRequests, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), runs, repository.NewInMemoryRunStepRepository(), &recordingDispatcher{}, NewMonotonicRunIDFactory(40), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}

	rerun, err := service.Rerun(ctx, RerunInput{
		WorkspaceID:    "workspace-a",
		RunID:          "run-saved-original",
		InitiatedBy:    "bob",
		IdempotencyKey: "saved-rerun",
	})
	if err != nil {
		t.Fatalf("Rerun() error = %v", err)
	}
	if rerun.Target() != domain.RunTargetTypeSavedRequest {
		t.Fatalf("Target() = %q", rerun.Target())
	}
	if rerun.SavedRequestID != "saved-1" {
		t.Fatalf("SavedRequestID = %q", rerun.SavedRequestID)
	}
}
