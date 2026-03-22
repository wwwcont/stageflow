package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/config"
	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

type fakeWorkspaceManagementUseCase struct {
	createFn       func(usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error)
	getFn          func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error)
	listFn         func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error)
	updateFn       func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error)
	archiveFn      func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error)
	unarchiveFn    func(usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error)
	policyFn       func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error)
	variablesFn    func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error)
	putSecretFn    func(usecase.PutWorkspaceSecretCommand) error
	listSecretFn   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error)
	deleteSecretFn func(usecase.DeleteWorkspaceSecretCommand) error
}

func (f fakeWorkspaceManagementUseCase) CreateWorkspace(_ context.Context, cmd usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
	return f.createFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) GetWorkspace(_ context.Context, query usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) {
	return f.getFn(query)
}
func (f fakeWorkspaceManagementUseCase) ListWorkspaces(_ context.Context, query usecase.ListWorkspacesQuery) ([]domain.Workspace, error) {
	return f.listFn(query)
}
func (f fakeWorkspaceManagementUseCase) UpdateWorkspace(_ context.Context, cmd usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
	return f.updateFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) ArchiveWorkspace(_ context.Context, cmd usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
	return f.archiveFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) UnarchiveWorkspace(_ context.Context, cmd usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
	if f.unarchiveFn == nil {
		return usecase.WorkspaceView{}, nil
	}
	return f.unarchiveFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) UpdateWorkspacePolicy(_ context.Context, cmd usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
	return f.policyFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) UpdateWorkspaceVariables(_ context.Context, cmd usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
	return f.variablesFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) PutWorkspaceSecret(_ context.Context, cmd usecase.PutWorkspaceSecretCommand) error {
	return f.putSecretFn(cmd)
}
func (f fakeWorkspaceManagementUseCase) ListWorkspaceSecrets(_ context.Context, query usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) {
	return f.listSecretFn(query)
}
func (f fakeWorkspaceManagementUseCase) DeleteWorkspaceSecret(_ context.Context, cmd usecase.DeleteWorkspaceSecretCommand) error {
	return f.deleteSecretFn(cmd)
}

type fakeSavedRequestManagementUseCase struct{}

func (fakeSavedRequestManagementUseCase) CreateSavedRequest(context.Context, usecase.CreateSavedRequestCommand) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, nil
}
func (fakeSavedRequestManagementUseCase) UpdateSavedRequest(context.Context, usecase.UpdateSavedRequestCommand) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, nil
}
func (fakeSavedRequestManagementUseCase) GetSavedRequest(context.Context, usecase.GetSavedRequestQuery) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, nil
}
func (fakeSavedRequestManagementUseCase) ListSavedRequests(context.Context, usecase.ListSavedRequestsQuery) ([]domain.SavedRequest, error) {
	return nil, nil
}

type fakeFlowManagementUseCase struct{}

func (fakeFlowManagementUseCase) CreateFlow(context.Context, usecase.CreateFlowCommand) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, nil
}
func (fakeFlowManagementUseCase) UpdateFlow(context.Context, usecase.UpdateFlowCommand) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, nil
}
func (fakeFlowManagementUseCase) GetFlow(context.Context, usecase.GetFlowQuery) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, nil
}
func (fakeFlowManagementUseCase) ListFlows(context.Context, usecase.ListFlowsQuery) ([]domain.Flow, error) {
	return nil, nil
}
func (fakeFlowManagementUseCase) ValidateFlow(context.Context, usecase.ValidateFlowCommand) (usecase.FlowValidationResult, error) {
	return usecase.FlowValidationResult{}, nil
}

type fakeRunService struct {
	launchFlowFn         func(usecase.LaunchFlowInput) (domain.FlowRun, error)
	runSavedRequestFn    func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error)
	getRunStatusFn       func(usecase.GetRunStatusQuery) (usecase.RunStatusView, error)
	launchSavedRequestFn func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error)
	listRunsFn           func(usecase.ListRunsQuery) ([]domain.FlowRun, error)
	rerunFn              func(usecase.RerunInput) (domain.FlowRun, error)
}

func (f fakeRunService) LaunchFlow(_ context.Context, input usecase.LaunchFlowInput) (domain.FlowRun, error) {
	return f.launchFlowFn(input)
}
func (f fakeRunService) LaunchSavedRequest(_ context.Context, input usecase.LaunchSavedRequestInput) (domain.FlowRun, error) {
	return f.launchSavedRequestFn(input)
}
func (f fakeRunService) RunSavedRequest(_ context.Context, input usecase.LaunchSavedRequestInput) (domain.FlowRun, error) {
	return f.runSavedRequestFn(input)
}
func (f fakeRunService) GetRunStatus(_ context.Context, query usecase.GetRunStatusQuery) (usecase.RunStatusView, error) {
	return f.getRunStatusFn(query)
}
func (f fakeRunService) ListRuns(_ context.Context, query usecase.ListRunsQuery) ([]domain.FlowRun, error) {
	if f.listRunsFn == nil {
		return nil, nil
	}
	return f.listRunsFn(query)
}
func (f fakeRunService) Rerun(_ context.Context, input usecase.RerunInput) (domain.FlowRun, error) {
	return f.rerunFn(input)
}

type fakeCurlImportUseCase struct{}

func (fakeCurlImportUseCase) ImportCurl(context.Context, usecase.ImportCurlInput) (usecase.ImportCurlResult, error) {
	return usecase.ImportCurlResult{}, nil
}

func testHTTPServer(workspaceSvc usecase.WorkspaceManagementUseCase, savedRequestSvc usecase.SavedRequestManagementUseCase, flowSvc usecase.FlowManagementUseCase, runSvc usecase.RunService) *Server {
	cfg := config.Config{ServiceName: "stageflow", Environment: "test", HTTP: config.HTTPConfig{Address: ":0"}}
	return NewServer(cfg, zap.NewNop(), nil, workspaceSvc, savedRequestSvc, flowSvc, runSvc, fakeCurlImportUseCase{}, nil)
}

func TestServer_WorkspaceFirst_CreateWorkspace(t *testing.T) {
	now := time.Date(2026, 3, 21, 16, 0, 0, 0, time.UTC)
	server := testHTTPServer(fakeWorkspaceManagementUseCase{
		createFn: func(cmd usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
			if cmd.WorkspaceID != "workspace-a" {
				t.Fatalf("WorkspaceID = %q", cmd.WorkspaceID)
			}
			if cmd.Policy.DefaultTimeoutMS != 1000 {
				t.Fatalf("DefaultTimeoutMS = %d", cmd.Policy.DefaultTimeoutMS)
			}
			return usecase.WorkspaceView{Workspace: domain.Workspace{
				ID:        cmd.WorkspaceID,
				Name:      cmd.Name,
				Slug:      cmd.Slug,
				OwnerTeam: cmd.OwnerTeam,
				Status:    cmd.Status,
				Policy:    cmd.Policy,
				CreatedAt: now,
				UpdatedAt: now,
			}}, nil
		},
		getFn:  func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) { return usecase.WorkspaceView{}, nil },
		listFn: func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error) { return nil, nil },
		updateFn: func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		archiveFn: func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		unarchiveFn: func(usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		policyFn: func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		variablesFn: func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		putSecretFn:    func(usecase.PutWorkspaceSecretCommand) error { return nil },
		listSecretFn:   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) { return nil, nil },
		deleteSecretFn: func(usecase.DeleteWorkspaceSecretCommand) error { return nil },
	}, fakeSavedRequestManagementUseCase{}, fakeFlowManagementUseCase{}, fakeRunService{
		launchFlowFn:         func(usecase.LaunchFlowInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		launchSavedRequestFn: func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		runSavedRequestFn:    func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		getRunStatusFn:       func(usecase.GetRunStatusQuery) (usecase.RunStatusView, error) { return usecase.RunStatusView{}, nil },
		rerunFn:              func(usecase.RerunInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
	})

	body := []byte(`{"id":"workspace-a","name":"Workspace A","slug":"workspace-a","owner_team":"platform","status":"active","policy":{"allowed_hosts":["svc.internal"],"max_saved_requests":10,"max_flows":10,"max_steps_per_flow":10,"max_request_body_bytes":1024,"default_timeout_ms":1000,"max_run_duration_seconds":60,"default_retry_policy":{"enabled":false}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces", bytes.NewReader(body))
	resp := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusCreated)
	}
}

func TestServer_WorkspaceFirst_RunSavedRequestUsesPathWorkspaceScope(t *testing.T) {
	var got usecase.LaunchSavedRequestInput
	server := testHTTPServer(fakeWorkspaceManagementUseCase{
		createFn: func(usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		getFn:  func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) { return usecase.WorkspaceView{}, nil },
		listFn: func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error) { return nil, nil },
		updateFn: func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		archiveFn: func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		unarchiveFn: func(usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		policyFn: func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		variablesFn: func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		putSecretFn:    func(usecase.PutWorkspaceSecretCommand) error { return nil },
		listSecretFn:   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) { return nil, nil },
		deleteSecretFn: func(usecase.DeleteWorkspaceSecretCommand) error { return nil },
	}, fakeSavedRequestManagementUseCase{}, fakeFlowManagementUseCase{}, fakeRunService{
		launchFlowFn:         func(usecase.LaunchFlowInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		launchSavedRequestFn: func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		runSavedRequestFn: func(input usecase.LaunchSavedRequestInput) (domain.FlowRun, error) {
			got = input
			return domain.FlowRun{ID: "run-1", WorkspaceID: input.WorkspaceID, TargetType: domain.RunTargetTypeSavedRequest, SavedRequestID: input.SavedRequestID, Status: domain.RunStatusQueued, InitiatedBy: input.InitiatedBy}, nil
		},
		getRunStatusFn: func(usecase.GetRunStatusQuery) (usecase.RunStatusView, error) { return usecase.RunStatusView{}, nil },
		rerunFn:        func(usecase.RerunInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/workspace-a/requests/request-a/run", bytes.NewBufferString(`{"initiated_by":"alice","input":{"env":"prod"}}`))
	resp := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusAccepted)
	}
	if got.WorkspaceID != "workspace-a" || got.SavedRequestID != "request-a" {
		t.Fatalf("got = %#v", got)
	}
}

func TestServer_WorkspaceFirst_LaunchFlowUsesPathWorkspaceScope(t *testing.T) {
	var got usecase.LaunchFlowInput
	server := testHTTPServer(fakeWorkspaceManagementUseCase{
		createFn: func(usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		getFn:  func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) { return usecase.WorkspaceView{}, nil },
		listFn: func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error) { return nil, nil },
		updateFn: func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		archiveFn: func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		unarchiveFn: func(usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		policyFn: func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		variablesFn: func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		putSecretFn:    func(usecase.PutWorkspaceSecretCommand) error { return nil },
		listSecretFn:   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) { return nil, nil },
		deleteSecretFn: func(usecase.DeleteWorkspaceSecretCommand) error { return nil },
	}, fakeSavedRequestManagementUseCase{}, fakeFlowManagementUseCase{}, fakeRunService{
		launchFlowFn: func(input usecase.LaunchFlowInput) (domain.FlowRun, error) {
			got = input
			return domain.FlowRun{ID: "run-flow", WorkspaceID: input.WorkspaceID, TargetType: domain.RunTargetTypeFlow, FlowID: input.FlowID, FlowVersion: 1, Status: domain.RunStatusQueued, InitiatedBy: input.InitiatedBy}, nil
		},
		launchSavedRequestFn: func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		runSavedRequestFn:    func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		getRunStatusFn:       func(usecase.GetRunStatusQuery) (usecase.RunStatusView, error) { return usecase.RunStatusView{}, nil },
		rerunFn:              func(usecase.RerunInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/workspace-a/flows/flow-a/runs", bytes.NewBufferString(`{"initiated_by":"alice","input":{"customer_id":"cust-1"}}`))
	resp := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusAccepted)
	}
	if got.WorkspaceID != "workspace-a" || got.FlowID != "flow-a" {
		t.Fatalf("got = %#v", got)
	}
}

func TestServer_RunEndpoints_DoNotRequireWorkspaceQuery(t *testing.T) {
	server := testHTTPServer(fakeWorkspaceManagementUseCase{
		createFn: func(usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		getFn:  func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) { return usecase.WorkspaceView{}, nil },
		listFn: func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error) { return nil, nil },
		updateFn: func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		archiveFn: func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		unarchiveFn: func(usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		policyFn: func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		variablesFn: func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		putSecretFn:    func(usecase.PutWorkspaceSecretCommand) error { return nil },
		listSecretFn:   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) { return nil, nil },
		deleteSecretFn: func(usecase.DeleteWorkspaceSecretCommand) error { return nil },
	}, fakeSavedRequestManagementUseCase{}, fakeFlowManagementUseCase{}, fakeRunService{
		launchFlowFn:         func(usecase.LaunchFlowInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		launchSavedRequestFn: func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		runSavedRequestFn:    func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		getRunStatusFn: func(query usecase.GetRunStatusQuery) (usecase.RunStatusView, error) {
			if query.WorkspaceID != "" {
				t.Fatalf("WorkspaceID = %q, want empty", query.WorkspaceID)
			}
			return usecase.RunStatusView{Run: domain.FlowRun{ID: query.RunID, WorkspaceID: "workspace-a", Status: domain.RunStatusSucceeded, InitiatedBy: "alice"}}, nil
		},
		rerunFn: func(usecase.RerunInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-1", nil)
	resp := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	var body runResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if body.ID != "run-1" {
		t.Fatalf("ID = %q", body.ID)
	}
}

func TestServer_WorkspaceFirst_UnarchiveWorkspace(t *testing.T) {
	var got usecase.UnarchiveWorkspaceCommand
	server := testHTTPServer(fakeWorkspaceManagementUseCase{
		createFn: func(usecase.CreateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		getFn:  func(usecase.GetWorkspaceQuery) (usecase.WorkspaceView, error) { return usecase.WorkspaceView{}, nil },
		listFn: func(usecase.ListWorkspacesQuery) ([]domain.Workspace, error) { return nil, nil },
		updateFn: func(usecase.UpdateWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		archiveFn: func(usecase.ArchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		unarchiveFn: func(cmd usecase.UnarchiveWorkspaceCommand) (usecase.WorkspaceView, error) {
			got = cmd
			return usecase.WorkspaceView{Workspace: domain.Workspace{ID: cmd.WorkspaceID, Name: "Workspace A", Slug: "workspace-a", OwnerTeam: "platform", Status: domain.WorkspaceStatusActive, Policy: domain.WorkspacePolicy{AllowedHosts: []string{"svc.internal"}, MaxSavedRequests: 1, MaxFlows: 1, MaxStepsPerFlow: 1, MaxRequestBodyBytes: 128, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60}}}, nil
		},
		policyFn: func(usecase.UpdateWorkspacePolicyCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		variablesFn: func(usecase.UpdateWorkspaceVariablesCommand) (usecase.WorkspaceView, error) {
			return usecase.WorkspaceView{}, nil
		},
		putSecretFn:    func(usecase.PutWorkspaceSecretCommand) error { return nil },
		listSecretFn:   func(usecase.ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) { return nil, nil },
		deleteSecretFn: func(usecase.DeleteWorkspaceSecretCommand) error { return nil },
	}, fakeSavedRequestManagementUseCase{}, fakeFlowManagementUseCase{}, fakeRunService{
		launchFlowFn:         func(usecase.LaunchFlowInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		launchSavedRequestFn: func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		runSavedRequestFn:    func(usecase.LaunchSavedRequestInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
		getRunStatusFn:       func(usecase.GetRunStatusQuery) (usecase.RunStatusView, error) { return usecase.RunStatusView{}, nil },
		rerunFn:              func(usecase.RerunInput) (domain.FlowRun, error) { return domain.FlowRun{}, nil },
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces/workspace-a/unarchive", nil)
	resp := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	if got.WorkspaceID != "workspace-a" {
		t.Fatalf("WorkspaceID = %q, want workspace-a", got.WorkspaceID)
	}
}
