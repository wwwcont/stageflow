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
