package execution

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"stageflow/internal/domain"
)

func TestDefaultTemplateRenderer_RenderString(t *testing.T) {
	renderer := NewDefaultTemplateRenderer()
	renderer.now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	renderer.uuid = func() (string, error) { return "11111111-2222-4333-8444-555555555555", nil }

	run := domain.FlowRun{ID: "run-1", InputJSON: json.RawMessage(`{"account_id":"acc-42"}`)}
	workspace := domain.Workspace{
		ID:        "workspace-1",
		Name:      "Workspace",
		Slug:      "workspace",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: "https://svc.internal"}},
		Secrets:   []domain.WorkspaceSecret{{Name: "crm_token", Value: "top-secret"}},
		Policy:    domain.WorkspacePolicy{AllowedHosts: []string{"svc.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 5, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
	}
	data, err := NewRenderData(run, workspace, map[string]any{"user_id": "user-7"}, map[string]map[string]any{"fetch_account": {"token": "abc123"}})
	if err != nil {
		t.Fatalf("NewRenderData() error = %v", err)
	}

	tests := []struct {
		name     string
		template string
		want     string
		wantErr  string
	}{
		{name: "run id", template: "{{run.id}}", want: "run-1"},
		{name: "run input", template: "acct={{run.input.account_id}}", want: "acct=acc-42"},
		{name: "workspace var", template: "{{workspace.vars.base_url}}", want: "https://svc.internal"},
		{name: "secret", template: "Bearer {{secret.crm_token}}", want: "Bearer top-secret"},
		{name: "runtime var", template: "{{vars.user_id}}", want: "user-7"},
		{name: "step value", template: "Bearer {{steps.fetch_account.token}}", want: "Bearer abc123"},
		{name: "uuid", template: "{{uuid}}", want: "11111111-2222-4333-8444-555555555555"},
		{name: "timestamp", template: "{{timestamp}}", want: "2026-01-02T03:04:05Z"},
		{name: "missing value", template: "{{steps.fetch_account.missing}}", wantErr: "step value does not exist"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderer.RenderString(tt.template, data)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RenderString() error = %v, want contains %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenderString() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("RenderString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultTemplateRenderer_ResolveRequestSpec(t *testing.T) {
	renderer := NewDefaultTemplateRenderer()
	run := domain.FlowRun{ID: "run-1", InputJSON: json.RawMessage(`{"env":"staging"}`)}
	workspace := domain.Workspace{
		ID:        "workspace-1",
		Name:      "Workspace",
		Slug:      "workspace",
		OwnerTeam: "platform",
		Status:    domain.WorkspaceStatusActive,
		Variables: []domain.WorkspaceVariable{{Name: "base_url", Value: "https://example.internal"}},
		Secrets:   []domain.WorkspaceSecret{{Name: "auth_token", Value: "secret"}},
		Policy:    domain.WorkspacePolicy{AllowedHosts: []string{"example.internal"}, MaxSavedRequests: 10, MaxFlows: 10, MaxStepsPerFlow: 5, MaxRequestBodyBytes: 1024, DefaultTimeoutMS: 1000, MaxRunDurationSeconds: 60, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}},
	}
	data, err := NewRenderData(run, workspace, map[string]any{"tenant_id": "tenant-42"}, map[string]map[string]any{"auth": {"token": "secret"}})
	if err != nil {
		t.Fatalf("NewRenderData() error = %v", err)
	}

	resolved, err := renderer.Resolve(context.Background(), domain.RequestSpec{
		Method:       "GET",
		URLTemplate:  "{{workspace.vars.base_url}}/{{run.input.env}}/accounts",
		Headers:      map[string]string{"Authorization": "Bearer {{secret.auth_token}}"},
		Query:        map[string]string{"trace": "{{run.id}}", "tenant_id": "{{vars.tenant_id}}"},
		BodyTemplate: "",
	}, data)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.URL != "https://example.internal/staging/accounts?tenant_id=tenant-42&trace=run-1" && resolved.URL != "https://example.internal/staging/accounts?trace=run-1&tenant_id=tenant-42" {
		t.Fatalf("Resolve().URL = %q", resolved.URL)
	}
	if resolved.Headers["Authorization"] != "Bearer secret" {
		t.Fatalf("Resolve().Headers[Authorization] = %q", resolved.Headers["Authorization"])
	}
}
