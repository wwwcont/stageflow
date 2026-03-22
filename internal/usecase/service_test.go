package usecase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/execution"
	"stageflow/internal/repository"
)

type recordingDispatcher struct {
	jobs []execution.RunJob
}

func (d *recordingDispatcher) Dispatch(_ context.Context, job execution.RunJob) error {
	d.jobs = append(d.jobs, job)
	return nil
}

func TestFlowService_LaunchFlow_IdempotencyKey(t *testing.T) {
	ctx := context.Background()
	workspaceRepo := repository.NewInMemoryWorkspaceRepository()
	savedRequestRepo := repository.NewInMemorySavedRequestRepository()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	dispatcher := &recordingDispatcher{}
	service, err := NewRunCoordinator(workspaceRepo, savedRequestRepo, flowRepo, flowStepRepo, runRepo, runStepRepo, dispatcher, NewMonotonicRunIDFactory(10), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}
	flow := domain.Flow{WorkspaceID: "bootstrap", ID: "flow-idempotent", Name: "Idempotent flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowID(ctx, flow.ID, []domain.FlowStep{{
		ID:             "flow-idempotent-step-1",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "call",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://example.internal/health", Timeout: time.Second},
		ExtractionSpec: domain.ExtractionSpec{},
		AssertionSpec:  domain.AssertionSpec{},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("ReplaceByFlowID() error = %v", err)
	}
	service.flowSteps = flowStepRepo

	first, err := service.LaunchFlow(ctx, LaunchFlowInput{WorkspaceID: "bootstrap", FlowID: flow.ID, InitiatedBy: "alice", InputJSON: json.RawMessage(`{"foo":"bar"}`), Queue: "critical", IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("first LaunchFlow() error = %v", err)
	}
	second, err := service.LaunchFlow(ctx, LaunchFlowInput{WorkspaceID: "bootstrap", FlowID: flow.ID, InitiatedBy: "alice", InputJSON: json.RawMessage(`{"foo":"bar"}`), Queue: "critical", IdempotencyKey: "idem-1"})
	if err != nil {
		t.Fatalf("second LaunchFlow() error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("run ids = %q and %q, want same run", first.ID, second.ID)
	}
	if len(dispatcher.jobs) != 1 {
		t.Fatalf("dispatch count = %d, want 1", len(dispatcher.jobs))
	}
	persisted, err := runRepo.GetByID(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persisted.IdempotencyKey != "idem-1" {
		t.Fatalf("IdempotencyKey = %q", persisted.IdempotencyKey)
	}
	if persisted.QueueName != "critical" {
		t.Fatalf("QueueName = %q", persisted.QueueName)
	}
}

func TestFlowService_Rerun_UsesOverrideAndIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	workspaceRepo := repository.NewInMemoryWorkspaceRepository()
	savedRequestRepo := repository.NewInMemorySavedRequestRepository()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	dispatcher := &recordingDispatcher{}
	service, err := NewRunCoordinator(workspaceRepo, savedRequestRepo, flowRepo, flowStepRepo, runRepo, runStepRepo, dispatcher, NewMonotonicRunIDFactory(20), "default")
	if err != nil {
		t.Fatalf("NewRunCoordinator() error = %v", err)
	}
	flow := domain.Flow{WorkspaceID: "bootstrap", ID: "flow-rerun", Name: "Rerun flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowID(ctx, flow.ID, []domain.FlowStep{{
		ID:             "flow-rerun-step-1",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "call",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://example.internal/health", Timeout: time.Second},
		ExtractionSpec: domain.ExtractionSpec{},
		AssertionSpec:  domain.AssertionSpec{},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("ReplaceByFlowID() error = %v", err)
	}
	service.flowSteps = flowStepRepo
	original := domain.FlowRun{ID: "run-original", WorkspaceID: "bootstrap", FlowID: flow.ID, FlowVersion: 1, Status: domain.RunStatusSucceeded, InputJSON: json.RawMessage(`{"old":true}`), InitiatedBy: "alice", QueueName: "default"}
	if err := runRepo.Create(ctx, original); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	rerun, err := service.Rerun(ctx, RerunInput{WorkspaceID: "bootstrap", RunID: original.ID, InitiatedBy: "bob", OverrideJSON: json.RawMessage(`{"new":true}`), Queue: "reruns", IdempotencyKey: "rerun-idem"})
	if err != nil {
		t.Fatalf("Rerun() error = %v", err)
	}
	persisted, err := runRepo.GetByID(ctx, rerun.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if string(persisted.InputJSON) != `{"new":true}` {
		t.Fatalf("InputJSON = %s", string(persisted.InputJSON))
	}
	if persisted.IdempotencyKey != "rerun-idem" {
		t.Fatalf("IdempotencyKey = %q", persisted.IdempotencyKey)
	}
	if persisted.QueueName != "reruns" {
		t.Fatalf("QueueName = %q", persisted.QueueName)
	}
}
