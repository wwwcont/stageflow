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
