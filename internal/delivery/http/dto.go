package http

import (
	"encoding/json"
	"fmt"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

type listResponse[T any] struct {
	Items []T `json:"items"`
}

type flowRequest struct {
	WorkspaceID string            `json:"workspace_id"`
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Status      string            `json:"status"`
	Steps       []flowStepRequest `json:"steps"`
}

type flowStepRequest struct {
	ID                  string                 `json:"id"`
	OrderIndex          int                    `json:"order_index"`
	Name                string                 `json:"name"`
	StepType            string                 `json:"step_type,omitempty"`
	SavedRequestID      string                 `json:"saved_request_id,omitempty"`
	RequestSpec         requestSpecDTO         `json:"request_spec"`
	RequestSpecOverride requestSpecOverrideDTO `json:"request_spec_override,omitempty"`
	ExtractionSpec      extractionSpecDTO      `json:"extraction_spec"`
	AssertionSpec       assertionSpecDTO       `json:"assertion_spec"`
}

type runLaunchRequest struct {
	WorkspaceID    string          `json:"workspace_id"`
	InitiatedBy    string          `json:"initiated_by"`
	Input          json.RawMessage `json:"input,omitempty"`
	Queue          string          `json:"queue,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type rerunRequest struct {
	WorkspaceID    string          `json:"workspace_id"`
	InitiatedBy    string          `json:"initiated_by"`
	OverrideInput  json.RawMessage `json:"override_input,omitempty"`
	Queue          string          `json:"queue,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
}

type importCurlRequest struct {
	Command string `json:"command"`
}

type flowResponse struct {
	WorkspaceID string `json:"workspace_id"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     int    `json:"version"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type flowDefinitionResponse struct {
	Flow  flowResponse       `json:"flow"`
	Steps []flowStepResponse `json:"steps"`
}

type flowStepResponse struct {
	ID                  string                 `json:"id"`
	FlowID              string                 `json:"flow_id"`
	OrderIndex          int                    `json:"order_index"`
	Name                string                 `json:"name"`
	StepType            string                 `json:"step_type,omitempty"`
	SavedRequestID      string                 `json:"saved_request_id,omitempty"`
	RequestSpec         requestSpecDTO         `json:"request_spec"`
	RequestSpecOverride requestSpecOverrideDTO `json:"request_spec_override,omitempty"`
	ExtractionSpec      extractionSpecDTO      `json:"extraction_spec"`
	AssertionSpec       assertionSpecDTO       `json:"assertion_spec"`
	CreatedAt           string                 `json:"created_at"`
	UpdatedAt           string                 `json:"updated_at"`
}

type requestSpecDTO struct {
	Method          string            `json:"method"`
	URLTemplate     string            `json:"url_template"`
	Headers         map[string]string `json:"headers,omitempty"`
	Query           map[string]string `json:"query,omitempty"`
	BodyTemplate    string            `json:"body_template,omitempty"`
	Timeout         string            `json:"timeout,omitempty"`
	FollowRedirects bool              `json:"follow_redirects,omitempty"`
	RetryPolicy     retryPolicyDTO    `json:"retry_policy,omitempty"`
}

type requestSpecOverrideDTO struct {
	Headers      map[string]string `json:"headers,omitempty"`
	Query        map[string]string `json:"query,omitempty"`
	BodyTemplate *string           `json:"body_template,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	RetryPolicy  *retryPolicyDTO   `json:"retry_policy,omitempty"`
}

type retryPolicyDTO struct {
	Enabled              bool   `json:"enabled"`
	MaxAttempts          int    `json:"max_attempts,omitempty"`
	BackoffStrategy      string `json:"backoff_strategy,omitempty"`
	InitialInterval      string `json:"initial_interval,omitempty"`
	MaxInterval          string `json:"max_interval,omitempty"`
	RetryableStatusCodes []int  `json:"retryable_status_codes,omitempty"`
}

type extractionSpecDTO struct {
	Rules []extractionRuleDTO `json:"rules,omitempty"`
}

type extractionRuleDTO struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	Selector   string `json:"selector,omitempty"`
	Required   bool   `json:"required,omitempty"`
	JSONEncode bool   `json:"json_encode,omitempty"`
}

type assertionSpecDTO struct {
	Rules []assertionRuleDTO `json:"rules,omitempty"`
}

type assertionRuleDTO struct {
	Name           string   `json:"name"`
	Target         string   `json:"target"`
	Operator       string   `json:"operator"`
	Selector       string   `json:"selector,omitempty"`
	ExpectedValue  string   `json:"expected_value,omitempty"`
	ExpectedValues []string `json:"expected_values,omitempty"`
}

type validationResponse struct {
	Valid  bool     `json:"valid"`
	Issues []string `json:"issues,omitempty"`
}

type runResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	TargetType     string `json:"target_type"`
	FlowID         string `json:"flow_id"`
	FlowVersion    int    `json:"flow_version"`
	SavedRequestID string `json:"saved_request_id,omitempty"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	StartedAt      string `json:"started_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
	InitiatedBy    string `json:"initiated_by"`
	QueueName      string `json:"queue_name,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
	ClaimedBy      string `json:"claimed_by,omitempty"`
	HeartbeatAt    string `json:"heartbeat_at,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	Input          any    `json:"input,omitempty"`
}

type runStepResponse struct {
	ID               string `json:"id"`
	RunID            string `json:"run_id"`
	StepName         string `json:"step_name"`
	StepOrder        int    `json:"step_order"`
	Status           string `json:"status"`
	RetryCount       int    `json:"retry_count"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	StartedAt        string `json:"started_at,omitempty"`
	FinishedAt       string `json:"finished_at,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
	RequestSnapshot  any    `json:"request_snapshot,omitempty"`
	ResponseSnapshot any    `json:"response_snapshot,omitempty"`
	ExtractedValues  any    `json:"extracted_values,omitempty"`
	AttemptHistory   any    `json:"attempt_history,omitempty"`
}

type runEventResponse struct {
	ID             string `json:"id"`
	RunID          string `json:"run_id"`
	WorkspaceID    string `json:"workspace_id"`
	FlowID         string `json:"flow_id,omitempty"`
	SavedRequestID string `json:"saved_request_id,omitempty"`
	Sequence       int64  `json:"sequence"`
	EventType      string `json:"event_type"`
	Level          string `json:"level"`
	StepName       string `json:"step_name,omitempty"`
	StepOrder      *int   `json:"step_order,omitempty"`
	Attempt        *int   `json:"attempt,omitempty"`
	Message        string `json:"message"`
	CreatedAt      string `json:"created_at"`
	Details        any    `json:"details,omitempty"`
}

type importCurlResponse struct {
	RequestSpec requestSpecDTO `json:"request_spec"`
}

func toCreateFlowCommand(request flowRequest) (usecase.CreateFlowCommand, error) {
	steps, err := toFlowStepDrafts(request.Steps)
	if err != nil {
		return usecase.CreateFlowCommand{}, err
	}
	return usecase.CreateFlowCommand{
		WorkspaceID: domain.WorkspaceID(request.WorkspaceID),
		FlowID:      domain.FlowID(request.ID),
		Name:        request.Name,
		Description: request.Description,
		Status:      domain.FlowStatus(request.Status),
		Steps:       steps,
	}, nil
}

func toUpdateFlowCommand(flowID string, request flowRequest) (usecase.UpdateFlowCommand, error) {
	steps, err := toFlowStepDrafts(request.Steps)
	if err != nil {
		return usecase.UpdateFlowCommand{}, err
	}
	return usecase.UpdateFlowCommand{
		WorkspaceID: domain.WorkspaceID(request.WorkspaceID),
		FlowID:      domain.FlowID(flowID),
		Name:        request.Name,
		Description: request.Description,
		Status:      domain.FlowStatus(request.Status),
		Steps:       steps,
	}, nil
}

func toValidateFlowCommand(flowID string, request flowRequest) (usecase.ValidateFlowCommand, error) {
	steps, err := toFlowStepDrafts(request.Steps)
	if err != nil {
		return usecase.ValidateFlowCommand{}, err
	}
	return usecase.ValidateFlowCommand{
		WorkspaceID: domain.WorkspaceID(request.WorkspaceID),
		FlowID:      domain.FlowID(flowID),
		Name:        request.Name,
		Description: request.Description,
		Status:      domain.FlowStatus(request.Status),
		Steps:       steps,
	}, nil
}

func toFlowStepDrafts(requests []flowStepRequest) ([]usecase.FlowStepDraft, error) {
	steps := make([]usecase.FlowStepDraft, 0, len(requests))
	for _, request := range requests {
		requestSpec, err := request.RequestSpec.toDomain()
		if err != nil {
			return nil, err
		}
		requestOverride, err := request.RequestSpecOverride.toDomain()
		if err != nil {
			return nil, err
		}
		steps = append(steps, usecase.FlowStepDraft{
			ID:                  domain.FlowStepID(request.ID),
			OrderIndex:          request.OrderIndex,
			Name:                request.Name,
			StepType:            domain.FlowStepType(request.StepType),
			SavedRequestID:      domain.SavedRequestID(request.SavedRequestID),
			RequestSpec:         requestSpec,
			RequestSpecOverride: requestOverride,
			ExtractionSpec:      request.ExtractionSpec.toDomain(),
			AssertionSpec:       request.AssertionSpec.toDomain(),
		})
	}
	return steps, nil
}

func (dto requestSpecDTO) toDomain() (domain.RequestSpec, error) {
	timeout, err := parseDuration(dto.Timeout)
	if err != nil {
		return domain.RequestSpec{}, fmt.Errorf("request_spec.timeout: %w", err)
	}
	initialInterval, err := parseDuration(dto.RetryPolicy.InitialInterval)
	if err != nil {
		return domain.RequestSpec{}, fmt.Errorf("request_spec.retry_policy.initial_interval: %w", err)
	}
	maxInterval, err := parseDuration(dto.RetryPolicy.MaxInterval)
	if err != nil {
		return domain.RequestSpec{}, fmt.Errorf("request_spec.retry_policy.max_interval: %w", err)
	}
	return domain.RequestSpec{
		Method:          dto.Method,
		URLTemplate:     dto.URLTemplate,
		Headers:         dto.Headers,
		Query:           dto.Query,
		BodyTemplate:    dto.BodyTemplate,
		Timeout:         timeout,
		FollowRedirects: dto.FollowRedirects,
		RetryPolicy: domain.RetryPolicy{
			Enabled:              dto.RetryPolicy.Enabled,
			MaxAttempts:          dto.RetryPolicy.MaxAttempts,
			BackoffStrategy:      domain.BackoffStrategy(dto.RetryPolicy.BackoffStrategy),
			InitialInterval:      initialInterval,
			MaxInterval:          maxInterval,
			RetryableStatusCodes: dto.RetryPolicy.RetryableStatusCodes,
		},
	}, nil
}

func (dto requestSpecOverrideDTO) toDomain() (domain.RequestSpecOverride, error) {
	timeout, err := parseDuration(dto.Timeout)
	if err != nil {
		return domain.RequestSpecOverride{}, fmt.Errorf("request_spec_override.timeout: %w", err)
	}
	var timeoutPtr *time.Duration
	if dto.Timeout != "" {
		timeoutPtr = &timeout
	}
	var retryPolicy *domain.RetryPolicy
	if dto.RetryPolicy != nil {
		initialInterval, err := parseDuration(dto.RetryPolicy.InitialInterval)
		if err != nil {
			return domain.RequestSpecOverride{}, fmt.Errorf("request_spec_override.retry_policy.initial_interval: %w", err)
		}
		maxInterval, err := parseDuration(dto.RetryPolicy.MaxInterval)
		if err != nil {
			return domain.RequestSpecOverride{}, fmt.Errorf("request_spec_override.retry_policy.max_interval: %w", err)
		}
		policy := domain.RetryPolicy{
			Enabled:              dto.RetryPolicy.Enabled,
			MaxAttempts:          dto.RetryPolicy.MaxAttempts,
			BackoffStrategy:      domain.BackoffStrategy(dto.RetryPolicy.BackoffStrategy),
			InitialInterval:      initialInterval,
			MaxInterval:          maxInterval,
			RetryableStatusCodes: dto.RetryPolicy.RetryableStatusCodes,
		}
		retryPolicy = &policy
	}
	return domain.RequestSpecOverride{
		Headers:      dto.Headers,
		Query:        dto.Query,
		BodyTemplate: dto.BodyTemplate,
		Timeout:      timeoutPtr,
		RetryPolicy:  retryPolicy,
	}, nil
}

func (dto extractionSpecDTO) toDomain() domain.ExtractionSpec {
	rules := make([]domain.ExtractionRule, 0, len(dto.Rules))
	for _, rule := range dto.Rules {
		rules = append(rules, domain.ExtractionRule{
			Name:       rule.Name,
			Source:     domain.ExtractionSource(rule.Source),
			Selector:   rule.Selector,
			Required:   rule.Required,
			JSONEncode: rule.JSONEncode,
		})
	}
	return domain.ExtractionSpec{Rules: rules}
}

func (dto assertionSpecDTO) toDomain() domain.AssertionSpec {
	rules := make([]domain.AssertionRule, 0, len(dto.Rules))
	for _, rule := range dto.Rules {
		rules = append(rules, domain.AssertionRule{
			Name:           rule.Name,
			Target:         domain.AssertionTarget(rule.Target),
			Operator:       domain.AssertionOperator(rule.Operator),
			Selector:       rule.Selector,
			ExpectedValue:  rule.ExpectedValue,
			ExpectedValues: rule.ExpectedValues,
		})
	}
	return domain.AssertionSpec{Rules: rules}
}

func mapFlowDefinition(view usecase.FlowDefinitionView) flowDefinitionResponse {
	steps := make([]flowStepResponse, 0, len(view.Steps))
	for _, step := range view.Steps {
		steps = append(steps, mapFlowStep(step))
	}
	return flowDefinitionResponse{Flow: mapFlow(view.Flow), Steps: steps}
}

func mapFlow(flow domain.Flow) flowResponse {
	return flowResponse{
		WorkspaceID: string(flow.WorkspaceID),
		ID:          string(flow.ID),
		Name:        flow.Name,
		Description: flow.Description,
		Version:     flow.Version,
		Status:      string(flow.Status),
		CreatedAt:   formatTime(flow.CreatedAt),
		UpdatedAt:   formatTime(flow.UpdatedAt),
	}
}

func mapFlowStep(step domain.FlowStep) flowStepResponse {
	return flowStepResponse{
		ID:                  string(step.ID),
		FlowID:              string(step.FlowID),
		OrderIndex:          step.OrderIndex,
		Name:                step.Name,
		StepType:            string(step.Type()),
		SavedRequestID:      string(step.SavedRequestID),
		RequestSpec:         mapRequestSpec(step.RequestSpec),
		RequestSpecOverride: mapRequestSpecOverride(step.RequestSpecOverride),
		ExtractionSpec:      mapExtractionSpec(step.ExtractionSpec),
		AssertionSpec:       mapAssertionSpec(step.AssertionSpec),
		CreatedAt:           formatTime(step.CreatedAt),
		UpdatedAt:           formatTime(step.UpdatedAt),
	}
}

func mapRequestSpec(spec domain.RequestSpec) requestSpecDTO {
	return requestSpecDTO{
		Method:          spec.Method,
		URLTemplate:     spec.URLTemplate,
		Headers:         spec.Headers,
		Query:           spec.Query,
		BodyTemplate:    spec.BodyTemplate,
		Timeout:         formatDuration(spec.Timeout),
		FollowRedirects: spec.FollowRedirects,
		RetryPolicy: retryPolicyDTO{
			Enabled:              spec.RetryPolicy.Enabled,
			MaxAttempts:          spec.RetryPolicy.MaxAttempts,
			BackoffStrategy:      string(spec.RetryPolicy.BackoffStrategy),
			InitialInterval:      formatDuration(spec.RetryPolicy.InitialInterval),
			MaxInterval:          formatDuration(spec.RetryPolicy.MaxInterval),
			RetryableStatusCodes: spec.RetryPolicy.RetryableStatusCodes,
		},
	}
}

func mapRequestSpecOverride(spec domain.RequestSpecOverride) requestSpecOverrideDTO {
	var retryPolicy *retryPolicyDTO
	if spec.RetryPolicy != nil {
		retryPolicy = &retryPolicyDTO{
			Enabled:              spec.RetryPolicy.Enabled,
			MaxAttempts:          spec.RetryPolicy.MaxAttempts,
			BackoffStrategy:      string(spec.RetryPolicy.BackoffStrategy),
			InitialInterval:      formatDuration(spec.RetryPolicy.InitialInterval),
			MaxInterval:          formatDuration(spec.RetryPolicy.MaxInterval),
			RetryableStatusCodes: spec.RetryPolicy.RetryableStatusCodes,
		}
	}
	return requestSpecOverrideDTO{
		Headers:      spec.Headers,
		Query:        spec.Query,
		BodyTemplate: spec.BodyTemplate,
		Timeout:      formatDurationPtr(spec.Timeout),
		RetryPolicy:  retryPolicy,
	}
}

func mapExtractionSpec(spec domain.ExtractionSpec) extractionSpecDTO {
	rules := make([]extractionRuleDTO, 0, len(spec.Rules))
	for _, rule := range spec.Rules {
		rules = append(rules, extractionRuleDTO{Name: rule.Name, Source: string(rule.Source), Selector: rule.Selector, Required: rule.Required, JSONEncode: rule.JSONEncode})
	}
	return extractionSpecDTO{Rules: rules}
}

func mapAssertionSpec(spec domain.AssertionSpec) assertionSpecDTO {
	rules := make([]assertionRuleDTO, 0, len(spec.Rules))
	for _, rule := range spec.Rules {
		rules = append(rules, assertionRuleDTO{Name: rule.Name, Target: string(rule.Target), Operator: string(rule.Operator), Selector: rule.Selector, ExpectedValue: rule.ExpectedValue, ExpectedValues: rule.ExpectedValues})
	}
	return assertionSpecDTO{Rules: rules}
}

func mapRun(run domain.FlowRun) runResponse {
	return runResponse{
		ID:             string(run.ID),
		WorkspaceID:    string(run.WorkspaceID),
		TargetType:     string(run.Target()),
		FlowID:         string(run.FlowID),
		FlowVersion:    run.FlowVersion,
		SavedRequestID: string(run.SavedRequestID),
		Status:         string(run.Status),
		CreatedAt:      formatTime(run.CreatedAt),
		UpdatedAt:      formatTime(run.UpdatedAt),
		StartedAt:      formatTimePtr(run.StartedAt),
		FinishedAt:     formatTimePtr(run.FinishedAt),
		InitiatedBy:    run.InitiatedBy,
		QueueName:      run.QueueName,
		IdempotencyKey: run.IdempotencyKey,
		ClaimedBy:      run.ClaimedBy,
		HeartbeatAt:    formatTimePtr(run.HeartbeatAt),
		ErrorMessage:   run.ErrorMessage,
		Input:          decodeJSONRaw(run.InputJSON),
	}
}

func mapRunStep(step domain.FlowRunStep) runStepResponse {
	return runStepResponse{
		ID:               string(step.ID),
		RunID:            string(step.RunID),
		StepName:         step.StepName,
		StepOrder:        step.StepOrder,
		Status:           string(step.Status),
		RetryCount:       step.RetryCount,
		CreatedAt:        formatTime(step.CreatedAt),
		UpdatedAt:        formatTime(step.UpdatedAt),
		StartedAt:        formatTimePtr(step.StartedAt),
		FinishedAt:       formatTimePtr(step.FinishedAt),
		ErrorMessage:     step.ErrorMessage,
		RequestSnapshot:  decodeJSONRaw(step.RequestSnapshotJSON),
		ResponseSnapshot: decodeJSONRaw(step.ResponseSnapshotJSON),
		ExtractedValues:  decodeJSONRaw(step.ExtractedValuesJSON),
		AttemptHistory:   decodeJSONRaw(step.AttemptHistoryJSON),
	}
}

func mapRunEvent(event domain.RunEvent) runEventResponse {
	return runEventResponse{
		ID:             string(event.ID),
		RunID:          string(event.RunID),
		WorkspaceID:    string(event.WorkspaceID),
		FlowID:         string(event.FlowID),
		SavedRequestID: string(event.SavedRequestID),
		Sequence:       event.Sequence,
		EventType:      string(event.EventType),
		Level:          string(event.Level),
		StepName:       event.StepName,
		StepOrder:      event.StepOrder,
		Attempt:        event.Attempt,
		Message:        event.Message,
		CreatedAt:      formatTime(event.CreatedAt),
		Details:        parseJSON(event.DetailsJSON),
	}
}

func parseJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	return value
}

func parseDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func formatDuration(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return value.String()
}

func formatDurationPtr(value *time.Duration) string {
	if value == nil {
		return ""
	}
	return formatDuration(*value)
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func formatTimePtr(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func decodeJSONRaw(payload json.RawMessage) any {
	if len(payload) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return string(payload)
	}
	return value
}
