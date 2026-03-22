package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var workspaceSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
var workspaceValueNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type WorkspaceID string

type WorkspaceStatus string

const (
	WorkspaceStatusActive   WorkspaceStatus = "active"
	WorkspaceStatusArchived WorkspaceStatus = "archived"
)

type Workspace struct {
	ID          WorkspaceID
	Name        string
	Slug        string
	Description string
	OwnerTeam   string
	Status      WorkspaceStatus
	Variables   []WorkspaceVariable
	Secrets     []WorkspaceSecret
	Policy      WorkspacePolicy
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type WorkspaceVariable struct {
	Name  string
	Value string
}

type WorkspaceSecret struct {
	Name  string
	Value string
}

type WorkspacePolicy struct {
	AllowedHosts          []string
	MaxSavedRequests      int
	MaxFlows              int
	MaxStepsPerFlow       int
	MaxRequestBodyBytes   int
	DefaultTimeoutMS      int
	MaxRunDurationSeconds int
	DefaultRetryPolicy    RetryPolicy
}

func (w Workspace) Normalized() Workspace {
	w.Name = strings.TrimSpace(w.Name)
	w.Slug = strings.ToLower(strings.TrimSpace(w.Slug))
	w.Description = strings.TrimSpace(w.Description)
	w.OwnerTeam = strings.TrimSpace(w.OwnerTeam)
	w.Variables = normalizeWorkspaceVariables(w.Variables)
	w.Secrets = normalizeWorkspaceSecrets(w.Secrets)
	w.Policy = w.Policy.Normalized()
	return w
}

func (w Workspace) Validate() error {
	normalized := w.Normalized()
	if normalized.ID == "" {
		return &ValidationError{Message: "workspace id is required"}
	}
	if normalized.Name == "" {
		return &ValidationError{Message: "workspace name is required"}
	}
	if normalized.Slug == "" {
		return &ValidationError{Message: "workspace slug is required"}
	}
	if !workspaceSlugPattern.MatchString(normalized.Slug) {
		return &ValidationError{Message: fmt.Sprintf("workspace slug %q is invalid", w.Slug)}
	}
	if normalized.OwnerTeam == "" {
		return &ValidationError{Message: "workspace owner_team is required"}
	}
	if err := validateWorkspaceVariables(normalized.Variables); err != nil {
		return err
	}
	if err := validateWorkspaceSecrets(normalized.Secrets); err != nil {
		return err
	}
	switch normalized.Status {
	case WorkspaceStatusActive, WorkspaceStatusArchived:
	default:
		return &ValidationError{Message: fmt.Sprintf("unsupported workspace status %q", w.Status)}
	}
	if err := normalized.Policy.Validate(); err != nil {
		return err
	}
	if !normalized.CreatedAt.IsZero() && !normalized.UpdatedAt.IsZero() && normalized.UpdatedAt.Before(normalized.CreatedAt) {
		return &ValidationError{Message: "workspace updated_at must not be before created_at"}
	}
	return nil
}

func (w Workspace) AllowsWrites() bool {
	return w.Status == WorkspaceStatusActive
}

func (p WorkspacePolicy) Validate() error {
	normalized := p.Normalized()
	if len(normalized.AllowedHosts) == 0 {
		return &ValidationError{Message: "workspace policy allowed_hosts must contain at least one host"}
	}
	if normalized.MaxSavedRequests <= 0 {
		return &ValidationError{Message: "workspace policy max_saved_requests must be > 0"}
	}
	if normalized.MaxFlows <= 0 {
		return &ValidationError{Message: "workspace policy max_flows must be > 0"}
	}
	if normalized.MaxStepsPerFlow <= 0 {
		return &ValidationError{Message: "workspace policy max_steps_per_flow must be > 0"}
	}
	if normalized.MaxRequestBodyBytes <= 0 {
		return &ValidationError{Message: "workspace policy max_request_body_bytes must be > 0"}
	}
	if normalized.DefaultTimeoutMS <= 0 {
		return &ValidationError{Message: "workspace policy default_timeout_ms must be > 0"}
	}
	if normalized.MaxRunDurationSeconds <= 0 {
		return &ValidationError{Message: "workspace policy max_run_duration_seconds must be > 0"}
	}
	if err := normalized.DefaultRetryPolicy.Validate(); err != nil {
		return err
	}
	return nil
}

func (p WorkspacePolicy) Normalized() WorkspacePolicy {
	if len(p.AllowedHosts) == 0 {
		return p
	}
	seen := make(map[string]struct{}, len(p.AllowedHosts))
	result := make([]string, 0, len(p.AllowedHosts))
	for _, host := range p.AllowedHosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	p.AllowedHosts = result
	return p
}

func (w Workspace) VariableMap() map[string]string {
	result := make(map[string]string, len(w.Variables))
	for _, variable := range w.Normalized().Variables {
		result[variable.Name] = variable.Value
	}
	return result
}

func (w Workspace) SecretMap() map[string]string {
	result := make(map[string]string, len(w.Secrets))
	for _, secret := range w.Normalized().Secrets {
		result[secret.Name] = secret.Value
	}
	return result
}

func normalizeWorkspaceVariables(items []WorkspaceVariable) []WorkspaceVariable {
	if len(items) == 0 {
		return nil
	}
	result := make([]WorkspaceVariable, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := WorkspaceVariable{
			Name:  strings.ToLower(strings.TrimSpace(item.Name)),
			Value: strings.TrimSpace(item.Value),
		}
		if normalized.Name == "" {
			continue
		}
		if _, ok := seen[normalized.Name]; ok {
			continue
		}
		seen[normalized.Name] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalizeWorkspaceSecrets(items []WorkspaceSecret) []WorkspaceSecret {
	if len(items) == 0 {
		return nil
	}
	result := make([]WorkspaceSecret, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		normalized := WorkspaceSecret{
			Name:  strings.ToLower(strings.TrimSpace(item.Name)),
			Value: item.Value,
		}
		if normalized.Name == "" {
			continue
		}
		if _, ok := seen[normalized.Name]; ok {
			continue
		}
		seen[normalized.Name] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func validateWorkspaceVariables(items []WorkspaceVariable) error {
	for _, item := range items {
		if !workspaceValueNamePattern.MatchString(item.Name) {
			return &ValidationError{Message: fmt.Sprintf("workspace variable name %q is invalid", item.Name)}
		}
	}
	return nil
}

func validateWorkspaceSecrets(items []WorkspaceSecret) error {
	for _, item := range items {
		if !workspaceValueNamePattern.MatchString(item.Name) {
			return &ValidationError{Message: fmt.Sprintf("workspace secret name %q is invalid", item.Name)}
		}
		if strings.TrimSpace(item.Value) == "" {
			return &ValidationError{Message: fmt.Sprintf("workspace secret %q value is required", item.Name)}
		}
	}
	return nil
}
