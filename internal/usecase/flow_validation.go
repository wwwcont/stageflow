package usecase

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"stageflow/internal/domain"
)

var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z_][a-zA-Z0-9_\.-]*)\s*\}\}`)

type templateValidationScope struct {
	RuntimeVars   map[string]struct{}
	PriorSteps    map[string]struct{}
	WorkspaceVars map[string]struct{}
	Secrets       map[string]struct{}
}

func newTemplateValidationScope(workspace domain.Workspace) templateValidationScope {
	scope := templateValidationScope{
		RuntimeVars:   map[string]struct{}{},
		PriorSteps:    map[string]struct{}{},
		WorkspaceVars: map[string]struct{}{},
		Secrets:       map[string]struct{}{},
	}
	for _, variable := range workspace.Normalized().Variables {
		scope.WorkspaceVars[variable.Name] = struct{}{}
	}
	for _, secret := range workspace.Normalized().Secrets {
		scope.Secrets[secret.Name] = struct{}{}
	}
	return scope
}

func validateStepTemplates(step domain.FlowStep, scope templateValidationScope) []string {
	issues := make([]string, 0)

	issues = append(issues, validateTemplateString("request url_template", step.Name, step.RequestSpec.URLTemplate, scope)...)
	issues = append(issues, validateURLTemplate(step.Name, step.RequestSpec.URLTemplate)...)
	issues = append(issues, validateTemplateString("request body_template", step.Name, step.RequestSpec.BodyTemplate, scope)...)

	for header, value := range step.RequestSpec.Headers {
		issues = append(issues, validateTemplateString(fmt.Sprintf("request header %q", header), step.Name, value, scope)...)
	}
	for name, value := range step.RequestSpec.Query {
		issues = append(issues, validateTemplateString(fmt.Sprintf("request query %q", name), step.Name, value, scope)...)
	}
	for _, rule := range step.ExtractionSpec.Rules {
		issues = append(issues, validateTemplateString(fmt.Sprintf("extraction selector %q", rule.Name), step.Name, rule.Selector, scope)...)
	}
	for _, rule := range step.AssertionSpec.Rules {
		issues = append(issues, validateTemplateString(fmt.Sprintf("assertion selector %q", rule.Name), step.Name, rule.Selector, scope)...)
		issues = append(issues, validateTemplateString(fmt.Sprintf("assertion expected_value %q", rule.Name), step.Name, rule.ExpectedValue, scope)...)
		for _, expected := range rule.ExpectedValues {
			issues = append(issues, validateTemplateString(fmt.Sprintf("assertion expected_values %q", rule.Name), step.Name, expected, scope)...)
		}
	}

	return issues
}

func validateTemplateString(field, stepName, raw string, scope templateValidationScope) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	issues := make([]string, 0)
	if strings.Count(raw, "{{") != strings.Count(raw, "}}") {
		issues = append(issues, fmt.Sprintf("step %q %s contains unbalanced template delimiters", stepName, field))
		return issues
	}

	matched := placeholderPattern.FindAllStringSubmatch(raw, -1)
	cleaned := placeholderPattern.ReplaceAllString(raw, "")
	if strings.Contains(cleaned, "{{") || strings.Contains(cleaned, "}}") {
		issues = append(issues, fmt.Sprintf("step %q %s contains malformed template expression", stepName, field))
	}
	for _, match := range matched {
		ref := match[1]
		if !isVariableReferenceAllowed(ref, scope) {
			issues = append(issues, fmt.Sprintf("step %q %s references unknown variable %q", stepName, field, ref))
		}
	}
	return issues
}

func isVariableReferenceAllowed(reference string, scope templateValidationScope) bool {
	switch {
	case reference == "", reference == "uuid", reference == "timestamp", reference == "run.id":
		return reference != ""
	case strings.HasPrefix(reference, "run.input."):
		return len(reference) > len("run.input.")
	case strings.HasPrefix(reference, "vars."):
		name := strings.Split(strings.TrimPrefix(reference, "vars."), ".")[0]
		_, ok := scope.RuntimeVars[name]
		return ok
	case strings.HasPrefix(reference, "workspace.vars."):
		name := strings.Split(strings.TrimPrefix(reference, "workspace.vars."), ".")[0]
		_, ok := scope.WorkspaceVars[name]
		return ok
	case strings.HasPrefix(reference, "secret."):
		name := strings.Split(strings.TrimPrefix(reference, "secret."), ".")[0]
		_, ok := scope.Secrets[name]
		return ok
	case strings.HasPrefix(reference, "steps."):
		stepName := strings.Split(strings.TrimPrefix(reference, "steps."), ".")[0]
		_, ok := scope.PriorSteps[stepName]
		return ok
	default:
		return false
	}
}

func validateURLTemplate(stepName, raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	replaced := placeholderPattern.ReplaceAllString(raw, "placeholder")
	if strings.HasPrefix(strings.TrimSpace(replaced), "placeholder") {
		replaced = "https://example.invalid" + strings.TrimPrefix(strings.TrimSpace(replaced), "placeholder")
	}
	parsed, err := url.Parse(replaced)
	if err != nil {
		return []string{fmt.Sprintf("step %q request url_template is invalid: %v", stepName, err)}
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return []string{fmt.Sprintf("step %q request url_template must use http or https", stepName)}
	}
	if parsed.Hostname() == "" {
		return []string{fmt.Sprintf("step %q request url_template must contain a host", stepName)}
	}
	if parsed.User != nil {
		return []string{fmt.Sprintf("step %q request url_template must not include userinfo", stepName)}
	}
	return nil
}
