package execution

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

func TestStageFlow_EndToEnd_RunExecutionWithDependentSteps(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"item-42"}`))
		case "/items/item-42/details":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	parsedURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}

	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:        "workspace-e2e",
		Name:      "Workspace E2E",
		Slug:      "workspace-e2e",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Policy:    domain.WorkspacePolicy{AllowedHosts: []string{parsedURL.Hostname()}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 5, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: server.URL}},
	})

	now := time.Date(2026, 3, 21, 13, 0, 0, 0, time.UTC)
	flow := domain.Flow{WorkspaceID: "workspace-e2e", ID: "flow-e2e", Name: "Dependent HTTP flow", Description: "creates item then fetches dependent details", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	steps := []domain.FlowStep{
		{
			ID:             "step-create-item",
			FlowID:         flow.ID,
			OrderIndex:     0,
			Name:           "create-item",
			RequestSpec:    domain.RequestSpec{Method: http.MethodGet, URLTemplate: server.URL + "/items", Timeout: time.Second},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "id", Source: domain.ExtractionSourceBody, Selector: "$.id", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "status-200", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "200"}}},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:             "step-fetch-item-details",
			FlowID:         flow.ID,
			OrderIndex:     1,
			Name:           "fetch-item-details",
			RequestSpec:    domain.RequestSpec{Method: http.MethodGet, URLTemplate: "{{workspace.vars.base_url}}/items/{{vars.id}}/details", Timeout: time.Second},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "status", Source: domain.ExtractionSourceBody, Selector: "$.status", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "status-ready", Target: domain.AssertionTargetBodyJSON, Operator: domain.AssertionOperatorEquals, Selector: "$.status", ExpectedValue: "ready"}}},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowID(ctx, flow.ID, steps); err != nil {
		t.Fatalf("ReplaceByFlowID() error = %v", err)
	}

	run := domain.FlowRun{ID: "run-e2e", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: flow.Version, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	httpExecutor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{AllowedHosts: []string{parsedURL.Hostname()}, AllowIPHosts: true, AllowLoopbackHosts: true})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}
	engine, err := NewSequentialEngine(workspaceRepo, repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, NewDefaultTemplateRenderer(), httpExecutor, NewDefaultExtractor(), NewDefaultAsserter(), clock.System{})
	if err != nil {
		t.Fatalf("NewSequentialEngine() error = %v", err)
	}

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	persistedRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persistedRun.Status != domain.RunStatusSucceeded {
		t.Fatalf("run status = %q, want %q", persistedRun.Status, domain.RunStatusSucceeded)
	}

	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if len(persistedSteps) != 2 {
		t.Fatalf("step count = %d, want 2", len(persistedSteps))
	}
	var details map[string]any
	if err := json.Unmarshal(persistedSteps[1].ExtractedValuesJSON, &details); err != nil {
		t.Fatalf("Unmarshal(ExtractedValuesJSON) error = %v", err)
	}
	if details["status"] != "ready" {
		t.Fatalf("step 2 extracted status = %#v, want ready", details["status"])
	}
}
