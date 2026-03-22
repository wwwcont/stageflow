package usecase

import (
	"strings"
	"testing"

	"stageflow/internal/domain"
)

func TestValidateStepTemplates_AllowsLocalhostURLTemplate(t *testing.T) {
	issues := validateStepTemplates(domain.FlowStep{
		Name: "post api reset",
		RequestSpec: domain.RequestSpec{
			Method:      "POST",
			URLTemplate: "http://localhost:8091/api/reset",
		},
	}, templateValidationScope{})
	for _, issue := range issues {
		if strings.Contains(issue, "localhost") {
			t.Fatalf("unexpected localhost validation issue: %v", issues)
		}
	}
}

func TestValidateStepTemplates_AllowsLoopbackIPURLTemplate(t *testing.T) {
	issues := validateStepTemplates(domain.FlowStep{
		Name: "post api reset",
		RequestSpec: domain.RequestSpec{
			Method:      "POST",
			URLTemplate: "http://127.0.0.1:8091/api/reset",
		},
	}, templateValidationScope{})
	for _, issue := range issues {
		if strings.Contains(issue, "loopback") || strings.Contains(issue, "127.0.0.1") {
			t.Fatalf("unexpected loopback validation issue: %v", issues)
		}
	}
}

func TestValidateWorkspaceRequestHost_TreatsLocalhostAndLoopbackAsEquivalent(t *testing.T) {
	step := domain.FlowStep{
		Name: "reset sandbox",
		RequestSpec: domain.RequestSpec{
			Method:      "POST",
			URLTemplate: "http://127.0.0.1:8091/api/reset",
		},
	}
	if issue := validateWorkspaceRequestHost([]string{"localhost"}, step); issue != "" {
		t.Fatalf("validateWorkspaceRequestHost() = %q, want empty", issue)
	}
}
