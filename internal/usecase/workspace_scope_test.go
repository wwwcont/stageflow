package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func testWorkspacePolicy() domain.WorkspacePolicy {
	return domain.WorkspacePolicy{
		AllowedHosts:          []string{"svc.internal"},
		MaxSavedRequests:      10,
		MaxFlows:              10,
		MaxStepsPerFlow:       5,
		MaxRequestBodyBytes:   1024,
		DefaultTimeoutMS:      1000,
		MaxRunDurationSeconds: 60,
		DefaultRetryPolicy: domain.RetryPolicy{
			Enabled:         true,
			MaxAttempts:     3,
			BackoffStrategy: domain.BackoffStrategyFixed,
			InitialInterval: 100 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
		},
	}
}

func testWorkspaceVariables() []domain.WorkspaceVariable {
	return []domain.WorkspaceVariable{
		{Name: "base_url", Value: "https://svc.internal"},
		{Name: "tenant_id", Value: "tenant-42"},
	}
}

func testWorkspaceSecrets() []domain.WorkspaceSecret {
	return []domain.WorkspaceSecret{
		{Name: "crm_token", Value: "secret-token"},
		{Name: "billing_api_key", Value: "billing-key"},
	}
}

func TestFlowManagementService_CreateFlow_RejectsArchivedWorkspace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-archived",
		Name:        "Archived workspace",
		Slug:        "archived-workspace",
		Description: "archived",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusArchived,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	_, err = service.CreateFlow(ctx, CreateFlowCommand{
		WorkspaceID: "workspace-archived",
		FlowID:      "flow-1",
		Name:        "blocked",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:          "step-1",
			OrderIndex:  0,
			Name:        "call",
			RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
		}},
	})
	if err == nil {
		t.Fatal("CreateFlow() error = nil, want conflict")
	}
	var conflictErr *domain.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("CreateFlow() error = %T, want ConflictError", err)
	}
}

func TestRunCoordinator_LaunchFlow_RejectsArchivedWorkspace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-archived",
		Name:        "Archived workspace",
		Slug:        "archived-workspace",
		Description: "archived",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusArchived,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	flowRepo := repository.NewInMemoryFlowRepository()
	if err := flowRepo.Create(ctx, domain.Flow{WorkspaceID: "workspace-archived", ID: "flow-archived", Name: "blocked", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	coordinator, err := NewRunCoordinator(workspaceRepo, repository.NewInMemorySavedRequestRepository(), flowRepo, repository.NewInMemoryFlowStepRepository(), repository.NewInMemoryRunRepository(), repository.NewInMemoryRunStepRepository(), &recordingDispatcher{}, NewMonotonicRunIDFactory(100), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}

	_, err = coordinator.LaunchFlow(ctx, LaunchFlowInput{WorkspaceID: "workspace-archived", FlowID: "flow-archived", InitiatedBy: "alice", InputJSON: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("LaunchFlow() error = nil, want conflict")
	}
	var conflictErr *domain.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("LaunchFlow() error = %T, want ConflictError", err)
	}
}

func TestWorkspaceManagementService_CreateWorkspace_ValidatesSlug(t *testing.T) {
	service, err := NewWorkspaceManagementService(repository.NewInMemoryWorkspaceRepository(), clock.System{})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}

	_, err = service.CreateWorkspace(context.Background(), CreateWorkspaceCommand{
		WorkspaceID: "workspace-invalid",
		Name:        "Invalid",
		Slug:        "Invalid Slug",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
	})
	if err == nil {
		t.Fatal("CreateWorkspace() error = nil, want validation error")
	}
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("CreateWorkspace() error = %T, want ValidationError", err)
	}
}

func TestWorkspaceManagementService_UpdateWorkspace_ArchivePolicyAndVariables(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 10, 30, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-manage",
		Name:        "Manage workspace",
		Slug:        "manage-workspace",
		Description: "before",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		Variables:   testWorkspaceVariables(),
		Secrets:     testWorkspaceSecrets(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewWorkspaceManagementService(workspaceRepo, fixedClock{now: now.Add(time.Minute)})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}

	updated, err := service.UpdateWorkspace(ctx, UpdateWorkspaceCommand{
		WorkspaceID: "workspace-manage",
		Name:        " Managed Workspace ",
		Slug:        "managed-workspace",
		Description: " after ",
		OwnerTeam:   "payments",
	})
	if err != nil {
		t.Fatalf("UpdateWorkspace() error = %v", err)
	}
	if updated.Workspace.Name != "Managed Workspace" || updated.Workspace.Slug != "managed-workspace" || updated.Workspace.OwnerTeam != "payments" {
		t.Fatalf("updated workspace = %#v", updated.Workspace)
	}

	newPolicy := testWorkspacePolicy()
	newPolicy.MaxFlows = 99
	policyView, err := service.UpdateWorkspacePolicy(ctx, UpdateWorkspacePolicyCommand{WorkspaceID: "workspace-manage", Policy: newPolicy})
	if err != nil {
		t.Fatalf("UpdateWorkspacePolicy() error = %v", err)
	}
	if policyView.Workspace.Policy.MaxFlows != 99 {
		t.Fatalf("MaxFlows = %d, want 99", policyView.Workspace.Policy.MaxFlows)
	}

	varsView, err := service.UpdateWorkspaceVariables(ctx, UpdateWorkspaceVariablesCommand{
		WorkspaceID: "workspace-manage",
		Variables:   []domain.WorkspaceVariable{{Name: "base_url", Value: "https://api.internal"}, {Name: "tenant_id", Value: "tenant-77"}},
	})
	if err != nil {
		t.Fatalf("UpdateWorkspaceVariables() error = %v", err)
	}
	if got := varsView.Workspace.VariableMap()["tenant_id"]; got != "tenant-77" {
		t.Fatalf("tenant_id = %q, want tenant-77", got)
	}
	if len(varsView.Workspace.Secrets) != len(testWorkspaceSecrets()) {
		t.Fatalf("Secrets changed unexpectedly: %#v", varsView.Workspace.Secrets)
	}

	archived, err := service.ArchiveWorkspace(ctx, ArchiveWorkspaceCommand{WorkspaceID: "workspace-manage"})
	if err != nil {
		t.Fatalf("ArchiveWorkspace() error = %v", err)
	}
	if archived.Workspace.Status != domain.WorkspaceStatusArchived {
		t.Fatalf("Status = %q, want archived", archived.Workspace.Status)
	}

	_, err = service.UpdateWorkspace(ctx, UpdateWorkspaceCommand{
		WorkspaceID: "workspace-manage",
		Name:        "blocked",
		Slug:        "blocked",
		Description: "blocked",
		OwnerTeam:   "blocked",
	})
	if err == nil {
		t.Fatal("UpdateWorkspace() on archived workspace error = nil, want conflict")
	}
	var conflictErr *domain.ConflictError
	if !errors.As(err, &conflictErr) {
		t.Fatalf("UpdateWorkspace() error = %T, want ConflictError", err)
	}

	restored, err := service.UnarchiveWorkspace(ctx, UnarchiveWorkspaceCommand{WorkspaceID: "workspace-manage"})
	if err != nil {
		t.Fatalf("UnarchiveWorkspace() error = %v", err)
	}
	if restored.Workspace.Status != domain.WorkspaceStatusActive {
		t.Fatalf("Status after restore = %q, want active", restored.Workspace.Status)
	}
}

func TestFlowManagementService_CreateFlow_AppliesWorkspacePolicyDefaults(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-defaults",
		Name:        "Defaults workspace",
		Slug:        "defaults-workspace",
		Description: "defaults",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	view, err := service.CreateFlow(ctx, CreateFlowCommand{
		WorkspaceID: "workspace-defaults",
		FlowID:      "flow-defaults",
		Name:        "defaults",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:          "step-1",
			OrderIndex:  0,
			Name:        "call",
			RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}
	if got, want := view.Steps[0].RequestSpec.Timeout, time.Second; got != want {
		t.Fatalf("Timeout = %s, want %s", got, want)
	}
	if !view.Steps[0].RequestSpec.RetryPolicy.Enabled || view.Steps[0].RequestSpec.RetryPolicy.MaxAttempts != 3 {
		t.Fatalf("RetryPolicy = %#v", view.Steps[0].RequestSpec.RetryPolicy)
	}
}

func TestFlowManagementService_GetFlow_ReturnsRequestedVersionSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-versioned",
		Name:        "Versioned workspace",
		Slug:        "versioned-workspace",
		Description: "versions",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	if _, err := service.CreateFlow(ctx, CreateFlowCommand{
		WorkspaceID: "workspace-versioned",
		FlowID:      "flow-versioned",
		Name:        "Version one",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:          "step-1",
			OrderIndex:  0,
			Name:        "call-v1",
			RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/v1"},
		}},
	}); err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}
	if _, err := service.UpdateFlow(ctx, UpdateFlowCommand{
		FlowID: "flow-versioned",
		Name:   "Version two",
		Status: domain.FlowStatusActive,
		Steps: []FlowStepDraft{{
			ID:          "step-2",
			OrderIndex:  0,
			Name:        "call-v2",
			RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/v2"},
		}},
	}); err != nil {
		t.Fatalf("UpdateFlow() error = %v", err)
	}

	v1, err := service.GetFlow(ctx, GetFlowQuery{WorkspaceID: "workspace-versioned", FlowID: "flow-versioned", Version: 1})
	if err != nil {
		t.Fatalf("GetFlow(v1) error = %v", err)
	}
	if v1.Flow.Version != 1 || v1.Flow.Name != "Version one" || len(v1.Steps) != 1 || v1.Steps[0].Name != "call-v1" {
		t.Fatalf("v1 = %#v", v1)
	}
	current, err := service.GetFlow(ctx, GetFlowQuery{WorkspaceID: "workspace-versioned", FlowID: "flow-versioned"})
	if err != nil {
		t.Fatalf("GetFlow(current) error = %v", err)
	}
	if current.Flow.Version != 2 || current.Flow.Name != "Version two" || len(current.Steps) != 1 || current.Steps[0].Name != "call-v2" {
		t.Fatalf("current = %#v", current)
	}
}

func TestRunCoordinator_LaunchFlow_RejectsPolicyViolationsBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 5, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-policy-launch",
		Name:        "Policy workspace",
		Slug:        "policy-workspace",
		Description: "policy",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	flow := domain.Flow{WorkspaceID: "workspace-policy-launch", ID: "flow-policy-launch", Name: "blocked", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowID(ctx, flow.ID, []domain.FlowStep{{
		ID:             "step-1",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "call",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://evil.internal/health", Timeout: time.Second},
		ExtractionSpec: domain.ExtractionSpec{},
		AssertionSpec:  domain.AssertionSpec{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}}); err != nil {
		t.Fatalf("ReplaceByFlowID() error = %v", err)
	}
	dispatcher := &recordingDispatcher{}
	coordinator, err := NewRunCoordinator(workspaceRepo, repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, repository.NewInMemoryRunRepository(), repository.NewInMemoryRunStepRepository(), dispatcher, NewMonotonicRunIDFactory(101), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}
	coordinator.flowSteps = flowStepRepo

	_, err = coordinator.LaunchFlow(ctx, LaunchFlowInput{WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, InitiatedBy: "alice", InputJSON: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("LaunchFlow() error = nil, want validation error")
	}
	var validationErr *domain.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("LaunchFlow() error = %T, want ValidationError", err)
	}
	if len(dispatcher.jobs) != 0 {
		t.Fatalf("dispatch count = %d, want 0", len(dispatcher.jobs))
	}
}

func TestFlowManagementService_ValidateFlow_RejectsHostOutsideWorkspaceAllowlist(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-hosts",
		Name:        "Hosts workspace",
		Slug:        "hosts-workspace",
		Description: "hosts",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	result, err := service.ValidateFlow(ctx, ValidateFlowCommand{
		WorkspaceID: "workspace-hosts",
		FlowID:      "flow-hosts",
		Name:        "blocked-host",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:          "step-1",
			OrderIndex:  0,
			Name:        "call",
			RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://evil.internal/health"},
		}},
	})
	if err != nil {
		t.Fatalf("ValidateFlow() error = %v", err)
	}
	if result.Valid {
		t.Fatal("ValidateFlow().Valid = true, want false")
	}
	if len(result.Issues) == 0 {
		t.Fatal("ValidateFlow().Issues = empty")
	}
}

func TestFlowManagementService_CreateFlow_AllowsSavedRequestRefWithinWorkspace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 30, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-refs",
		Name:        "Refs workspace",
		Slug:        "refs-workspace",
		Description: "refs",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		Variables:   testWorkspaceVariables(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	body := `{"env":"staging"}`
	timeout := 2 * time.Second
	savedRequestRepo := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-health",
		WorkspaceID: "workspace-refs",
		Name:        "Health",
		Description: "health",
		RequestSpec: domain.RequestSpec{
			Method:      "GET",
			URLTemplate: "https://svc.internal/health",
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	service, err := NewFlowManagementService(workspaceRepo, savedRequestRepo, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	view, err := service.CreateFlow(ctx, CreateFlowCommand{
		WorkspaceID: "workspace-refs",
		FlowID:      "flow-refs",
		Name:        "saved-ref",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:             "step-1",
			OrderIndex:     0,
			Name:           "call-saved",
			StepType:       domain.FlowStepTypeSavedRequestRef,
			SavedRequestID: "saved-health",
			RequestSpecOverride: domain.RequestSpecOverride{
				Headers:      map[string]string{"X-Env": "{{workspace.vars.tenant_id}}"},
				BodyTemplate: &body,
				Timeout:      &timeout,
			},
		}},
	})
	if err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}
	if got := view.Steps[0].Type(); got != domain.FlowStepTypeSavedRequestRef {
		t.Fatalf("Type() = %q", got)
	}
	if got := view.Steps[0].SavedRequestID; got != "saved-health" {
		t.Fatalf("SavedRequestID = %q", got)
	}
}

func TestFlowManagementService_ValidateFlow_RejectsSavedRequestFromDifferentWorkspace(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 40, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(
		domain.Workspace{
			ID:          "workspace-a",
			Name:        "Workspace A",
			Slug:        "workspace-a",
			Description: "a",
			OwnerTeam:   "payments",
			Status:      domain.WorkspaceStatusActive,
			Policy:      testWorkspacePolicy(),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		domain.Workspace{
			ID:          "workspace-b",
			Name:        "Workspace B",
			Slug:        "workspace-b",
			Description: "b",
			OwnerTeam:   "platform",
			Status:      domain.WorkspaceStatusActive,
			Policy:      testWorkspacePolicy(),
			CreatedAt:   now,
			UpdatedAt:   now,
		},
	)
	savedRequestRepo := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-b",
		WorkspaceID: "workspace-b",
		Name:        "Health B",
		Description: "health",
		RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/health"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, savedRequestRepo, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	result, err := service.ValidateFlow(ctx, ValidateFlowCommand{
		WorkspaceID: "workspace-a",
		FlowID:      "flow-invalid-ref",
		Name:        "invalid-ref",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:             "step-1",
			OrderIndex:     0,
			Name:           "call-saved",
			StepType:       domain.FlowStepTypeSavedRequestRef,
			SavedRequestID: "saved-b",
		}},
	})
	if err != nil {
		t.Fatalf("ValidateFlow() error = %v", err)
	}
	if result.Valid {
		t.Fatal("ValidateFlow().Valid = true, want false")
	}
	if len(result.Issues) == 0 {
		t.Fatal("ValidateFlow().Issues = empty")
	}
}

func TestFlowManagementService_ValidateFlow_RejectsUnknownWorkspaceSecretAndRuntimeVar(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 45, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-vars",
		Name:        "Vars workspace",
		Slug:        "vars-workspace",
		Description: "vars",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		Variables:   testWorkspaceVariables(),
		Secrets:     testWorkspaceSecrets(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	result, err := service.ValidateFlow(ctx, ValidateFlowCommand{
		WorkspaceID: "workspace-vars",
		FlowID:      "flow-vars",
		Name:        "invalid-vars",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{{
			ID:         "step-1",
			OrderIndex: 0,
			Name:       "call",
			RequestSpec: domain.RequestSpec{
				Method:      "GET",
				URLTemplate: "{{workspace.vars.base_url}}/users/{{vars.user_id}}",
				Headers:     map[string]string{"Authorization": "Bearer {{secret.unknown_token}}"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("ValidateFlow() error = %v", err)
	}
	if result.Valid {
		t.Fatal("ValidateFlow().Valid = true, want false")
	}
	if len(result.Issues) == 0 {
		t.Fatal("ValidateFlow().Issues = empty")
	}
}

func TestFlowManagementService_ValidateFlow_AcceptsDefinedRuntimeWorkspaceVarsAndSecrets(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 50, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "workspace-runtime",
		Name:        "Runtime workspace",
		Slug:        "runtime-workspace",
		Description: "runtime",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy:      testWorkspacePolicy(),
		Variables:   testWorkspaceVariables(),
		Secrets:     testWorkspaceSecrets(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	service, err := NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), fixedClock{now: now})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}

	result, err := service.ValidateFlow(ctx, ValidateFlowCommand{
		WorkspaceID: "workspace-runtime",
		FlowID:      "flow-runtime",
		Name:        "valid-runtime",
		Status:      domain.FlowStatusDraft,
		Steps: []FlowStepDraft{
			{
				ID:         "step-1",
				OrderIndex: 0,
				Name:       "fetch-user",
				RequestSpec: domain.RequestSpec{
					Method:      "GET",
					URLTemplate: "{{workspace.vars.base_url}}/users/source",
					Headers:     map[string]string{"Authorization": "Bearer {{secret.crm_token}}"},
				},
				ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "user_id", Source: domain.ExtractionSourceBody, Selector: "$.id"}}},
			},
			{
				ID:         "step-2",
				OrderIndex: 1,
				Name:       "fetch-contracts",
				RequestSpec: domain.RequestSpec{
					Method:      "GET",
					URLTemplate: "{{workspace.vars.base_url}}/tenants/{{workspace.vars.tenant_id}}/users/{{vars.user_id}}",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ValidateFlow() error = %v", err)
	}
	if !result.Valid {
		t.Fatalf("ValidateFlow().Issues = %#v, want valid", result.Issues)
	}
}

func TestRunCoordinator_GetRunStatus_RejectsForeignWorkspace(t *testing.T) {
	ctx := context.Background()
	runRepo := repository.NewInMemoryRunRepository()
	if err := runRepo.Create(ctx, domain.FlowRun{ID: "run-1", WorkspaceID: "workspace-a", FlowID: "flow-a", FlowVersion: 1, Status: domain.RunStatusSucceeded, InputJSON: json.RawMessage(`{}`), InitiatedBy: "alice"}); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	coordinator, err := NewRunCoordinator(repository.NewInMemoryWorkspaceRepository(), repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), runRepo, repository.NewInMemoryRunStepRepository(), &recordingDispatcher{}, NewMonotonicRunIDFactory(1), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}

	_, err = coordinator.GetRunStatus(ctx, GetRunStatusQuery{WorkspaceID: "workspace-b", RunID: "run-1"})
	if err == nil {
		t.Fatal("GetRunStatus() error = nil, want not found")
	}
	var notFoundErr *domain.NotFoundError
	if !errors.As(err, &notFoundErr) {
		t.Fatalf("GetRunStatus() error = %T, want NotFoundError", err)
	}
}
