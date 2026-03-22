package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

func TestPostgresRepositories_FlowAndRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	db := newPostgresHarnessDB(t)
	defer db.Close()

	workspaceRepo := NewWorkspaceRepository(db)
	savedRequestRepo := NewSavedRequestRepository(db)
	flowRepo := NewFlowRepository(db)
	flowStepRepo := NewFlowStepRepository(db)
	runRepo := NewRunRepository(db)
	runStepRepo := NewRunStepRepository(db)
	runEventRepo := NewRunEventRepository(db)

	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	workspace := domain.Workspace{
		ID:          "workspace-pg",
		Name:        "Postgres workspace",
		Slug:        "workspace-pg",
		Description: "adapter integration",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Variables:   []domain.WorkspaceVariable{{Name: "base_url", Value: "https://svc.internal"}},
		Secrets:     []domain.WorkspaceSecret{{Name: "api_token", Value: "secret"}},
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
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := workspaceRepo.Create(ctx, workspace); err != nil {
		t.Fatalf("Create(workspace) error = %v", err)
	}
	persistedWorkspace, err := workspaceRepo.GetByID(ctx, workspace.ID)
	if err != nil {
		t.Fatalf("GetByID(workspace) error = %v", err)
	}
	if persistedWorkspace.Slug != workspace.Slug || len(persistedWorkspace.Variables) != 1 || len(persistedWorkspace.Secrets) != 1 {
		t.Fatalf("persisted workspace = %#v", persistedWorkspace)
	}
	workspaces, err := workspaceRepo.List(ctx, repository.WorkspaceListFilter{NameLike: "postgres"})
	if err != nil {
		t.Fatalf("List(workspaces) error = %v", err)
	}
	if len(workspaces) != 1 {
		t.Fatalf("workspace count = %d, want 1", len(workspaces))
	}

	savedRequest := domain.SavedRequest{
		ID:          "saved-pg",
		WorkspaceID: workspace.ID,
		Name:        "Ping",
		Description: "adapter integration",
		RequestSpec: domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/ping"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := savedRequestRepo.Create(ctx, savedRequest); err != nil {
		t.Fatalf("Create(saved request) error = %v", err)
	}
	persistedRequest, err := savedRequestRepo.GetByID(ctx, savedRequest.ID)
	if err != nil {
		t.Fatalf("GetByID(saved request) error = %v", err)
	}
	if persistedRequest.Name != savedRequest.Name {
		t.Fatalf("persisted saved request name = %q, want %q", persistedRequest.Name, savedRequest.Name)
	}
	requests, err := savedRequestRepo.List(ctx, repository.SavedRequestListFilter{WorkspaceID: workspace.ID})
	if err != nil {
		t.Fatalf("List(saved requests) error = %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("saved request count = %d, want 1", len(requests))
	}

	flow := domain.Flow{WorkspaceID: "workspace-pg", ID: "flow-pg", Name: "postgres flow", Description: "adapter integration", Version: 1, Status: domain.FlowStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := flowRepo.Create(ctx, flow); err != nil {
		t.Fatalf("Create(flow) error = %v", err)
	}
	steps := []domain.FlowStep{{
		ID:             "flow-pg-step-1",
		FlowID:         flow.ID,
		OrderIndex:     0,
		Name:           "fetch",
		RequestSpec:    domain.RequestSpec{Method: "GET", URLTemplate: "https://svc.internal/items"},
		ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "id", Source: domain.ExtractionSourceBody, Selector: "$.id"}}},
		AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "status", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "200"}}},
		CreatedAt:      now,
		UpdatedAt:      now,
	}}
	if err := flowStepRepo.CreateMany(ctx, steps); err != nil {
		t.Fatalf("CreateMany(flow steps) error = %v", err)
	}
	if err := flowStepRepo.ReplaceByFlowVersion(ctx, flow.ID, flow.Version, steps); err != nil {
		t.Fatalf("ReplaceByFlowVersion(flow steps) error = %v", err)
	}
	persistedFlow, err := flowRepo.GetByID(ctx, flow.ID)
	if err != nil {
		t.Fatalf("GetByID(flow) error = %v", err)
	}
	if persistedFlow.Name != flow.Name {
		t.Fatalf("persisted flow name = %q, want %q", persistedFlow.Name, flow.Name)
	}
	persistedFlowVersion, err := flowRepo.GetVersion(ctx, flow.ID, 1)
	if err != nil {
		t.Fatalf("GetVersion(flow) error = %v", err)
	}
	if persistedFlowVersion.Version != 1 || persistedFlowVersion.Name != flow.Name {
		t.Fatalf("persisted flow version = %#v", persistedFlowVersion)
	}
	persistedSteps, err := flowStepRepo.ListByFlowID(ctx, flow.ID)
	if err != nil {
		t.Fatalf("ListByFlowID() error = %v", err)
	}
	if len(persistedSteps) != 1 || persistedSteps[0].Name != "fetch" {
		t.Fatalf("persisted steps = %#v", persistedSteps)
	}
	persistedVersionSteps, err := flowStepRepo.ListByFlowVersion(ctx, flow.ID, 1)
	if err != nil {
		t.Fatalf("ListByFlowVersion() error = %v", err)
	}
	if len(persistedVersionSteps) != 1 || persistedVersionSteps[0].Name != "fetch" {
		t.Fatalf("persisted version steps = %#v", persistedVersionSteps)
	}
	flows, err := flowRepo.List(ctx, repository.FlowListFilter{NameLike: "postgres"})
	if err != nil {
		t.Fatalf("List(flows) error = %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("flow count = %d, want 1", len(flows))
	}
	flowVersions, err := flowRepo.ListVersions(ctx, flow.ID)
	if err != nil {
		t.Fatalf("ListVersions(flow) error = %v", err)
	}
	if len(flowVersions) != 1 || flowVersions[0].Version != 1 {
		t.Fatalf("flow versions = %#v", flowVersions)
	}

	run := domain.FlowRun{ID: "run-pg", WorkspaceID: flow.WorkspaceID, FlowID: flow.ID, FlowVersion: 1, Status: domain.RunStatusQueued, CreatedAt: now, UpdatedAt: now, InputJSON: json.RawMessage(`{"customer_id":"cust-1"}`), InitiatedBy: "test", QueueName: "default", IdempotencyKey: "idem-1"}
	if err := runRepo.Create(ctx, run); err != nil {
		t.Fatalf("Create(run) error = %v", err)
	}
	claimedRun, err := runRepo.ClaimForExecution(ctx, run.ID, "worker-a", now.Add(time.Second), now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ClaimForExecution() error = %v", err)
	}
	if claimedRun.Status != domain.RunStatusRunning {
		t.Fatalf("claimed run status = %q, want running", claimedRun.Status)
	}
	if err := runRepo.Heartbeat(ctx, run.ID, "worker-a", now.Add(2*time.Second)); err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	finishedAt := now.Add(3 * time.Second)
	if err := runRepo.UpdateStatus(ctx, run.ID, domain.RunStatusSucceeded, claimedRun.StartedAt, &finishedAt, ""); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	persistedRun, err := runRepo.GetByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetByID(run) error = %v", err)
	}
	if persistedRun.Status != domain.RunStatusSucceeded {
		t.Fatalf("persisted run status = %q, want succeeded", persistedRun.Status)
	}
	foundRun, err := runRepo.FindByIdempotencyKey(ctx, flow.WorkspaceID, flow.ID, run.IdempotencyKey)
	if err != nil {
		t.Fatalf("FindByIdempotencyKey() error = %v", err)
	}
	if foundRun.ID != run.ID {
		t.Fatalf("idempotency lookup id = %q, want %q", foundRun.ID, run.ID)
	}
	runs, err := runRepo.List(ctx, repository.RunListFilter{WorkspaceID: flow.WorkspaceID, FlowID: flow.ID})
	if err != nil {
		t.Fatalf("List(runs) error = %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runs))
	}

	runStep := domain.FlowRunStep{ID: "run-pg-step-1", RunID: run.ID, StepName: "fetch", StepOrder: 0, Status: domain.RunStepStatusRunning, CreatedAt: now, UpdatedAt: now, RetryCount: 0}
	if err := runStepRepo.Create(ctx, runStep); err != nil {
		t.Fatalf("Create(run step) error = %v", err)
	}
	runStep.Status = domain.RunStepStatusSucceeded
	runStep.FinishedAt = ptrTime(now.Add(4 * time.Second))
	runStep.ExtractedValuesJSON = json.RawMessage(`{"id":"item-1"}`)
	if err := runStepRepo.Update(ctx, runStep); err != nil {
		t.Fatalf("Update(run step) error = %v", err)
	}
	runSteps, err := runStepRepo.ListByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRunID(run step) error = %v", err)
	}
	if len(runSteps) != 1 || runSteps[0].Status != domain.RunStepStatusSucceeded {
		t.Fatalf("run steps = %#v", runSteps)
	}

	event := domain.RunEvent{
		ID:          "run-pg-event-1",
		RunID:       run.ID,
		WorkspaceID: workspace.ID,
		FlowID:      flow.ID,
		EventType:   domain.RunEventTypeRunStarted,
		Level:       domain.RunEventLevelInfo,
		Message:     "run started",
		CreatedAt:   now.Add(500 * time.Millisecond),
	}
	persistedEvent, err := runEventRepo.Append(ctx, event)
	if err != nil {
		t.Fatalf("Append(run event) error = %v", err)
	}
	if persistedEvent.Sequence != 1 {
		t.Fatalf("persisted event sequence = %d, want 1", persistedEvent.Sequence)
	}
	events, err := runEventRepo.ListByRunID(ctx, run.ID, 0)
	if err != nil {
		t.Fatalf("ListByRunID(run events) error = %v", err)
	}
	if len(events) != 1 || events[0].Message != event.Message {
		t.Fatalf("run events = %#v", events)
	}
}

func ptrTime(value time.Time) *time.Time { return &value }

type postgresHarness struct {
	mu               sync.Mutex
	workspaces       map[domain.WorkspaceID]domain.Workspace
	requests         map[domain.SavedRequestID]domain.SavedRequest
	flows            map[domain.FlowID]domain.Flow
	flowSteps        map[domain.FlowID][]domain.FlowStep
	flowVersions     map[domain.FlowID]map[int]domain.Flow
	flowStepVersions map[domain.FlowID]map[int][]domain.FlowStep
	runs             map[domain.RunID]domain.FlowRun
	runSteps         map[domain.RunID][]domain.FlowRunStep
	runEvents        map[domain.RunID][]domain.RunEvent
}

var (
	registerHarnessDriver sync.Once
	harnessesMu           sync.Mutex
	harnesses             = map[string]*postgresHarness{}
)

func newPostgresHarnessDB(t *testing.T) *sql.DB {
	t.Helper()
	registerHarnessDriver.Do(func() { sql.Register("stageflow-postgres-harness", harnessDriver{}) })
	name := fmt.Sprintf("harness-%d", time.Now().UnixNano())
	harnessesMu.Lock()
	harnesses[name] = &postgresHarness{
		workspaces:       map[domain.WorkspaceID]domain.Workspace{},
		requests:         map[domain.SavedRequestID]domain.SavedRequest{},
		flows:            map[domain.FlowID]domain.Flow{},
		flowSteps:        map[domain.FlowID][]domain.FlowStep{},
		flowVersions:     map[domain.FlowID]map[int]domain.Flow{},
		flowStepVersions: map[domain.FlowID]map[int][]domain.FlowStep{},
		runs:             map[domain.RunID]domain.FlowRun{},
		runSteps:         map[domain.RunID][]domain.FlowRunStep{},
		runEvents:        map[domain.RunID][]domain.RunEvent{},
	}
	harnessesMu.Unlock()
	db, err := sql.Open("stageflow-postgres-harness", name)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	return db
}

type harnessDriver struct{}

func (harnessDriver) Open(name string) (driver.Conn, error) {
	harnessesMu.Lock()
	defer harnessesMu.Unlock()
	h, ok := harnesses[name]
	if !ok {
		return nil, fmt.Errorf("unknown harness %q", name)
	}
	return &harnessConn{h: h}, nil
}

type harnessConn struct{ h *postgresHarness }

func (c *harnessConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("not implemented") }
func (c *harnessConn) Close() error                        { return nil }
func (c *harnessConn) Begin() (driver.Tx, error)           { return harnessTx{}, nil }
func (c *harnessConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return harnessTx{}, nil
}
func (c *harnessConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.h.exec(query, args)
}
func (c *harnessConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.h.query(query, args)
}

type harnessTx struct{}

func (harnessTx) Commit() error   { return nil }
func (harnessTx) Rollback() error { return nil }

type harnessResult int64

func (r harnessResult) LastInsertId() (int64, error) { return 0, fmt.Errorf("unsupported") }
func (r harnessResult) RowsAffected() (int64, error) { return int64(r), nil }

type harnessRows struct {
	columns []string
	values  [][]driver.Value
	idx     int
}

func (r *harnessRows) Columns() []string { return r.columns }
func (r *harnessRows) Close() error      { return nil }
func (r *harnessRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.idx])
	r.idx++
	return nil
}

func (h *postgresHarness) exec(query string, args []driver.NamedValue) (driver.Result, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case strings.Contains(query, "INSERT INTO workspaces"):
		var policy domain.WorkspacePolicy
		if err := json.Unmarshal(asBytes(args[6]), &policy); err != nil {
			return nil, err
		}
		workspace := domain.Workspace{
			ID:          domain.WorkspaceID(asString(args[0])),
			Name:        asString(args[1]),
			Slug:        asString(args[2]),
			Description: asString(args[3]),
			OwnerTeam:   asString(args[4]),
			Status:      domain.WorkspaceStatus(asString(args[5])),
			Policy:      policy,
			CreatedAt:   asTime(args[7]),
			UpdatedAt:   asTime(args[8]),
		}
		h.workspaces[workspace.ID] = workspace
		return harnessResult(1), nil
	case strings.Contains(query, "UPDATE workspaces"):
		id := domain.WorkspaceID(asString(args[0]))
		workspace, ok := h.workspaces[id]
		if !ok {
			return harnessResult(0), nil
		}
		var policy domain.WorkspacePolicy
		if err := json.Unmarshal(asBytes(args[6]), &policy); err != nil {
			return nil, err
		}
		workspace.Name = asString(args[1])
		workspace.Slug = asString(args[2])
		workspace.Description = asString(args[3])
		workspace.OwnerTeam = asString(args[4])
		workspace.Status = domain.WorkspaceStatus(asString(args[5]))
		workspace.Policy = policy
		workspace.UpdatedAt = asTime(args[7])
		h.workspaces[id] = workspace
		return harnessResult(1), nil
	case strings.Contains(query, "DELETE FROM workspace_variables"):
		workspaceID := domain.WorkspaceID(asString(args[0]))
		workspace, ok := h.workspaces[workspaceID]
		if ok {
			workspace.Variables = nil
			h.workspaces[workspaceID] = workspace
		}
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO workspace_variables"):
		workspaceID := domain.WorkspaceID(asString(args[0]))
		workspace, ok := h.workspaces[workspaceID]
		if !ok {
			return nil, fmt.Errorf("unknown workspace %q", workspaceID)
		}
		workspace.Variables = append(workspace.Variables, domain.WorkspaceVariable{Name: asString(args[1]), Value: asString(args[2])})
		h.workspaces[workspaceID] = workspace
		return harnessResult(1), nil
	case strings.Contains(query, "DELETE FROM workspace_secrets"):
		workspaceID := domain.WorkspaceID(asString(args[0]))
		workspace, ok := h.workspaces[workspaceID]
		if ok {
			workspace.Secrets = nil
			h.workspaces[workspaceID] = workspace
		}
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO workspace_secrets"):
		workspaceID := domain.WorkspaceID(asString(args[0]))
		workspace, ok := h.workspaces[workspaceID]
		if !ok {
			return nil, fmt.Errorf("unknown workspace %q", workspaceID)
		}
		workspace.Secrets = append(workspace.Secrets, domain.WorkspaceSecret{Name: asString(args[1]), Value: asString(args[2])})
		h.workspaces[workspaceID] = workspace
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO saved_requests"):
		request := domain.SavedRequest{
			ID:          domain.SavedRequestID(asString(args[0])),
			WorkspaceID: domain.WorkspaceID(asString(args[1])),
			Name:        asString(args[2]),
			Description: asString(args[3]),
			CreatedAt:   asTime(args[5]),
			UpdatedAt:   asTime(args[6]),
		}
		if err := json.Unmarshal(asBytes(args[4]), &request.RequestSpec); err != nil {
			return nil, err
		}
		h.requests[request.ID] = request
		return harnessResult(1), nil
	case strings.Contains(query, "UPDATE saved_requests"):
		id := domain.SavedRequestID(asString(args[0]))
		request, ok := h.requests[id]
		if !ok {
			return harnessResult(0), nil
		}
		request.WorkspaceID = domain.WorkspaceID(asString(args[1]))
		request.Name = asString(args[2])
		request.Description = asString(args[3])
		if err := json.Unmarshal(asBytes(args[4]), &request.RequestSpec); err != nil {
			return nil, err
		}
		request.UpdatedAt = asTime(args[5])
		h.requests[id] = request
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flows"):
		flow := domain.Flow{ID: asFlowID(args[0]), WorkspaceID: domain.WorkspaceID(asString(args[1])), Name: asString(args[2]), Description: asString(args[3]), Version: asInt(args[4]), Status: domain.FlowStatus(asString(args[5])), CreatedAt: asTime(args[6]), UpdatedAt: asTime(args[7])}
		h.flows[flow.ID] = flow
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flow_versions"):
		flow := domain.Flow{ID: asFlowID(args[0]), WorkspaceID: domain.WorkspaceID(asString(args[1])), Version: asInt(args[2]), Name: asString(args[3]), Description: asString(args[4]), Status: domain.FlowStatus(asString(args[5])), CreatedAt: asTime(args[6]), UpdatedAt: asTime(args[7])}
		if _, ok := h.flowVersions[flow.ID]; !ok {
			h.flowVersions[flow.ID] = map[int]domain.Flow{}
		}
		h.flowVersions[flow.ID][flow.Version] = flow
		return harnessResult(1), nil
	case strings.Contains(query, "UPDATE flows"):
		id := asFlowID(args[0])
		flow, ok := h.flows[id]
		if !ok {
			return harnessResult(0), nil
		}
		flow.WorkspaceID, flow.Name, flow.Description, flow.Version, flow.Status, flow.UpdatedAt = domain.WorkspaceID(asString(args[1])), asString(args[2]), asString(args[3]), asInt(args[4]), domain.FlowStatus(asString(args[5])), asTime(args[6])
		h.flows[id] = flow
		return harnessResult(1), nil
	case strings.Contains(query, "DELETE FROM flow_steps"):
		delete(h.flowSteps, asFlowID(args[0]))
		return harnessResult(1), nil
	case strings.Contains(query, "DELETE FROM flow_step_versions"):
		flowID, version := asFlowID(args[0]), asInt(args[1])
		if versions, ok := h.flowStepVersions[flowID]; ok {
			delete(versions, version)
		}
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flow_steps"):
		step, err := decodeFlowStepArgs(args)
		if err != nil {
			return nil, err
		}
		h.flowSteps[step.FlowID] = append(h.flowSteps[step.FlowID], step)
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flow_step_versions"):
		step, err := decodeVersionedFlowStepArgs(args)
		if err != nil {
			return nil, err
		}
		version := asInt(args[2])
		if _, ok := h.flowStepVersions[step.FlowID]; !ok {
			h.flowStepVersions[step.FlowID] = map[int][]domain.FlowStep{}
		}
		h.flowStepVersions[step.FlowID][version] = append(h.flowStepVersions[step.FlowID][version], step)
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flow_runs"):
		run := domain.FlowRun{
			ID:             asRunID(args[0]),
			WorkspaceID:    domain.WorkspaceID(asString(args[1])),
			TargetType:     domain.RunTargetType(asString(args[2])),
			FlowID:         asFlowID(args[3]),
			FlowVersion:    asInt(args[4]),
			SavedRequestID: domain.SavedRequestID(asString(args[5])),
			Status:         domain.RunStatus(asString(args[6])),
			CreatedAt:      asTime(args[7]),
			UpdatedAt:      asTime(args[8]),
			StartedAt:      asTimePtr(args[9]),
			FinishedAt:     asTimePtr(args[10]),
			InputJSON:      asBytes(args[11]),
			InitiatedBy:    asString(args[12]),
			QueueName:      asString(args[13]),
			IdempotencyKey: asString(args[14]),
			ClaimedBy:      asString(args[15]),
			HeartbeatAt:    asTimePtr(args[16]),
			ErrorMessage:   asString(args[17]),
		}
		h.runs[run.ID] = run
		return harnessResult(1), nil
	case strings.Contains(query, "SET status = $2,") && strings.Contains(query, "WHERE id = $1") && strings.Contains(query, "flow_runs"):
		run, ok := h.runs[asRunID(args[0])]
		if !ok {
			return harnessResult(0), nil
		}
		run.Status = domain.RunStatus(asString(args[1]))
		if startedAt := asTimePtr(args[2]); startedAt != nil {
			run.StartedAt = startedAt
		}
		if finishedAt := asTimePtr(args[3]); finishedAt != nil {
			run.FinishedAt = finishedAt
		}
		run.ErrorMessage = asString(args[4])
		run.UpdatedAt = time.Now().UTC()
		if run.Status != domain.RunStatusRunning {
			run.ClaimedBy = ""
			run.HeartbeatAt = nil
		}
		h.runs[run.ID] = run
		return harnessResult(1), nil
	case strings.Contains(query, "SET heartbeat_at = $3"):
		run, ok := h.runs[asRunID(args[0])]
		if !ok || run.ClaimedBy != asString(args[1]) || run.Status != domain.RunStatusRunning {
			return harnessResult(0), nil
		}
		now := asTime(args[2])
		run.HeartbeatAt = &now
		run.UpdatedAt = now
		h.runs[run.ID] = run
		return harnessResult(1), nil
	case strings.Contains(query, "INSERT INTO flow_run_steps"):
		step := domain.FlowRunStep{ID: domain.RunStepID(asString(args[0])), RunID: asRunID(args[1]), StepName: asString(args[2]), StepOrder: asInt(args[3]), Status: domain.RunStepStatus(asString(args[4])), RequestSnapshotJSON: asBytes(args[5]), ResponseSnapshotJSON: asBytes(args[6]), ExtractedValuesJSON: asBytes(args[7]), AttemptHistoryJSON: asBytes(args[8]), RetryCount: asInt(args[9]), CreatedAt: asTime(args[10]), UpdatedAt: asTime(args[11]), StartedAt: asTimePtr(args[12]), FinishedAt: asTimePtr(args[13]), ErrorMessage: asString(args[14])}
		h.runSteps[step.RunID] = append(h.runSteps[step.RunID], step)
		return harnessResult(1), nil
	case strings.Contains(query, "UPDATE flow_run_steps"):
		stepID := domain.RunStepID(asString(args[0]))
		for runID, steps := range h.runSteps {
			for i, step := range steps {
				if step.ID != stepID {
					continue
				}
				step.StepName, step.StepOrder, step.Status = asString(args[1]), asInt(args[2]), domain.RunStepStatus(asString(args[3]))
				step.RequestSnapshotJSON, step.ResponseSnapshotJSON, step.ExtractedValuesJSON, step.AttemptHistoryJSON = asBytes(args[4]), asBytes(args[5]), asBytes(args[6]), asBytes(args[7])
				step.RetryCount, step.UpdatedAt, step.StartedAt, step.FinishedAt, step.ErrorMessage = asInt(args[8]), asTime(args[9]), asTimePtr(args[10]), asTimePtr(args[11]), asString(args[12])
				steps[i] = step
				h.runSteps[runID] = steps
				return harnessResult(1), nil
			}
		}
		return harnessResult(0), nil
	default:
		return nil, fmt.Errorf("unsupported exec query: %s", query)
	}
}

func (h *postgresHarness) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case strings.Contains(query, "FROM workspaces") && strings.Contains(query, "WHERE id = $1"):
		workspace, ok := h.workspaces[domain.WorkspaceID(asString(args[0]))]
		if !ok {
			return &harnessRows{}, nil
		}
		return &harnessRows{columns: []string{"id", "name", "slug", "description", "owner_team", "status", "policy_json", "created_at", "updated_at"}, values: [][]driver.Value{workspaceRow(workspace)}}, nil
	case strings.Contains(query, "FROM workspaces") && strings.Contains(query, "WHERE slug = $1"):
		for _, workspace := range h.workspaces {
			if workspace.Slug == asString(args[0]) {
				return &harnessRows{columns: []string{"id", "name", "slug", "description", "owner_team", "status", "policy_json", "created_at", "updated_at"}, values: [][]driver.Value{workspaceRow(workspace)}}, nil
			}
		}
		return &harnessRows{}, nil
	case strings.Contains(query, "FROM workspaces") && strings.Contains(query, "WHERE 1=1"):
		rows := [][]driver.Value{}
		for _, workspace := range h.workspaces {
			if len(args) > 0 && strings.Contains(query, "LOWER(name) LIKE") && !strings.Contains(strings.ToLower(workspace.Name), strings.Trim(strings.ToLower(asString(args[0])), "%")) {
				continue
			}
			rows = append(rows, workspaceRow(workspace))
		}
		return &harnessRows{columns: []string{"id", "name", "slug", "description", "owner_team", "status", "policy_json", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "FROM workspace_variables"):
		workspace := h.workspaces[domain.WorkspaceID(asString(args[0]))]
		rows := make([][]driver.Value, 0, len(workspace.Variables))
		for _, item := range workspace.Variables {
			rows = append(rows, []driver.Value{item.Name, item.Value})
		}
		return &harnessRows{columns: []string{"name", "value"}, values: rows}, nil
	case strings.Contains(query, "FROM workspace_secrets"):
		workspace := h.workspaces[domain.WorkspaceID(asString(args[0]))]
		rows := make([][]driver.Value, 0, len(workspace.Secrets))
		for _, item := range workspace.Secrets {
			rows = append(rows, []driver.Value{item.Name, item.Value})
		}
		return &harnessRows{columns: []string{"name", "value"}, values: rows}, nil
	case strings.Contains(query, "FROM saved_requests") && strings.Contains(query, "WHERE id = $1"):
		request, ok := h.requests[domain.SavedRequestID(asString(args[0]))]
		if !ok {
			return &harnessRows{}, nil
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "name", "description", "request_spec", "created_at", "updated_at"}, values: [][]driver.Value{savedRequestRow(request)}}, nil
	case strings.Contains(query, "FROM saved_requests") && strings.Contains(query, "WHERE 1=1"):
		rows := [][]driver.Value{}
		for _, request := range h.requests {
			argIdx := 0
			if strings.Contains(query, "workspace_id = $1") && string(request.WorkspaceID) != asString(args[0]) {
				continue
			}
			if strings.Contains(query, "workspace_id = $1") {
				argIdx = 1
			}
			if len(args) > argIdx && strings.Contains(query, "LOWER(name) LIKE") && !strings.Contains(strings.ToLower(request.Name), strings.Trim(strings.ToLower(asString(args[argIdx])), "%")) {
				continue
			}
			rows = append(rows, savedRequestRow(request))
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "name", "description", "request_spec", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "FROM flows") && strings.Contains(query, "WHERE id = $1"):
		flow, ok := h.flows[asFlowID(args[0])]
		if !ok {
			return &harnessRows{}, nil
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "name", "description", "version", "status", "created_at", "updated_at"}, values: [][]driver.Value{{string(flow.ID), string(flow.WorkspaceID), flow.Name, flow.Description, int64(flow.Version), string(flow.Status), flow.CreatedAt, flow.UpdatedAt}}}, nil
	case strings.Contains(query, "FROM flow_versions") && strings.Contains(query, "WHERE flow_id = $1 AND version = $2"):
		versions := h.flowVersions[asFlowID(args[0])]
		flow, ok := versions[asInt(args[1])]
		if !ok {
			return &harnessRows{}, nil
		}
		return &harnessRows{columns: []string{"flow_id", "workspace_id", "name", "description", "version", "status", "created_at", "updated_at"}, values: [][]driver.Value{{string(flow.ID), string(flow.WorkspaceID), flow.Name, flow.Description, int64(flow.Version), string(flow.Status), flow.CreatedAt, flow.UpdatedAt}}}, nil
	case strings.Contains(query, "FROM flow_versions") && strings.Contains(query, "WHERE flow_id = $1"):
		versions := h.flowVersions[asFlowID(args[0])]
		rows := [][]driver.Value{}
		for _, flow := range versions {
			rows = append(rows, []driver.Value{string(flow.ID), string(flow.WorkspaceID), flow.Name, flow.Description, int64(flow.Version), string(flow.Status), flow.CreatedAt, flow.UpdatedAt})
		}
		return &harnessRows{columns: []string{"flow_id", "workspace_id", "name", "description", "version", "status", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "FROM flows") && strings.Contains(query, "WHERE 1=1"):
		rows := [][]driver.Value{}
		for _, flow := range h.flows {
			if len(args) > 0 && strings.Contains(query, "workspace_id = $1") && string(flow.WorkspaceID) != asString(args[0]) {
				continue
			}
			nameArgIndex := 0
			if strings.Contains(query, "workspace_id = $1") {
				nameArgIndex = 1
			}
			if len(args) > nameArgIndex && strings.Contains(query, "LOWER(name) LIKE") && !strings.Contains(strings.ToLower(flow.Name), strings.Trim(strings.ToLower(asString(args[nameArgIndex])), "%")) {
				continue
			}
			rows = append(rows, []driver.Value{string(flow.ID), string(flow.WorkspaceID), flow.Name, flow.Description, int64(flow.Version), string(flow.Status), flow.CreatedAt, flow.UpdatedAt})
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "name", "description", "version", "status", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "FROM flow_steps"):
		steps := h.flowSteps[asFlowID(args[0])]
		rows := make([][]driver.Value, 0, len(steps))
		for _, step := range steps {
			requestSpec, _ := json.Marshal(step.RequestSpec)
			requestSpecOverride, _ := json.Marshal(step.RequestSpecOverride)
			extractionSpec, _ := json.Marshal(step.ExtractionSpec)
			assertionSpec, _ := json.Marshal(step.AssertionSpec)
			rows = append(rows, []driver.Value{string(step.ID), string(step.FlowID), int64(step.OrderIndex), step.Name, string(step.Type()), valueOrNilString(step.SavedRequestID), requestSpec, requestSpecOverride, extractionSpec, assertionSpec, step.CreatedAt, step.UpdatedAt})
		}
		return &harnessRows{columns: []string{"id", "flow_id", "order_index", "name", "step_type", "saved_request_id", "request_spec", "request_spec_override", "extraction_spec", "assertion_spec", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "FROM flow_step_versions"):
		versions := h.flowStepVersions[asFlowID(args[0])]
		steps := versions[asInt(args[1])]
		rows := make([][]driver.Value, 0, len(steps))
		for _, step := range steps {
			requestSpec, _ := json.Marshal(step.RequestSpec)
			requestSpecOverride, _ := json.Marshal(step.RequestSpecOverride)
			extractionSpec, _ := json.Marshal(step.ExtractionSpec)
			assertionSpec, _ := json.Marshal(step.AssertionSpec)
			rows = append(rows, []driver.Value{string(step.ID), string(step.FlowID), int64(step.OrderIndex), step.Name, string(step.Type()), valueOrNilString(step.SavedRequestID), requestSpec, requestSpecOverride, extractionSpec, assertionSpec, step.CreatedAt, step.UpdatedAt})
		}
		return &harnessRows{columns: []string{"id", "flow_id", "order_index", "name", "step_type", "saved_request_id", "request_spec", "request_spec_override", "extraction_spec", "assertion_spec", "created_at", "updated_at"}, values: rows}, nil
	case strings.Contains(query, "RETURNING id, workspace_id, target_type, flow_id, flow_version, saved_request_id"):
		run, ok := h.runs[asRunID(args[0])]
		if !ok {
			return &harnessRows{}, nil
		}
		now := asTime(args[2])
		run.Status = domain.RunStatusRunning
		run.ClaimedBy = asString(args[1])
		run.HeartbeatAt = &now
		if run.StartedAt == nil {
			run.StartedAt = &now
		}
		run.FinishedAt = nil
		run.ErrorMessage = ""
		run.UpdatedAt = now
		h.runs[run.ID] = run
		return &harnessRows{columns: []string{"id", "workspace_id", "target_type", "flow_id", "flow_version", "saved_request_id", "status", "created_at", "updated_at", "started_at", "finished_at", "input_json", "initiated_by", "queue_name", "idempotency_key", "claimed_by", "heartbeat_at", "error_message"}, values: [][]driver.Value{flowRunRow(run)}}, nil
	case strings.Contains(query, "FROM flow_runs") && strings.Contains(query, "WHERE workspace_id = $1 AND target_type = 'flow' AND flow_id = $2 AND idempotency_key = $3"):
		for _, run := range h.runs {
			if string(run.WorkspaceID) == asString(args[0]) && run.Target() == domain.RunTargetTypeFlow && run.FlowID == asFlowID(args[1]) && run.IdempotencyKey == asString(args[2]) {
				return &harnessRows{columns: []string{"id", "workspace_id", "target_type", "flow_id", "flow_version", "saved_request_id", "status", "created_at", "updated_at", "started_at", "finished_at", "input_json", "initiated_by", "queue_name", "idempotency_key", "claimed_by", "heartbeat_at", "error_message"}, values: [][]driver.Value{flowRunRow(run)}}, nil
			}
		}
		return &harnessRows{}, nil
	case strings.Contains(query, "FROM flow_runs") && strings.Contains(query, "WHERE id = $1"):
		run, ok := h.runs[asRunID(args[0])]
		if !ok {
			return &harnessRows{}, nil
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "target_type", "flow_id", "flow_version", "saved_request_id", "status", "created_at", "updated_at", "started_at", "finished_at", "input_json", "initiated_by", "queue_name", "idempotency_key", "claimed_by", "heartbeat_at", "error_message"}, values: [][]driver.Value{flowRunRow(run)}}, nil
	case strings.Contains(query, "FROM flow_runs") && strings.Contains(query, "WHERE 1=1"):
		rows := [][]driver.Value{}
		for _, run := range h.runs {
			argIdx := 0
			if strings.Contains(query, "workspace_id = $1") && string(run.WorkspaceID) != asString(args[0]) {
				continue
			}
			if strings.Contains(query, "workspace_id = $1") {
				argIdx = 1
			}
			if len(args) > argIdx && strings.Contains(query, fmt.Sprintf("flow_id = $%d", argIdx+1)) && run.FlowID != asFlowID(args[argIdx]) {
				continue
			}
			rows = append(rows, flowRunRow(run))
		}
		return &harnessRows{columns: []string{"id", "workspace_id", "target_type", "flow_id", "flow_version", "saved_request_id", "status", "created_at", "updated_at", "started_at", "finished_at", "input_json", "initiated_by", "queue_name", "idempotency_key", "claimed_by", "heartbeat_at", "error_message"}, values: rows}, nil
	case strings.Contains(query, "FROM flow_run_steps"):
		steps := h.runSteps[asRunID(args[0])]
		rows := make([][]driver.Value, 0, len(steps))
		for _, step := range steps {
			rows = append(rows, []driver.Value{string(step.ID), string(step.RunID), step.StepName, int64(step.StepOrder), string(step.Status), []byte(step.RequestSnapshotJSON), []byte(step.ResponseSnapshotJSON), []byte(step.ExtractedValuesJSON), []byte(step.AttemptHistoryJSON), int64(step.RetryCount), step.CreatedAt, step.UpdatedAt, valueOrNil(step.StartedAt), valueOrNil(step.FinishedAt), step.ErrorMessage})
		}
		return &harnessRows{columns: []string{"id", "run_id", "step_name", "step_order", "status", "request_snapshot_json", "response_snapshot_json", "extracted_values_json", "attempt_history_json", "retry_count", "created_at", "updated_at", "started_at", "finished_at", "error_message"}, values: rows}, nil
	case strings.Contains(query, "INSERT INTO run_events"):
		runID := asRunID(args[1])
		event := domain.RunEvent{
			ID:             domain.RunEventID(asString(args[0])),
			RunID:          runID,
			WorkspaceID:    domain.WorkspaceID(asString(args[2])),
			FlowID:         asFlowID(args[3]),
			SavedRequestID: domain.SavedRequestID(asString(args[4])),
			Sequence:       int64(len(h.runEvents[runID]) + 1),
			EventType:      domain.RunEventType(asString(args[6])),
			Level:          domain.RunEventLevel(asString(args[7])),
			StepName:       asString(args[8]),
			Message:        asString(args[11]),
			DetailsJSON:    asBytes(args[12]),
			CreatedAt:      asTime(args[13]),
		}
		if value := asIntPtr(args[9]); value != nil {
			event.StepOrder = value
		}
		if value := asIntPtr(args[10]); value != nil {
			event.Attempt = value
		}
		h.runEvents[runID] = append(h.runEvents[runID], event)
		return &harnessRows{columns: []string{"sequence"}, values: [][]driver.Value{{event.Sequence}}}, nil
	case strings.Contains(query, "FROM run_events"):
		runID := asRunID(args[0])
		after := asInt64(args[1])
		events := h.runEvents[runID]
		rows := make([][]driver.Value, 0, len(events))
		for _, event := range events {
			if event.Sequence <= after {
				continue
			}
			rows = append(rows, runEventRow(event))
		}
		return &harnessRows{columns: []string{"id", "run_id", "workspace_id", "flow_id", "saved_request_id", "sequence", "event_type", "level", "step_name", "step_order", "attempt", "message", "details_json", "created_at"}, values: rows}, nil
	default:
		return nil, fmt.Errorf("unsupported query: %s", query)
	}
}

func decodeFlowStepArgs(args []driver.NamedValue) (domain.FlowStep, error) {
	step := domain.FlowStep{ID: domain.FlowStepID(asString(args[0])), FlowID: asFlowID(args[1]), OrderIndex: asInt(args[2]), Name: asString(args[3]), StepType: domain.FlowStepType(asString(args[4])), SavedRequestID: domain.SavedRequestID(asString(args[5])), CreatedAt: asTime(args[10]), UpdatedAt: asTime(args[11])}
	if err := json.Unmarshal(asBytes(args[6]), &step.RequestSpec); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[7]), &step.RequestSpecOverride); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[8]), &step.ExtractionSpec); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[9]), &step.AssertionSpec); err != nil {
		return domain.FlowStep{}, err
	}
	return step, nil
}

func decodeVersionedFlowStepArgs(args []driver.NamedValue) (domain.FlowStep, error) {
	step := domain.FlowStep{ID: domain.FlowStepID(asString(args[0])), FlowID: asFlowID(args[1]), OrderIndex: asInt(args[3]), Name: asString(args[4]), StepType: domain.FlowStepType(asString(args[5])), SavedRequestID: domain.SavedRequestID(asString(args[6])), CreatedAt: asTime(args[11]), UpdatedAt: asTime(args[12])}
	if err := json.Unmarshal(asBytes(args[7]), &step.RequestSpec); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[8]), &step.RequestSpecOverride); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[9]), &step.ExtractionSpec); err != nil {
		return domain.FlowStep{}, err
	}
	if err := json.Unmarshal(asBytes(args[10]), &step.AssertionSpec); err != nil {
		return domain.FlowStep{}, err
	}
	return step, nil
}

func workspaceRow(workspace domain.Workspace) []driver.Value {
	policy, _ := json.Marshal(workspace.Policy)
	return []driver.Value{
		string(workspace.ID),
		workspace.Name,
		workspace.Slug,
		workspace.Description,
		workspace.OwnerTeam,
		string(workspace.Status),
		policy,
		workspace.CreatedAt,
		workspace.UpdatedAt,
	}
}

func savedRequestRow(request domain.SavedRequest) []driver.Value {
	requestSpec, _ := json.Marshal(request.RequestSpec)
	return []driver.Value{
		string(request.ID),
		string(request.WorkspaceID),
		request.Name,
		request.Description,
		requestSpec,
		request.CreatedAt,
		request.UpdatedAt,
	}
}

func flowRunRow(run domain.FlowRun) []driver.Value {
	return []driver.Value{
		string(run.ID),
		string(run.WorkspaceID),
		string(run.Target()),
		valueOrNilString(run.FlowID),
		valueOrNilInt(run.FlowVersion),
		valueOrNilString(run.SavedRequestID),
		string(run.Status),
		run.CreatedAt,
		run.UpdatedAt,
		valueOrNil(run.StartedAt),
		valueOrNil(run.FinishedAt),
		[]byte(run.InputJSON),
		run.InitiatedBy,
		run.QueueName,
		run.IdempotencyKey,
		run.ClaimedBy,
		valueOrNil(run.HeartbeatAt),
		run.ErrorMessage,
	}
}

func runEventRow(event domain.RunEvent) []driver.Value {
	return []driver.Value{
		string(event.ID),
		string(event.RunID),
		string(event.WorkspaceID),
		valueOrNilString(event.FlowID),
		valueOrNilString(event.SavedRequestID),
		event.Sequence,
		string(event.EventType),
		string(event.Level),
		valueOrNilString(event.StepName),
		valueOrNil(event.StepOrder),
		valueOrNil(event.Attempt),
		event.Message,
		[]byte(event.DetailsJSON),
		event.CreatedAt,
	}
}

func valueOrNilString[T ~string](value T) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func valueOrNilInt(value int) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func asString(value driver.NamedValue) string {
	if value.Value == nil {
		return ""
	}
	switch v := value.Value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
func asBytes(value driver.NamedValue) []byte {
	if value.Value == nil {
		return nil
	}
	switch v := value.Value.(type) {
	case []byte:
		return append([]byte(nil), v...)
	case string:
		return []byte(v)
	default:
		return []byte(fmt.Sprint(v))
	}
}
func asInt(value driver.NamedValue) int {
	switch v := value.Value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func asInt64(value driver.NamedValue) int64 {
	switch v := value.Value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func asIntPtr(value driver.NamedValue) *int {
	if value.Value == nil {
		return nil
	}
	v := asInt(value)
	return &v
}
func asTime(value driver.NamedValue) time.Time {
	if t, ok := value.Value.(time.Time); ok {
		return t.UTC()
	}
	return time.Time{}
}
func asTimePtr(value driver.NamedValue) *time.Time {
	if value.Value == nil {
		return nil
	}
	t := asTime(value)
	return &t
}
func asFlowID(value driver.NamedValue) domain.FlowID { return domain.FlowID(asString(value)) }
func asRunID(value driver.NamedValue) domain.RunID   { return domain.RunID(asString(value)) }
func valueOrNil[T any](value *T) any {
	if value == nil {
		return nil
	}
	return *value
}
