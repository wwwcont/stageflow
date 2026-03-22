package http

import (
	"fmt"
	"strings"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

type workspaceRequest struct {
	ID          string             `json:"id,omitempty"`
	Name        string             `json:"name"`
	Slug        string             `json:"slug"`
	Description string             `json:"description,omitempty"`
	OwnerTeam   string             `json:"owner_team"`
	Status      string             `json:"status,omitempty"`
	Policy      workspacePolicyDTO `json:"policy,omitempty"`
	Variables   []workspaceVarDTO  `json:"variables,omitempty"`
}

type workspacePolicyDTO struct {
	AllowedHosts          []string       `json:"allowed_hosts"`
	MaxSavedRequests      int            `json:"max_saved_requests"`
	MaxFlows              int            `json:"max_flows"`
	MaxStepsPerFlow       int            `json:"max_steps_per_flow"`
	MaxRequestBodyBytes   int            `json:"max_request_body_bytes"`
	DefaultTimeoutMS      int            `json:"default_timeout_ms"`
	MaxRunDurationSeconds int            `json:"max_run_duration_seconds"`
	DefaultRetryPolicy    retryPolicyDTO `json:"default_retry_policy"`
}

type workspaceVarDTO struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type workspaceSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type workspaceSecretResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type workspaceResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Slug        string             `json:"slug"`
	Description string             `json:"description,omitempty"`
	OwnerTeam   string             `json:"owner_team"`
	Status      string             `json:"status"`
	Policy      workspacePolicyDTO `json:"policy"`
	Variables   []workspaceVarDTO  `json:"variables,omitempty"`
	CreatedAt   string             `json:"created_at"`
	UpdatedAt   string             `json:"updated_at"`
}

type savedRequestRequest struct {
	ID          string         `json:"id,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	RequestSpec requestSpecDTO `json:"request_spec"`
}

type savedRequestResponse struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	RequestSpec requestSpecDTO `json:"request_spec"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func toCreateWorkspaceCommand(request workspaceRequest) (usecase.CreateWorkspaceCommand, error) {
	policy, err := request.Policy.toDomain("policy")
	if err != nil {
		return usecase.CreateWorkspaceCommand{}, err
	}
	status := domain.WorkspaceStatus(strings.TrimSpace(request.Status))
	if status == "" {
		status = domain.WorkspaceStatusActive
	}
	return usecase.CreateWorkspaceCommand{
		WorkspaceID: domain.WorkspaceID(strings.TrimSpace(request.ID)),
		Name:        request.Name,
		Slug:        request.Slug,
		Description: request.Description,
		OwnerTeam:   request.OwnerTeam,
		Status:      status,
		Policy:      policy,
	}, nil
}

func toUpdateWorkspaceCommand(workspaceID string, request workspaceRequest) usecase.UpdateWorkspaceCommand {
	return usecase.UpdateWorkspaceCommand{
		WorkspaceID: domain.WorkspaceID(workspaceID),
		Name:        request.Name,
		Slug:        request.Slug,
		Description: request.Description,
		OwnerTeam:   request.OwnerTeam,
	}
}

func toUpdateWorkspacePolicyCommand(workspaceID string, dto workspacePolicyDTO) (usecase.UpdateWorkspacePolicyCommand, error) {
	policy, err := dto.toDomain("policy")
	if err != nil {
		return usecase.UpdateWorkspacePolicyCommand{}, err
	}
	return usecase.UpdateWorkspacePolicyCommand{
		WorkspaceID: domain.WorkspaceID(workspaceID),
		Policy:      policy,
	}, nil
}

func toUpdateWorkspaceVariablesCommand(workspaceID string, dto []workspaceVarDTO) usecase.UpdateWorkspaceVariablesCommand {
	vars := make([]domain.WorkspaceVariable, 0, len(dto))
	for _, item := range dto {
		vars = append(vars, domain.WorkspaceVariable{Name: item.Name, Value: item.Value})
	}
	return usecase.UpdateWorkspaceVariablesCommand{
		WorkspaceID: domain.WorkspaceID(workspaceID),
		Variables:   vars,
	}
}

func toCreateSavedRequestCommand(workspaceID string, request savedRequestRequest) (usecase.CreateSavedRequestCommand, error) {
	spec, err := request.RequestSpec.toDomain()
	if err != nil {
		return usecase.CreateSavedRequestCommand{}, err
	}
	return usecase.CreateSavedRequestCommand{
		WorkspaceID:    domain.WorkspaceID(workspaceID),
		SavedRequestID: domain.SavedRequestID(strings.TrimSpace(request.ID)),
		Name:           request.Name,
		Description:    request.Description,
		RequestSpec:    spec,
	}, nil
}

func toUpdateSavedRequestCommand(workspaceID, requestID string, request savedRequestRequest) (usecase.UpdateSavedRequestCommand, error) {
	spec, err := request.RequestSpec.toDomain()
	if err != nil {
		return usecase.UpdateSavedRequestCommand{}, err
	}
	return usecase.UpdateSavedRequestCommand{
		WorkspaceID:    domain.WorkspaceID(workspaceID),
		SavedRequestID: domain.SavedRequestID(requestID),
		Name:           request.Name,
		Description:    request.Description,
		RequestSpec:    spec,
	}, nil
}

func (dto workspacePolicyDTO) toDomain(prefix string) (domain.WorkspacePolicy, error) {
	initialInterval, err := parseDuration(dto.DefaultRetryPolicy.InitialInterval)
	if err != nil {
		return domain.WorkspacePolicy{}, fmt.Errorf("%s.default_retry_policy.initial_interval: %w", prefix, err)
	}
	maxInterval, err := parseDuration(dto.DefaultRetryPolicy.MaxInterval)
	if err != nil {
		return domain.WorkspacePolicy{}, fmt.Errorf("%s.default_retry_policy.max_interval: %w", prefix, err)
	}
	return domain.WorkspacePolicy{
		AllowedHosts:          dto.AllowedHosts,
		MaxSavedRequests:      dto.MaxSavedRequests,
		MaxFlows:              dto.MaxFlows,
		MaxStepsPerFlow:       dto.MaxStepsPerFlow,
		MaxRequestBodyBytes:   dto.MaxRequestBodyBytes,
		DefaultTimeoutMS:      dto.DefaultTimeoutMS,
		MaxRunDurationSeconds: dto.MaxRunDurationSeconds,
		DefaultRetryPolicy: domain.RetryPolicy{
			Enabled:              dto.DefaultRetryPolicy.Enabled,
			MaxAttempts:          dto.DefaultRetryPolicy.MaxAttempts,
			BackoffStrategy:      domain.BackoffStrategy(dto.DefaultRetryPolicy.BackoffStrategy),
			InitialInterval:      initialInterval,
			MaxInterval:          maxInterval,
			RetryableStatusCodes: dto.DefaultRetryPolicy.RetryableStatusCodes,
		},
	}, nil
}

func mapWorkspace(view usecase.WorkspaceView) workspaceResponse {
	workspace := view.Workspace
	vars := make([]workspaceVarDTO, 0, len(workspace.Variables))
	for _, item := range workspace.Variables {
		vars = append(vars, workspaceVarDTO{Name: item.Name, Value: item.Value})
	}
	return workspaceResponse{
		ID:          string(workspace.ID),
		Name:        workspace.Name,
		Slug:        workspace.Slug,
		Description: workspace.Description,
		OwnerTeam:   workspace.OwnerTeam,
		Status:      string(workspace.Status),
		Policy: workspacePolicyDTO{
			AllowedHosts:          workspace.Policy.AllowedHosts,
			MaxSavedRequests:      workspace.Policy.MaxSavedRequests,
			MaxFlows:              workspace.Policy.MaxFlows,
			MaxStepsPerFlow:       workspace.Policy.MaxStepsPerFlow,
			MaxRequestBodyBytes:   workspace.Policy.MaxRequestBodyBytes,
			DefaultTimeoutMS:      workspace.Policy.DefaultTimeoutMS,
			MaxRunDurationSeconds: workspace.Policy.MaxRunDurationSeconds,
			DefaultRetryPolicy: retryPolicyDTO{
				Enabled:              workspace.Policy.DefaultRetryPolicy.Enabled,
				MaxAttempts:          workspace.Policy.DefaultRetryPolicy.MaxAttempts,
				BackoffStrategy:      string(workspace.Policy.DefaultRetryPolicy.BackoffStrategy),
				InitialInterval:      formatDurationValue(workspace.Policy.DefaultRetryPolicy.InitialInterval),
				MaxInterval:          formatDurationValue(workspace.Policy.DefaultRetryPolicy.MaxInterval),
				RetryableStatusCodes: workspace.Policy.DefaultRetryPolicy.RetryableStatusCodes,
			},
		},
		Variables: vars,
		CreatedAt: formatTime(workspace.CreatedAt),
		UpdatedAt: formatTime(workspace.UpdatedAt),
	}
}

func formatDurationValue(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return value.String()
}

func mapSavedRequest(view usecase.SavedRequestView) savedRequestResponse {
	request := view.SavedRequest
	return savedRequestResponse{
		ID:          string(request.ID),
		WorkspaceID: string(request.WorkspaceID),
		Name:        request.Name,
		Description: request.Description,
		RequestSpec: mapRequestSpec(request.RequestSpec),
		CreatedAt:   formatTime(request.CreatedAt),
		UpdatedAt:   formatTime(request.UpdatedAt),
	}
}

func mapWorkspaceSecrets(items []domain.WorkspaceSecret) []workspaceSecretResponse {
	result := make([]workspaceSecretResponse, 0, len(items))
	for _, item := range items {
		result = append(result, workspaceSecretResponse{ID: item.Name, Name: item.Name})
	}
	return result
}
