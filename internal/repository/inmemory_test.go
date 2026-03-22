package repository

import (
	"context"
	"testing"
	"time"

	"stageflow/internal/domain"
)

func TestInMemoryWorkspaceRepository_GetBySlug_NormalizesLookup(t *testing.T) {
	repo := NewInMemoryWorkspaceRepository()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	workspace := domain.Workspace{
		ID:          "workspace-a",
		Name:        "Payments",
		Slug:        "Payments-Team",
		Description: "payments",
		OwnerTeam:   "payments",
		Status:      domain.WorkspaceStatusActive,
		Policy: domain.WorkspacePolicy{
			AllowedHosts:          []string{"SVC.INTERNAL", "svc.internal"},
			MaxSavedRequests:      10,
			MaxFlows:              10,
			MaxStepsPerFlow:       5,
			MaxRequestBodyBytes:   1024,
			DefaultTimeoutMS:      1000,
			MaxRunDurationSeconds: 60,
			DefaultRetryPolicy:    domain.RetryPolicy{Enabled: false},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.Create(context.Background(), workspace); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	persisted, err := repo.GetBySlug(context.Background(), "payments-team")
	if err != nil {
		t.Fatalf("GetBySlug() error = %v", err)
	}
	if persisted.Slug != "payments-team" {
		t.Fatalf("Slug = %q, want %q", persisted.Slug, "payments-team")
	}
	if len(persisted.Policy.AllowedHosts) != 1 || persisted.Policy.AllowedHosts[0] != "svc.internal" {
		t.Fatalf("AllowedHosts = %#v", persisted.Policy.AllowedHosts)
	}
}

func TestInMemoryFlowRepository_List_NameLikeUsesContainsCaseInsensitive(t *testing.T) {
	repo := NewInMemoryFlowRepository()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	if err := repo.Create(context.Background(), domain.Flow{WorkspaceID: "workspace-a", ID: "flow-a", Name: "Payments Capture", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	flows, err := repo.List(context.Background(), FlowListFilter{WorkspaceID: "workspace-a", NameLike: "capture"})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(flows) != 1 || flows[0].ID != "flow-a" {
		t.Fatalf("flows = %#v", flows)
	}
}

func TestInMemoryFlowStepRepository_ListByFlowID_EmptyFlowReturnsEmptySlice(t *testing.T) {
	repo := NewInMemoryFlowStepRepository()

	steps, err := repo.ListByFlowID(context.Background(), "flow-empty")
	if err != nil {
		t.Fatalf("ListByFlowID() error = %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("len(steps) = %d, want 0", len(steps))
	}
}

func TestInMemoryFlowRepositories_PreserveVersionSnapshots(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	flowRepo := NewInMemoryFlowRepository()
	stepRepo := NewInMemoryFlowStepRepository()

	flow := domain.Flow{WorkspaceID: "workspace-a", ID: "flow-versioned", Name: "Versioned", Version: 1, Status: domain.FlowStatusDraft, CreatedAt: now, UpdatedAt: now}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	v1Steps := []domain.FlowStep{{ID: "step-v1", FlowID: flow.ID, OrderIndex: 0, Name: "v1", RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/v1"}, ExtractionSpec: domain.ExtractionSpec{}, AssertionSpec: domain.AssertionSpec{}, CreatedAt: now, UpdatedAt: now}}
	if err := stepRepo.ReplaceByFlowID(ctx, flow.ID, v1Steps); err != nil {
		t.Fatalf("ReplaceByFlowID(v1) error = %v", err)
	}
	if err := stepRepo.ReplaceByFlowVersion(ctx, flow.ID, 1, v1Steps); err != nil {
		t.Fatalf("ReplaceByFlowVersion(v1) error = %v", err)
	}

	flow.Version = 2
	flow.Name = "Versioned v2"
	flow.Status = domain.FlowStatusActive
	flow.UpdatedAt = now.Add(time.Minute)
	if err := flowRepo.Update(ctx, flow); err != nil {
		t.Fatalf("Update(flow) error = %v", err)
	}
	v2Steps := []domain.FlowStep{{ID: "step-v2", FlowID: flow.ID, OrderIndex: 0, Name: "v2", RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/v2"}, ExtractionSpec: domain.ExtractionSpec{}, AssertionSpec: domain.AssertionSpec{}, CreatedAt: now.Add(time.Minute), UpdatedAt: now.Add(time.Minute)}}
	if err := stepRepo.ReplaceByFlowID(ctx, flow.ID, v2Steps); err != nil {
		t.Fatalf("ReplaceByFlowID(v2) error = %v", err)
	}
	if err := stepRepo.ReplaceByFlowVersion(ctx, flow.ID, 2, v2Steps); err != nil {
		t.Fatalf("ReplaceByFlowVersion(v2) error = %v", err)
	}

	versionOne, err := flowRepo.GetVersion(ctx, flow.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion(v1) error = %v", err)
	}
	if versionOne.Name != "Versioned" || versionOne.Version != 1 {
		t.Fatalf("versionOne = %#v", versionOne)
	}
	versionOneSteps, err := stepRepo.ListByFlowVersion(ctx, flow.ID, 1)
	if err != nil {
		t.Fatalf("ListByFlowVersion(v1) error = %v", err)
	}
	if len(versionOneSteps) != 1 || versionOneSteps[0].Name != "v1" {
		t.Fatalf("versionOneSteps = %#v", versionOneSteps)
	}
}
