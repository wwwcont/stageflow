package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

type sequenceClock struct {
	times []time.Time
	idx   int
}

func (c *sequenceClock) Now() time.Time {
	if len(c.times) == 0 {
		return time.Unix(0, 0).UTC()
	}
	if c.idx >= len(c.times) {
		return c.times[len(c.times)-1]
	}
	value := c.times[c.idx]
	c.idx++
	return value
}

type recordingSleeper struct {
	delays []time.Duration
}

func (s *recordingSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	s.delays = append(s.delays, delay)
	return ctx.Err()
}

type stubHTTPExecutor struct {
	calls []domain.RequestSpec
	run   func(spec domain.RequestSpec, policy HTTPExecutionPolicy, call int) (HTTPExecutionResult, error)
}

func (e *stubHTTPExecutor) Execute(_ context.Context, spec domain.RequestSpec, policy HTTPExecutionPolicy) (HTTPExecutionResult, error) {
	e.calls = append(e.calls, spec)
	return e.run(spec, policy, len(e.calls))
}

func TestSequentialEngine_ExecuteRun_Success(t *testing.T) {
	ctx := context.Background()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	flow, steps := successFlowDefinition()
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)

	run := domain.FlowRun{
		ID:          "run-success",
		WorkspaceID: "workspace-success",
		FlowID:      "flow-success",
		FlowVersion: 1,
		Status:      domain.RunStatusQueued,
		InputJSON:   json.RawMessage(`{"customer_id":"cust-42"}`),
		InitiatedBy: "test",
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		switch call {
		case 1:
			if spec.URLTemplate != "https://svc.internal/session" {
				t.Fatalf("first URLTemplate = %q", spec.URLTemplate)
			}
			return HTTPExecutionResult{
				Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"token":"abc123"}`)},
				RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
				ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"token":"abc123"}`},
			}, nil
		case 2:
			if spec.URLTemplate != "https://svc.internal/orders/abc123" {
				t.Fatalf("second URLTemplate = %q", spec.URLTemplate)
			}
			if spec.BodyTemplate != `{"customer":"cust-42"}` {
				t.Fatalf("second BodyTemplate = %q", spec.BodyTemplate)
			}
			return HTTPExecutionResult{
				Response:         HTTPResponse{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"order_id":"ord-9"}`)},
				RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
				ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"order_id":"ord-9"}`},
			}, nil
		default:
			return HTTPExecutionResult{}, errors.New("unexpected call")
		}
	}}

	engine := mustEngine(t, testWorkspaceRepo("workspace-success"), repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(12))
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
	if persistedSteps[0].Status != domain.RunStepStatusSucceeded || persistedSteps[1].Status != domain.RunStepStatusSucceeded {
		t.Fatalf("step statuses = %q, %q", persistedSteps[0].Status, persistedSteps[1].Status)
	}
	if persistedSteps[1].RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0", persistedSteps[1].RetryCount)
	}
	var extracted map[string]any
	if err := json.Unmarshal(persistedSteps[1].ExtractedValuesJSON, &extracted); err != nil {
		t.Fatalf("Unmarshal(ExtractedValuesJSON) error = %v", err)
	}
	if extracted["order_id"] != "ord-9" {
		t.Fatalf("extracted order_id = %#v", extracted["order_id"])
	}
	var history []StepAttemptRecord
	if err := json.Unmarshal(persistedSteps[1].AttemptHistoryJSON, &history); err != nil {
		t.Fatalf("Unmarshal(AttemptHistoryJSON) error = %v", err)
	}
	if len(history) != 1 || history[0].ErrorKind != "" {
		t.Fatalf("attempt history = %#v", history)
	}
}

func TestSequentialEngine_ExecuteRun_RedactsWorkspaceSecretsInSnapshots(t *testing.T) {
	ctx := context.Background()
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:        "workspace-secrets",
		Name:      "Secrets workspace",
		Slug:      "workspace-secrets",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: "https://svc.internal"}},
		Secrets:   []domain.WorkspaceSecret{{Name: "crm_token", Value: "super-secret-token"}},
		Policy: domain.WorkspacePolicy{
			AllowedHosts:          []string{"svc.internal"},
			MaxSavedRequests:      10,
			MaxFlows:              10,
			MaxStepsPerFlow:       5,
			MaxRequestBodyBytes:   1024,
			DefaultTimeoutMS:      1000,
			MaxRunDurationSeconds: 60,
			DefaultRetryPolicy:    domain.RetryPolicy{Enabled: false},
		},
	})
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	now := time.Now().UTC()
	flow := domain.Flow{WorkspaceID: "workspace-secrets", ID: "flow-secrets", Name: "Secrets flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	steps := []domain.FlowStep{{
		ID:         "step-secret",
		FlowID:     flow.ID,
		OrderIndex: 0,
		Name:       "call-secret",
		RequestSpec: domain.RequestSpec{
			Method:      http.MethodGet,
			URLTemplate: "{{workspace.vars.base_url}}/secure",
			Headers:     map[string]string{"Authorization": "Bearer {{secret.crm_token}}"},
			Timeout:     time.Second,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}}
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)

	run := domain.FlowRun{ID: "run-secret", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		return HTTPExecutionResult{
			Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{}`)},
			RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
			ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"X-Echo": {"super-secret-token"}}, Body: `{"token":"super-secret-token"}`},
		}, nil
	}}
	engine := mustEngine(t, workspaceRepo, repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(10))

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if len(persistedSteps) != 1 {
		t.Fatalf("step count = %d, want 1", len(persistedSteps))
	}
	if got := string(persistedSteps[0].RequestSnapshotJSON); strings.Contains(got, "super-secret-token") {
		t.Fatalf("RequestSnapshotJSON leaked secret: %s", got)
	}
	if got := string(persistedSteps[0].ResponseSnapshotJSON); strings.Contains(got, "super-secret-token") {
		t.Fatalf("ResponseSnapshotJSON leaked secret: %s", got)
	}
}

func TestSequentialEngine_ExecuteRun_RetriesWithExponentialBackoff(t *testing.T) {
	ctx := context.Background()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	flow, steps := retryFlowDefinition()
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)

	run := domain.FlowRun{ID: "run-retry", WorkspaceID: "workspace-retry", FlowID: "flow-retry", FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		if call < 3 {
			return HTTPExecutionResult{RequestSnapshot: HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate}}, &domain.TransportError{Operation: "perform request", Cause: errors.New("temporary network issue")}
		}
		return HTTPExecutionResult{
			Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)},
			RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate},
			ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"ok":true}`},
		}, nil
	}}
	engine := mustEngine(t, testWorkspaceRepo("workspace-retry"), repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(16))
	sleeper := &recordingSleeper{}
	engine.sleeper = sleeper

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !reflect.DeepEqual(sleeper.delays, []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}) {
		t.Fatalf("delays = %#v", sleeper.delays)
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if persistedSteps[0].RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2", persistedSteps[0].RetryCount)
	}
	var history []StepAttemptRecord
	if err := json.Unmarshal(persistedSteps[0].AttemptHistoryJSON, &history); err != nil {
		t.Fatalf("Unmarshal(AttemptHistoryJSON) error = %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("attempt count = %d, want 3", len(history))
	}
	if history[0].ErrorKind != FailureKindTransport || history[1].ErrorKind != FailureKindTransport || history[2].ErrorKind != "" {
		t.Fatalf("history kinds = %#v", history)
	}
}

func TestSequentialEngine_ExecuteRun_FailsOnAssertionError(t *testing.T) {
	ctx := context.Background()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	flow, steps := assertionFailureDefinition()
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)

	run := domain.FlowRun{ID: "run-assertion", WorkspaceID: "workspace-assert", FlowID: "flow-assert", FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		return HTTPExecutionResult{
			Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"ok":true}`)},
			RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate},
			ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"ok":true}`},
		}, nil
	}}
	engine := mustEngine(t, testWorkspaceRepo("workspace-assert"), repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(10))

	err := engine.ExecuteRun(ctx, run.ID)
	if err == nil || !errors.Is(err, domain.ErrAssertionFailure) {
		t.Fatalf("ExecuteRun() error = %v, want assertion failure", err)
	}
	persistedRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persistedRun.Status != domain.RunStatusFailed {
		t.Fatalf("run status = %q, want %q", persistedRun.Status, domain.RunStatusFailed)
	}
	if persistedRun.ErrorMessage == "" || persistedRun.ErrorMessage[:len(string(FailureKindAssertion))] != string(FailureKindAssertion) {
		t.Fatalf("run error message = %q", persistedRun.ErrorMessage)
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	var history []StepAttemptRecord
	if err := json.Unmarshal(persistedSteps[0].AttemptHistoryJSON, &history); err != nil {
		t.Fatalf("Unmarshal(AttemptHistoryJSON) error = %v", err)
	}
	if history[0].ErrorKind != FailureKindAssertion {
		t.Fatalf("error kind = %q, want %q", history[0].ErrorKind, FailureKindAssertion)
	}
}

func TestSequentialEngine_ExecuteRun_ResumesExistingRunState(t *testing.T) {
	ctx := context.Background()
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	flow, steps := successFlowDefinition()
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)

	startedAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	run := domain.FlowRun{ID: "run-resume", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: 1, Status: domain.RunStatusRunning, InputJSON: json.RawMessage(`{"customer_id":"cust-77"}`), InitiatedBy: "test", StartedAt: &startedAt, CreatedAt: startedAt, UpdatedAt: startedAt}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	attemptHistory, _ := json.Marshal([]StepAttemptRecord{{Attempt: 1, StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second)}})
	extractedValues, _ := json.Marshal(map[string]any{"token": "resume-token"})
	if err := runStepRepo.Create(ctx, domain.FlowRunStep{
		ID:                  "run-resume-step-00-login",
		RunID:               run.ID,
		StepName:            steps[0].Name,
		StepOrder:           steps[0].OrderIndex,
		Status:              domain.RunStepStatusSucceeded,
		ExtractedValuesJSON: extractedValues,
		AttemptHistoryJSON:  attemptHistory,
		CreatedAt:           startedAt,
		UpdatedAt:           startedAt.Add(time.Second),
		StartedAt:           &startedAt,
		FinishedAt:          ptrTime(startedAt.Add(time.Second)),
	}); err != nil {
		t.Fatalf("Create(run step) error = %v", err)
	}

	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		if call != 1 {
			t.Fatalf("call count = %d, want 1", call)
		}
		if spec.URLTemplate != "https://svc.internal/orders/resume-token" {
			t.Fatalf("URLTemplate = %q", spec.URLTemplate)
		}
		return HTTPExecutionResult{
			Response:         HTTPResponse{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"order_id":"ord-resume"}`)},
			RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
			ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"order_id":"ord-resume"}`},
		}, nil
	}}
	engine := mustEngine(t, testWorkspaceRepo(flow.WorkspaceID), repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(10))

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if len(persistedSteps) != 2 {
		t.Fatalf("step count = %d, want 2", len(persistedSteps))
	}
}

func TestSequentialEngine_ExecuteRun_FlowSupportsSavedRequestRefsAndRuntimeVars(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:        "workspace-runtime",
		Name:      "Runtime workspace",
		Slug:      "workspace-runtime",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: "https://svc.internal"}},
		Secrets:   []domain.WorkspaceSecret{{Name: "api_token", Value: "runtime-secret"}},
		Policy: domain.WorkspacePolicy{
			AllowedHosts:          []string{"svc.internal"},
			MaxSavedRequests:      10,
			MaxFlows:              10,
			MaxStepsPerFlow:       10,
			MaxRequestBodyBytes:   128,
			DefaultTimeoutMS:      250,
			MaxRunDurationSeconds: 60,
			DefaultRetryPolicy:    domain.RetryPolicy{Enabled: true, MaxAttempts: 2, BackoffStrategy: domain.BackoffStrategyFixed, InitialInterval: 25 * time.Millisecond, RetryableStatusCodes: []int{502}},
		},
	})
	savedRequestRepo := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-login",
		WorkspaceID: "workspace-runtime",
		Name:        "saved-login",
		RequestSpec: domain.RequestSpec{
			Method:       http.MethodPost,
			URLTemplate:  "{{workspace.vars.base_url}}/session",
			Headers:      map[string]string{"Authorization": "Bearer {{secret.api_token}}"},
			BodyTemplate: `{"user":"{{run.input.user}}"}`,
		},
		CreatedAt: now,
		UpdatedAt: now,
	})
	flowRepo := repository.NewInMemoryFlowRepository()
	flowStepRepo := repository.NewInMemoryFlowStepRepository()
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	flow := domain.Flow{WorkspaceID: "workspace-runtime", ID: "flow-runtime", Name: "Runtime flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	overrideBody := `{"user":"{{run.input.user}}","scope":"checkout"}`
	steps := []domain.FlowStep{
		{
			ID:             "step-login-ref",
			FlowID:         flow.ID,
			OrderIndex:     0,
			Name:           "login-ref",
			StepType:       domain.FlowStepTypeSavedRequestRef,
			SavedRequestID: "saved-login",
			RequestSpecOverride: domain.RequestSpecOverride{
				Headers:      map[string]string{"X-Trace": "{{run.id}}"},
				BodyTemplate: &overrideBody,
			},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "AuthToken", Source: domain.ExtractionSourceBody, Selector: "$.token", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:         "step-checkout",
			FlowID:     flow.ID,
			OrderIndex: 1,
			Name:       "checkout",
			RequestSpec: domain.RequestSpec{
				Method:       http.MethodPost,
				URLTemplate:  "{{workspace.vars.base_url}}/checkout/{{vars.AuthToken}}",
				BodyTemplate: `{"body":"runtime-secret","token":"{{vars.AuthToken}}"}`,
			},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "ReceiptID", Source: domain.ExtractionSourceBody, Selector: "$.receipt_id", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)
	run := domain.FlowRun{ID: "run-runtime", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: flow.Version, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{"user":"alice"}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}

	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		switch call {
		case 1:
			if spec.URLTemplate != "https://svc.internal/session" {
				t.Fatalf("first URLTemplate = %q", spec.URLTemplate)
			}
			if spec.BodyTemplate != `{"user":"alice","scope":"checkout"}` {
				t.Fatalf("first BodyTemplate = %q", spec.BodyTemplate)
			}
			if spec.Timeout != 250*time.Millisecond {
				t.Fatalf("first Timeout = %s", spec.Timeout)
			}
			if !spec.RetryPolicy.Enabled {
				t.Fatalf("retry defaults were not applied to request spec")
			}
			return HTTPExecutionResult{
				Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"token":"tok-1"}`)},
				RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
				ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"X-Secret": {"runtime-secret"}}, Body: `{"token":"tok-1","secret":"runtime-secret"}`},
			}, nil
		case 2:
			if spec.URLTemplate != "https://svc.internal/checkout/tok-1" {
				t.Fatalf("second URLTemplate = %q", spec.URLTemplate)
			}
			if spec.BodyTemplate != `{"body":"runtime-secret","token":"tok-1"}` {
				t.Fatalf("second BodyTemplate = %q", spec.BodyTemplate)
			}
			return HTTPExecutionResult{
				Response:         HTTPResponse{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{"receipt_id":"rcpt-1"}`)},
				RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate, Headers: spec.Headers, Body: spec.BodyTemplate},
				ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusCreated, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{"receipt_id":"rcpt-1"}`},
			}, nil
		default:
			return HTTPExecutionResult{}, errors.New("unexpected call")
		}
	}}
	engine := mustEngine(t, workspaceRepo, savedRequestRepo, flowRepo, flowStepRepo, runRepo, runStepRepo, executor, newSequenceClock(16))

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if len(persistedSteps) != 2 {
		t.Fatalf("step count = %d, want 2", len(persistedSteps))
	}
	var firstExtracted map[string]any
	if err := json.Unmarshal(persistedSteps[0].ExtractedValuesJSON, &firstExtracted); err != nil {
		t.Fatalf("Unmarshal(first ExtractedValuesJSON) error = %v", err)
	}
	if _, ok := firstExtracted["AuthToken"]; !ok {
		t.Fatalf("extracted values = %#v, want AuthToken key preserved exactly", firstExtracted)
	}
	if got := string(persistedSteps[0].RequestSnapshotJSON); strings.Contains(got, "runtime-secret") {
		t.Fatalf("first RequestSnapshotJSON leaked secret: %s", got)
	}
	if got := string(persistedSteps[0].ResponseSnapshotJSON); strings.Contains(got, "runtime-secret") {
		t.Fatalf("first ResponseSnapshotJSON leaked secret: %s", got)
	}
}

func TestSequentialEngine_ExecuteRun_SavedRequestRunUsesStandaloneTarget(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 21, 11, 30, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:        "workspace-saved-run",
		Name:      "Saved Run workspace",
		Slug:      "workspace-saved-run",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: "https://svc.internal"}},
		Policy: domain.WorkspacePolicy{
			AllowedHosts:          []string{"svc.internal"},
			MaxSavedRequests:      10,
			MaxFlows:              10,
			MaxStepsPerFlow:       10,
			MaxRequestBodyBytes:   256,
			DefaultTimeoutMS:      150,
			MaxRunDurationSeconds: 60,
			DefaultRetryPolicy:    domain.RetryPolicy{Enabled: true, MaxAttempts: 2, BackoffStrategy: domain.BackoffStrategyFixed, InitialInterval: 10 * time.Millisecond},
		},
	})
	savedRequestRepo := repository.NewInMemorySavedRequestRepository(domain.SavedRequest{
		ID:          "saved-health",
		WorkspaceID: "workspace-saved-run",
		Name:        "health-check",
		RequestSpec: domain.RequestSpec{Method: http.MethodGet, URLTemplate: "{{workspace.vars.base_url}}/health"},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	runRepo := repository.NewInMemoryRunRepository()
	runStepRepo := repository.NewInMemoryRunStepRepository()
	run := domain.FlowRun{
		ID:             "run-saved-health",
		WorkspaceID:    "workspace-saved-run",
		TargetType:     domain.RunTargetTypeSavedRequest,
		SavedRequestID: "saved-health",
		Status:         domain.RunStatusQueued,
		InputJSON:      json.RawMessage(`{}`),
		InitiatedBy:    "alice",
	}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	executor := &stubHTTPExecutor{run: func(spec domain.RequestSpec, _ HTTPExecutionPolicy, call int) (HTTPExecutionResult, error) {
		if call != 1 {
			t.Fatalf("call count = %d, want 1", call)
		}
		if spec.URLTemplate != "https://svc.internal/health" {
			t.Fatalf("URLTemplate = %q", spec.URLTemplate)
		}
		if spec.Timeout != 150*time.Millisecond {
			t.Fatalf("Timeout = %s", spec.Timeout)
		}
		if !spec.RetryPolicy.Enabled {
			t.Fatalf("expected workspace retry policy to be applied")
		}
		return HTTPExecutionResult{
			Response:         HTTPResponse{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: []byte(`{}`)},
			RequestSnapshot:  HTTPRequestSnapshot{Method: spec.Method, URL: spec.URLTemplate},
			ResponseSnapshot: HTTPResponseSnapshot{StatusCode: http.StatusOK, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: `{}`},
		}, nil
	}}
	engine := mustEngine(t, workspaceRepo, savedRequestRepo, repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), runRepo, runStepRepo, executor, newSequenceClock(10))

	if err := engine.ExecuteRun(ctx, run.ID); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	persistedRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if persistedRun.Target() != domain.RunTargetTypeSavedRequest {
		t.Fatalf("Target() = %q", persistedRun.Target())
	}
	persistedSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID() error = %v", err)
	}
	if len(persistedSteps) != 1 {
		t.Fatalf("step count = %d, want 1", len(persistedSteps))
	}
	if persistedSteps[0].StepName != "health-check" {
		t.Fatalf("StepName = %q", persistedSteps[0].StepName)
	}
}

func TestSequentialEngine_ExecuteRun_WithSafeHTTPExecutor(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/checkout" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"receipt_id":"r-1"}`))
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
	flow := domain.Flow{WorkspaceID: "workspace-http", ID: "flow-http", Name: "HTTP flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	steps := []domain.FlowStep{{
		ID:             "step-http",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "checkout",
		RequestSpec:    domain.RequestSpec{Method: http.MethodPost, URLTemplate: server.URL + "/checkout", Timeout: time.Second},
		ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "receipt_id", Source: domain.ExtractionSourceBody, Selector: "$.receipt_id", Required: true}}},
		AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "created", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "201"}}},
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}}
	mustCreateFlow(t, ctx, flowRepo, flowStepRepo, flow, steps)
	run := domain.FlowRun{ID: "run-http", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: 1, Status: domain.RunStatusQueued, InputJSON: json.RawMessage(`{}`), InitiatedBy: "test"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	httpExecutor, err := NewSafeHTTPExecutor(HTTPExecutorConfig{AllowedHosts: []string{parsedURL.Hostname()}, AllowIPHosts: true, AllowLoopbackHosts: true})
	if err != nil {
		t.Fatalf("NewSafeHTTPExecutor() error = %v", err)
	}
	engine := mustEngine(t, testWorkspaceRepo(flow.WorkspaceID), repository.NewInMemorySavedRequestRepository(), flowRepo, flowStepRepo, runRepo, runStepRepo, httpExecutor, newSequenceClock(10))

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
}

func mustEngine(t *testing.T, workspaceRepo repository.WorkspaceRepository, savedRequestRepo repository.SavedRequestRepository, flowRepo repository.FlowRepository, flowStepRepo repository.FlowStepRepository, runRepo repository.RunRepository, runStepRepo repository.RunStepRepository, httpExecutor HTTPExecutor, clk *sequenceClock) *SequentialEngine {
	t.Helper()
	engine, err := NewSequentialEngine(workspaceRepo, savedRequestRepo, flowRepo, flowStepRepo, runRepo, runStepRepo, NewDefaultTemplateRenderer(), httpExecutor, NewDefaultExtractor(), NewDefaultAsserter(), clk)
	if err != nil {
		t.Fatalf("NewSequentialEngine() error = %v", err)
	}
	return engine
}

func newSequenceClock(count int) *sequenceClock {
	base := time.Date(2026, 3, 21, 9, 0, 0, 0, time.UTC)
	times := make([]time.Time, count)
	for i := range times {
		times[i] = base.Add(time.Duration(i) * time.Second)
	}
	return &sequenceClock{times: times}
}

func testWorkspaceRepo(ids ...domain.WorkspaceID) repository.WorkspaceRepository {
	workspaces := make([]domain.Workspace, 0, len(ids))
	for _, id := range ids {
		workspaces = append(workspaces, domain.Workspace{
			ID:        id,
			Name:      string(id),
			Slug:      strings.ReplaceAll(string(id), "_", "-"),
			OwnerTeam: "platform",
			Status:    domain.WorkspaceStatusActive,
			Policy: domain.WorkspacePolicy{
				AllowedHosts:          []string{"svc.internal", "example.internal", "127.0.0.1", "localhost"},
				MaxSavedRequests:      10,
				MaxFlows:              10,
				MaxStepsPerFlow:       10,
				MaxRequestBodyBytes:   1 << 20,
				DefaultTimeoutMS:      1000,
				MaxRunDurationSeconds: 60,
				DefaultRetryPolicy:    domain.RetryPolicy{Enabled: false},
			},
		})
	}
	return repository.NewInMemoryWorkspaceRepository(workspaces...)
}

func mustCreateFlow(t *testing.T, ctx context.Context, flowRepo repository.FlowRepository, flowStepRepo repository.FlowStepRepository, flow domain.Flow, steps []domain.FlowStep) {
	t.Helper()
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowID(ctx, flow.ID, steps); err != nil {
		t.Fatalf("ReplaceByFlowID() error = %v", err)
	}
}

func successFlowDefinition() (domain.Flow, []domain.FlowStep) {
	now := time.Date(2026, 3, 21, 8, 0, 0, 0, time.UTC)
	flow := domain.Flow{WorkspaceID: "workspace-success", ID: "flow-success", Name: "Success flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	steps := []domain.FlowStep{
		{
			ID:             "step-login",
			FlowID:         flow.ID,
			OrderIndex:     0,
			Name:           "login",
			RequestSpec:    domain.RequestSpec{Method: http.MethodGet, URLTemplate: "https://svc.internal/session", Timeout: time.Second},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "token", Source: domain.ExtractionSourceBody, Selector: "$.token", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "status-ok", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "200"}}},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:             "step-create-order",
			FlowID:         flow.ID,
			OrderIndex:     1,
			Name:           "create-order",
			RequestSpec:    domain.RequestSpec{Method: http.MethodPost, URLTemplate: "https://svc.internal/orders/{{steps.login.token}}", BodyTemplate: `{"customer":"{{run.input.customer_id}}"}`, Timeout: time.Second},
			ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "order_id", Source: domain.ExtractionSourceBody, Selector: "$.order_id", Required: true}}},
			AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "created", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "201"}}},
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	return flow, steps
}

func retryFlowDefinition() (domain.Flow, []domain.FlowStep) {
	now := time.Date(2026, 3, 21, 8, 0, 0, 0, time.UTC)
	flow := domain.Flow{WorkspaceID: "workspace-retry", ID: "flow-retry", Name: "Retry flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	steps := []domain.FlowStep{{
		ID:             "step-retry",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "retry-step",
		RequestSpec:    domain.RequestSpec{Method: http.MethodGet, URLTemplate: "https://svc.internal/retry", Timeout: time.Second, RetryPolicy: domain.RetryPolicy{Enabled: true, MaxAttempts: 3, BackoffStrategy: domain.BackoffStrategyExponential, InitialInterval: 100 * time.Millisecond, MaxInterval: 300 * time.Millisecond}},
		ExtractionSpec: domain.ExtractionSpec{},
		AssertionSpec:  domain.AssertionSpec{},
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	return flow, steps
}

func assertionFailureDefinition() (domain.Flow, []domain.FlowStep) {
	now := time.Date(2026, 3, 21, 8, 0, 0, 0, time.UTC)
	flow := domain.Flow{WorkspaceID: "workspace-assert", ID: "flow-assert", Name: "Assertion flow", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	steps := []domain.FlowStep{{
		ID:             "step-assert",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "assert-step",
		RequestSpec:    domain.RequestSpec{Method: http.MethodGet, URLTemplate: "https://svc.internal/assert", Timeout: time.Second},
		ExtractionSpec: domain.ExtractionSpec{},
		AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "must-be-created", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "201"}}},
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	return flow, steps
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
