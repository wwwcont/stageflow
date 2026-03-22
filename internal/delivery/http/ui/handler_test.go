package ui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/config"
	"stageflow/internal/delivery/http/ui/forms"
	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/internal/usecase"
	"stageflow/pkg/clock"
)

type fakeSavedRequestSvc struct{}

func (fakeSavedRequestSvc) CreateSavedRequest(context.Context, usecase.CreateSavedRequestCommand) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, nil
}
func (fakeSavedRequestSvc) UpdateSavedRequest(context.Context, usecase.UpdateSavedRequestCommand) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, nil
}
func (fakeSavedRequestSvc) GetSavedRequest(context.Context, usecase.GetSavedRequestQuery) (usecase.SavedRequestView, error) {
	return usecase.SavedRequestView{}, &domain.NotFoundError{Entity: "saved request", ID: "missing"}
}
func (fakeSavedRequestSvc) ListSavedRequests(context.Context, usecase.ListSavedRequestsQuery) ([]domain.SavedRequest, error) {
	return nil, nil
}

type fakeFlowSvc struct{}

func (fakeFlowSvc) CreateFlow(context.Context, usecase.CreateFlowCommand) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, nil
}
func (fakeFlowSvc) UpdateFlow(context.Context, usecase.UpdateFlowCommand) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, nil
}
func (fakeFlowSvc) GetFlow(context.Context, usecase.GetFlowQuery) (usecase.FlowDefinitionView, error) {
	return usecase.FlowDefinitionView{}, &domain.NotFoundError{Entity: "flow", ID: "missing"}
}
func (fakeFlowSvc) ListFlows(context.Context, usecase.ListFlowsQuery) ([]domain.Flow, error) {
	return nil, nil
}
func (fakeFlowSvc) ValidateFlow(context.Context, usecase.ValidateFlowCommand) (usecase.FlowValidationResult, error) {
	return usecase.FlowValidationResult{Valid: true}, nil
}

type fakeRunSvc struct{}

func (fakeRunSvc) LaunchFlow(context.Context, usecase.LaunchFlowInput) (domain.FlowRun, error) {
	return domain.FlowRun{}, nil
}
func (fakeRunSvc) LaunchSavedRequest(context.Context, usecase.LaunchSavedRequestInput) (domain.FlowRun, error) {
	return domain.FlowRun{}, nil
}
func (fakeRunSvc) RunSavedRequest(context.Context, usecase.LaunchSavedRequestInput) (domain.FlowRun, error) {
	return domain.FlowRun{}, nil
}
func (fakeRunSvc) GetRunStatus(context.Context, usecase.GetRunStatusQuery) (usecase.RunStatusView, error) {
	return usecase.RunStatusView{}, &domain.NotFoundError{Entity: "run", ID: "missing"}
}
func (fakeRunSvc) ListRuns(context.Context, usecase.ListRunsQuery) ([]domain.FlowRun, error) {
	return nil, nil
}
func (fakeRunSvc) Rerun(context.Context, usecase.RerunInput) (domain.FlowRun, error) {
	return domain.FlowRun{}, nil
}

type fakeCurlSvc struct {
	result usecase.ImportCurlResult
	err    error
}

func (f fakeCurlSvc) ImportCurl(context.Context, usecase.ImportCurlInput) (usecase.ImportCurlResult, error) {
	if f.err != nil {
		return usecase.ImportCurlResult{}, f.err
	}
	return f.result, nil
}

func testUIHandler(t *testing.T) http.Handler {
	t.Helper()
	return testUIHandlerWithCurl(t, fakeCurlSvc{})
}

func testUIHandlerWithCurl(t *testing.T, curlSvc fakeCurlSvc) http.Handler {
	t.Helper()
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	repo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "bootstrap",
		Name:        "Bootstrap workspace",
		Slug:        "bootstrap",
		Description: "Default workspace used in tests.",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      domain.WorkspacePolicy{AllowedHosts: []string{"example.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 10, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	workspaceSvc, err := usecase.NewWorkspaceManagementService(repo, clock.System{})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}
	cfg := config.Config{UI: config.UIConfig{Enabled: true, Prefix: "/ui"}, Runtime: config.RuntimeConfig{AllowedHosts: []string{"example.internal"}}}
	return NewHandler(cfg, zap.NewNop(), workspaceSvc, fakeSavedRequestSvc{}, fakeFlowSvc{}, fakeRunSvc{}, curlSvc, nil)
}

func TestHandler_Home(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "StageFlow") || !strings.Contains(body, "Bootstrap workspace") {
		t.Fatalf("body missing expected content: %s", body)
	}
}

func TestHandler_WorkspaceOverview(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/workspaces/bootstrap", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	for _, want := range []string{"Bootstrap workspace", "Requests", "Flows", "Policy"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestHandler_WorkspaceOverviewArchivedShowsRestore(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	repo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "archived",
		Name:        "Archived workspace",
		Slug:        "archived",
		Description: "Archived workspace used in tests.",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusArchived,
		Policy:      domain.WorkspacePolicy{AllowedHosts: []string{"example.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 10, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	workspaceSvc, err := usecase.NewWorkspaceManagementService(repo, clock.System{})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}
	cfg := config.Config{UI: config.UIConfig{Enabled: true, Prefix: "/ui"}, Runtime: config.RuntimeConfig{AllowedHosts: []string{"example.internal"}}}
	h := NewHandler(cfg, zap.NewNop(), workspaceSvc, fakeSavedRequestSvc{}, fakeFlowSvc{}, fakeRunSvc{}, fakeCurlSvc{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/ui/workspaces/archived", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	if !strings.Contains(body, "Restore from archive") {
		t.Fatalf("body missing restore action: %s", body)
	}
}

func TestHandler_FlowDetails_AllowsDraftFlowWithoutSteps(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "bootstrap",
		Name:        "Bootstrap workspace",
		Slug:        "bootstrap",
		Description: "Default workspace used in tests.",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      domain.WorkspacePolicy{AllowedHosts: []string{"example.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 10, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	workspaceSvc, err := usecase.NewWorkspaceManagementService(workspaceRepo, clock.System{})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}
	flowSvc, err := usecase.NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), clock.System{})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}
	if _, err := flowSvc.CreateFlow(context.Background(), usecase.CreateFlowCommand{
		WorkspaceID: "bootstrap",
		FlowID:      "empty-draft",
		Name:        "Empty draft",
		Description: "Should still be editable before steps are added.",
		Status:      domain.FlowStatusDraft,
	}); err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}

	cfg := config.Config{UI: config.UIConfig{Enabled: true, Prefix: "/ui"}, Runtime: config.RuntimeConfig{AllowedHosts: []string{"example.internal"}}}
	h := NewHandler(cfg, zap.NewNop(), workspaceSvc, fakeSavedRequestSvc{}, flowSvc, fakeRunSvc{}, fakeCurlSvc{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/ui/workspaces/bootstrap/flows/empty-draft", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{"Empty draft", "No steps defined yet.", "Add step"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestHandler_DocsPage(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/docs", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	for _, want := range []string{"StageFlow documentation", "Import a flow step from cURL", "Sandbox examples"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestHandler_LanguageSwitchRedirectsAndSetsCookie(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/language?lang=ru&next=%2Fui%2Fdocs", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	if got := resp.Header().Get("Location"); got != "/ui/docs?lang=ru" {
		t.Fatalf("redirect location = %q, want %q", got, "/ui/docs?lang=ru")
	}
	cookies := resp.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected language cookie to be set")
	}
	var found bool
	for _, cookie := range cookies {
		if cookie.Name == langCookieName {
			found = true
			if cookie.Value != "ru" {
				t.Fatalf("cookie value = %q, want %q", cookie.Value, "ru")
			}
		}
	}
	if !found {
		t.Fatalf("language cookie %q was not set", langCookieName)
	}
}

func TestHandler_LanguageSwitch_StripsExistingLangQueryFromNext(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/language?lang=ru&next=%2Fui%2Fdocs%3Flang%3Den%26tab%3Dcurl", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	if got := resp.Header().Get("Location"); got != "/ui/docs?lang=ru&tab=curl" {
		t.Fatalf("redirect location = %q, want %q", got, "/ui/docs?lang=ru&tab=curl")
	}
}

func TestHandler_DocsPage_RussianQueryOverridesLanguage(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/docs?lang=ru", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	for _, want := range []string{"Документация StageFlow", "Импорт шага из cURL", "<html lang=\"ru\">"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestHandler_Home_RussianLinksPreserveLanguage(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/ui?lang=ru", nil)
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
	body := resp.Body.String()
	for _, want := range []string{`href="/ui?lang=ru"`, `href="/ui/workspaces?lang=ru"`, `href="/ui/workspaces/bootstrap?lang=ru"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestHandler_WorkspaceCreateRedirectPreservesLanguage(t *testing.T) {
	h := testUIHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/ui/workspaces?lang=ru", strings.NewReader("id=ru-space&name=RU+Space&slug=ru-space&owner_team=platform"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	if got := resp.Header().Get("Location"); got != "/ui/workspaces/ru-space?lang=ru" {
		t.Fatalf("redirect location = %q, want %q", got, "/ui/workspaces/ru-space?lang=ru")
	}
}

func TestNormalizeAllowedHostEntries(t *testing.T) {
	got := normalizeAllowedHostEntries([]string{" localhost:8091 ", "http://Example.internal/api", "https://svc.internal:8443/path"})
	want := []string{"localhost", "example.internal", "svc.internal"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("normalizeAllowedHostEntries() = %#v, want %#v", got, want)
	}
}

func TestParseStepForm_IgnoresEmptyDefaultExtractionRow(t *testing.T) {
	step, err := parseStepForm(forms.FlowStepForm{
		ID:       "step-1",
		Name:     "step-1",
		StepType: string(domain.FlowStepTypeInlineRequest),
		InlineRequest: forms.RequestForm{
			Method: "GET",
			URL:    "https://example.internal/health",
		},
		ExtractionRules: []forms.ExtractionRuleForm{{Source: "body", Required: "true", JSONEncode: "false"}},
	}, 0)
	if err != nil {
		t.Fatalf("parseStepForm() error = %v", err)
	}
	if len(step.ExtractionSpec.Rules) != 0 {
		t.Fatalf("ExtractionSpec.Rules = %#v, want empty", step.ExtractionSpec.Rules)
	}
}

func TestHandler_FlowStepImportCurlPreview(t *testing.T) {
	now := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	workspaceRepo := repository.NewInMemoryWorkspaceRepository(domain.Workspace{
		ID:          "bootstrap",
		Name:        "Bootstrap workspace",
		Slug:        "bootstrap",
		Description: "Default workspace used in tests.",
		OwnerTeam:   "platform",
		Status:      domain.WorkspaceStatusActive,
		Policy:      domain.WorkspacePolicy{AllowedHosts: []string{"example.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 10, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	workspaceSvc, err := usecase.NewWorkspaceManagementService(workspaceRepo, clock.System{})
	if err != nil {
		t.Fatalf("NewWorkspaceManagementService() error = %v", err)
	}
	flowSvc, err := usecase.NewFlowManagementService(workspaceRepo, repository.NewInMemorySavedRequestRepository(), repository.NewInMemoryFlowRepository(), repository.NewInMemoryFlowStepRepository(), clock.System{})
	if err != nil {
		t.Fatalf("NewFlowManagementService() error = %v", err)
	}
	if _, err := flowSvc.CreateFlow(context.Background(), usecase.CreateFlowCommand{
		WorkspaceID: "bootstrap",
		FlowID:      "demo-flow",
		Name:        "Demo flow",
		Description: "Flow for cURL import preview.",
		Status:      domain.FlowStatusDraft,
	}); err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}

	curlSvc := fakeCurlSvc{result: usecase.ImportCurlResult{RequestSpec: domain.RequestSpec{
		Method:       http.MethodPost,
		URLTemplate:  "https://sandbox.example.test/api/users",
		Headers:      map[string]string{"Content-Type": "application/json"},
		BodyTemplate: `{"name":"Alice"}`,
		Timeout:      2 * time.Second,
	}}}
	cfg := config.Config{UI: config.UIConfig{Enabled: true, Prefix: "/ui"}, Runtime: config.RuntimeConfig{AllowedHosts: []string{"example.internal"}}}
	h := NewHandler(cfg, zap.NewNop(), workspaceSvc, fakeSavedRequestSvc{}, flowSvc, fakeRunSvc{}, curlSvc, nil)
	req := httptest.NewRequest(http.MethodPost, "/ui/workspaces/bootstrap/flows/demo-flow/steps/import-curl", strings.NewReader("command=curl+example"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	body := resp.Body.String()
	for _, want := range []string{"Review the generated step", "post api users", "https://sandbox.example.test/api/users", "{&#34;name&#34;:&#34;Alice&#34;}", "Add step to flow"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}
