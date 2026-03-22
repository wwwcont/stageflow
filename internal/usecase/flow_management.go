package usecase

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type FlowManagementService struct {
	workspaces    repository.WorkspaceRepository
	savedRequests repository.SavedRequestRepository
	flows         repository.FlowRepository
	flowSteps     repository.FlowStepRepository
	clock         clock.Clock
}

func NewFlowManagementService(workspaces repository.WorkspaceRepository, savedRequests repository.SavedRequestRepository, flows repository.FlowRepository, flowSteps repository.FlowStepRepository, clk clock.Clock) (*FlowManagementService, error) {
	switch {
	case workspaces == nil:
		return nil, fmt.Errorf("workspace repository is required")
	case savedRequests == nil:
		return nil, fmt.Errorf("saved request repository is required")
	case flows == nil:
		return nil, fmt.Errorf("flow repository is required")
	case flowSteps == nil:
		return nil, fmt.Errorf("flow step repository is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &FlowManagementService{workspaces: workspaces, savedRequests: savedRequests, flows: flows, flowSteps: flowSteps, clock: clk}, nil
}

func (s *FlowManagementService) CreateFlow(ctx context.Context, cmd CreateFlowCommand) (FlowDefinitionView, error) {
	workspace, err := s.requireActiveWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return FlowDefinitionView{}, err
	}
	flow, steps, issues, err := s.prepareNewDefinition(ctx, cmd, workspace)
	if err != nil {
		return FlowDefinitionView{}, err
	}
	issues = append(issues, s.validateWorkspaceFlowLimits(ctx, workspace, steps)...)
	if len(issues) > 0 {
		return FlowDefinitionView{}, &domain.ValidationError{Message: strings.Join(issues, "; ")}
	}
	if err := s.flows.Create(ctx, flow); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("create flow %q: %w", flow.ID, err)
	}
	if err := s.flowSteps.CreateMany(ctx, steps); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("create flow %q steps: %w", flow.ID, err)
	}
	if err := s.flowSteps.ReplaceByFlowVersion(ctx, flow.ID, flow.Version, steps); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("snapshot flow %q version %d steps: %w", flow.ID, flow.Version, err)
	}
	return FlowDefinitionView{Flow: flow, Steps: steps}, nil
}

func (s *FlowManagementService) UpdateFlow(ctx context.Context, cmd UpdateFlowCommand) (FlowDefinitionView, error) {
	existing, err := s.flows.GetByID(ctx, cmd.FlowID)
	if err != nil {
		return FlowDefinitionView{}, fmt.Errorf("get flow %q: %w", cmd.FlowID, err)
	}
	if cmd.WorkspaceID != "" && cmd.WorkspaceID != existing.WorkspaceID {
		return FlowDefinitionView{}, &domain.ValidationError{Message: "flow workspace_id cannot be changed"}
	}
	workspace, err := s.requireActiveWorkspace(ctx, existing.WorkspaceID)
	if err != nil {
		return FlowDefinitionView{}, err
	}
	flow, steps, issues, err := s.prepareUpdatedDefinition(ctx, existing, cmd, workspace)
	if err != nil {
		return FlowDefinitionView{}, err
	}
	issues = append(issues, s.validateResolvedWorkspacePolicy(ctx, workspace, steps)...)
	if len(issues) > 0 {
		return FlowDefinitionView{}, &domain.ValidationError{Message: strings.Join(issues, "; ")}
	}
	if err := s.flows.Update(ctx, flow); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("update flow %q: %w", flow.ID, err)
	}
	if err := s.flowSteps.ReplaceByFlowID(ctx, flow.ID, steps); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("replace flow %q steps: %w", flow.ID, err)
	}
	if err := s.flowSteps.ReplaceByFlowVersion(ctx, flow.ID, flow.Version, steps); err != nil {
		return FlowDefinitionView{}, fmt.Errorf("snapshot flow %q version %d steps: %w", flow.ID, flow.Version, err)
	}
	return FlowDefinitionView{Flow: flow, Steps: steps}, nil
}

func (s *FlowManagementService) GetFlow(ctx context.Context, query GetFlowQuery) (FlowDefinitionView, error) {
	var (
		flow domain.Flow
		err  error
	)
	if query.Version > 0 {
		flow, err = s.flows.GetVersion(ctx, query.FlowID, query.Version)
		if err != nil {
			return FlowDefinitionView{}, fmt.Errorf("get flow %q version %d: %w", query.FlowID, query.Version, err)
		}
	} else {
		flow, err = s.flows.GetByID(ctx, query.FlowID)
		if err != nil {
			return FlowDefinitionView{}, fmt.Errorf("get flow %q: %w", query.FlowID, err)
		}
	}
	if query.WorkspaceID != "" && flow.WorkspaceID != query.WorkspaceID {
		return FlowDefinitionView{}, &domain.NotFoundError{Entity: "flow", ID: string(query.FlowID)}
	}
	var steps []domain.FlowStep
	if query.Version > 0 {
		steps, err = s.flowSteps.ListByFlowVersion(ctx, query.FlowID, query.Version)
		if err != nil {
			return FlowDefinitionView{}, fmt.Errorf("list flow %q version %d steps: %w", query.FlowID, query.Version, err)
		}
	} else {
		steps, err = s.flowSteps.ListByFlowID(ctx, query.FlowID)
		if err != nil {
			return FlowDefinitionView{}, fmt.Errorf("list flow %q steps: %w", query.FlowID, err)
		}
	}
	return FlowDefinitionView{Flow: flow, Steps: steps}, nil
}

func (s *FlowManagementService) ListFlows(ctx context.Context, query ListFlowsQuery) ([]domain.Flow, error) {
	flows, err := s.flows.List(ctx, repository.FlowListFilter{
		WorkspaceID: query.WorkspaceID,
		Statuses:    query.Statuses,
		NameLike:    query.NameLike,
		Limit:       query.Limit,
		Offset:      query.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	return flows, nil
}

func (s *FlowManagementService) ValidateFlow(ctx context.Context, cmd ValidateFlowCommand) (FlowValidationResult, error) {
	var workspace domain.Workspace
	var err error
	if cmd.WorkspaceID != "" {
		workspace, err = s.workspaces.GetByID(ctx, cmd.WorkspaceID)
		if err != nil {
			return FlowValidationResult{}, err
		}
	}
	_, steps, issues, err := s.prepareValidationDefinition(ctx, cmd, workspace)
	if err != nil {
		return FlowValidationResult{}, err
	}
	if workspace.ID != "" {
		issues = append(issues, s.validateResolvedWorkspacePolicy(ctx, workspace, steps)...)
	}
	return FlowValidationResult{Valid: len(issues) == 0, Issues: issues}, nil
}

func (s *FlowManagementService) prepareNewDefinition(ctx context.Context, cmd CreateFlowCommand, workspace domain.Workspace) (domain.Flow, []domain.FlowStep, []string, error) {
	now := s.clock.Now().UTC()
	flow := domain.Flow{
		WorkspaceID: cmd.WorkspaceID,
		ID:          cmd.FlowID,
		Name:        strings.TrimSpace(cmd.Name),
		Description: strings.TrimSpace(cmd.Description),
		Version:     1,
		Status:      cmd.Status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	steps := materializeSteps(cmd.FlowID, cmd.Steps, workspace.Policy, now)
	issues := s.validateFlowDefinition(ctx, workspace, flow, steps)
	return flow, steps, issues, nil
}

func (s *FlowManagementService) prepareUpdatedDefinition(ctx context.Context, existing domain.Flow, cmd UpdateFlowCommand, workspace domain.Workspace) (domain.Flow, []domain.FlowStep, []string, error) {
	now := s.clock.Now().UTC()
	flow := domain.Flow{
		WorkspaceID: existing.WorkspaceID,
		ID:          existing.ID,
		Name:        strings.TrimSpace(cmd.Name),
		Description: strings.TrimSpace(cmd.Description),
		Version:     existing.Version + 1,
		Status:      cmd.Status,
		CreatedAt:   existing.CreatedAt,
		UpdatedAt:   now,
	}
	steps := materializeSteps(existing.ID, cmd.Steps, workspace.Policy, now)
	issues := s.validateFlowDefinition(ctx, workspace, flow, steps)
	return flow, steps, issues, nil
}

func (s *FlowManagementService) prepareValidationDefinition(ctx context.Context, cmd ValidateFlowCommand, workspace domain.Workspace) (domain.Flow, []domain.FlowStep, []string, error) {
	now := s.clock.Now().UTC()
	flow := domain.Flow{
		WorkspaceID: cmd.WorkspaceID,
		ID:          cmd.FlowID,
		Name:        strings.TrimSpace(cmd.Name),
		Description: strings.TrimSpace(cmd.Description),
		Version:     1,
		Status:      cmd.Status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	steps := materializeSteps(cmd.FlowID, cmd.Steps, workspace.Policy, now)
	issues := s.validateFlowDefinition(ctx, workspace, flow, steps)
	return flow, steps, issues, nil
}

func materializeSteps(flowID domain.FlowID, drafts []FlowStepDraft, policy domain.WorkspacePolicy, now time.Time) []domain.FlowStep {
	steps := make([]domain.FlowStep, 0, len(drafts))
	for _, draft := range drafts {
		requestSpec := draft.RequestSpec
		stepType := draft.StepType
		if stepType == "" && draft.SavedRequestID == "" {
			stepType = domain.FlowStepTypeInlineRequest
		}
		if stepType == domain.FlowStepTypeInlineRequest || (stepType == "" && draft.SavedRequestID == "") {
			requestSpec = applyWorkspacePolicyDefaults(draft.RequestSpec, policy)
		}
		steps = append(steps, domain.FlowStep{
			ID:                  draft.ID,
			FlowID:              flowID,
			OrderIndex:          draft.OrderIndex,
			Name:                strings.TrimSpace(draft.Name),
			StepType:            stepType,
			SavedRequestID:      draft.SavedRequestID,
			RequestSpec:         requestSpec,
			RequestSpecOverride: draft.RequestSpecOverride,
			ExtractionSpec:      draft.ExtractionSpec,
			AssertionSpec:       draft.AssertionSpec,
			CreatedAt:           now,
			UpdatedAt:           now,
		})
	}
	return steps
}

var _ FlowManagementUseCase = (*FlowManagementService)(nil)

func (s *FlowManagementService) requireActiveWorkspace(ctx context.Context, workspaceID domain.WorkspaceID) (domain.Workspace, error) {
	workspace, err := s.workspaces.GetByID(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace %q: %w", workspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.Workspace{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	return workspace, nil
}

func (s *FlowManagementService) validateWorkspaceFlowLimits(ctx context.Context, workspace domain.Workspace, steps []domain.FlowStep) []string {
	issues := s.validateResolvedWorkspacePolicy(ctx, workspace, steps)
	flows, err := s.flows.List(ctx, repository.FlowListFilter{WorkspaceID: workspace.ID})
	if err != nil {
		return append(issues, fmt.Sprintf("list workspace %q flows: %s", workspace.ID, err.Error()))
	}
	if len(flows) >= workspace.Policy.MaxFlows {
		issues = append(issues, fmt.Sprintf("workspace %q reached max_flows limit %d", workspace.ID, workspace.Policy.MaxFlows))
	}
	return issues
}

func (s *FlowManagementService) validateResolvedWorkspacePolicy(ctx context.Context, workspace domain.Workspace, steps []domain.FlowStep) []string {
	resolved := make([]domain.FlowStep, 0, len(steps))
	issues := make([]string, 0)
	for _, step := range steps {
		effectiveStep, stepIssues := s.resolveFlowStep(ctx, workspace.ID, workspace.Policy, step)
		if len(stepIssues) > 0 {
			issues = append(issues, stepIssues...)
			continue
		}
		resolved = append(resolved, effectiveStep)
	}
	issues = append(issues, validateWorkspacePolicy(workspace.Policy, resolved)...)
	return dedupeStrings(issues)
}

func (s *FlowManagementService) validateFlowDefinition(ctx context.Context, workspace domain.Workspace, flow domain.Flow, steps []domain.FlowStep) []string {
	issues := make([]string, 0)
	if err := flow.Validate(); err != nil {
		issues = append(issues, err.Error())
	}
	if len(steps) == 0 {
		if flow.Status == domain.FlowStatusDraft {
			return issues
		}
		issues = append(issues, "flow must contain at least one step unless it is in draft status")
		return issues
	}

	sort.Slice(steps, func(i, j int) bool { return steps[i].OrderIndex < steps[j].OrderIndex })

	seenNames := make(map[string]struct{}, len(steps))
	seenOrders := make(map[int]struct{}, len(steps))
	scope := newTemplateValidationScope(workspace)
	for idx, step := range steps {
		if err := step.Validate(); err != nil {
			issues = append(issues, err.Error())
		}
		effectiveStep, stepIssues := s.resolveFlowStep(ctx, flow.WorkspaceID, workspace.Policy, step)
		issues = append(issues, stepIssues...)

		canonicalName := strings.ToLower(step.Name)
		if _, exists := seenNames[canonicalName]; exists {
			issues = append(issues, fmt.Sprintf("duplicate step name %q", step.Name))
		} else {
			seenNames[canonicalName] = struct{}{}
		}
		if _, exists := seenOrders[step.OrderIndex]; exists {
			issues = append(issues, fmt.Sprintf("duplicate step order_index %d", step.OrderIndex))
		} else {
			seenOrders[step.OrderIndex] = struct{}{}
		}
		if step.OrderIndex != idx {
			issues = append(issues, fmt.Sprintf("step %q order_index must be contiguous and expected %d, got %d", step.Name, idx, step.OrderIndex))
		}

		issues = append(issues, validateStepTemplates(effectiveStep, scope)...)
		scope.PriorSteps[step.Name] = struct{}{}
		for _, rule := range step.ExtractionSpec.Rules {
			scope.RuntimeVars[rule.Name] = struct{}{}
		}
	}

	return dedupeStrings(issues)
}

func (s *FlowManagementService) resolveFlowStep(ctx context.Context, workspaceID domain.WorkspaceID, policy domain.WorkspacePolicy, step domain.FlowStep) (domain.FlowStep, []string) {
	switch step.Type() {
	case domain.FlowStepTypeInlineRequest:
		resolved := step
		resolved.RequestSpec = applyWorkspacePolicyDefaults(step.RequestSpec, policy)
		return resolved, nil
	case domain.FlowStepTypeSavedRequestRef:
		request, err := s.savedRequests.GetByID(ctx, step.SavedRequestID)
		if err != nil {
			return step, []string{fmt.Sprintf("step %q references unknown saved request %q", step.Name, step.SavedRequestID)}
		}
		if request.WorkspaceID != workspaceID {
			return step, []string{fmt.Sprintf("step %q saved_request_id %q belongs to workspace %q instead of %q", step.Name, step.SavedRequestID, request.WorkspaceID, workspaceID)}
		}
		effective, err := step.RequestSpecOverride.Apply(request.RequestSpec)
		if err != nil {
			return step, []string{fmt.Sprintf("step %q request_spec_override is invalid: %v", step.Name, err)}
		}
		resolved := step
		resolved.RequestSpec = applyWorkspacePolicyDefaults(effective, policy)
		return resolved, nil
	default:
		return step, []string{fmt.Sprintf("step %q uses unsupported step_type %q", step.Name, step.StepType)}
	}
}

func validateWorkspacePolicy(policy domain.WorkspacePolicy, steps []domain.FlowStep) []string {
	issues := make([]string, 0)
	if len(steps) > policy.MaxStepsPerFlow {
		issues = append(issues, fmt.Sprintf("flow exceeds workspace max_steps_per_flow limit %d", policy.MaxStepsPerFlow))
	}
	for _, step := range steps {
		if len(step.RequestSpec.BodyTemplate) > policy.MaxRequestBodyBytes {
			issues = append(issues, fmt.Sprintf("step %q exceeds workspace max_request_body_bytes limit %d", step.Name, policy.MaxRequestBodyBytes))
		}
		if step.RequestSpec.Timeout > time.Duration(policy.MaxRunDurationSeconds)*time.Second {
			issues = append(issues, fmt.Sprintf("step %q timeout exceeds workspace max_run_duration_seconds limit %d", step.Name, policy.MaxRunDurationSeconds))
		}
		if issue := validateWorkspaceRequestHost(policy.AllowedHosts, step); issue != "" {
			issues = append(issues, issue)
		}
	}
	return issues
}

func applyWorkspacePolicyDefaults(spec domain.RequestSpec, policy domain.WorkspacePolicy) domain.RequestSpec {
	if policy.DefaultTimeoutMS > 0 && spec.Timeout <= 0 {
		spec.Timeout = time.Duration(policy.DefaultTimeoutMS) * time.Millisecond
	}
	if isZeroRetryPolicy(spec.RetryPolicy) && !isZeroRetryPolicy(policy.DefaultRetryPolicy) {
		spec.RetryPolicy = policy.DefaultRetryPolicy
	}
	return spec
}

func isZeroRetryPolicy(policy domain.RetryPolicy) bool {
	return !policy.Enabled &&
		policy.MaxAttempts == 0 &&
		policy.BackoffStrategy == "" &&
		policy.InitialInterval == 0 &&
		policy.MaxInterval == 0 &&
		len(policy.RetryableStatusCodes) == 0
}

func validateWorkspaceRequestHost(allowedHosts []string, step domain.FlowStep) string {
	if strings.Contains(step.RequestSpec.URLTemplate, "{{") {
		return ""
	}
	parsed, err := url.Parse(step.RequestSpec.URLTemplate)
	if err != nil || parsed.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	expandedAllowedHosts := expandLoopbackAllowedHosts(allowedHosts)
	for _, allowed := range expandedAllowedHosts {
		if host == allowed {
			return ""
		}
	}
	return fmt.Sprintf("step %q host %q is outside workspace allowed_hosts", step.Name, host)
}

func expandLoopbackAllowedHosts(hosts []string) []string {
	seen := make(map[string]struct{}, len(hosts)+2)
	expanded := make([]string, 0, len(hosts)+2)
	for _, host := range hosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; !ok {
			seen[normalized] = struct{}{}
			expanded = append(expanded, normalized)
		}
		if normalized == "localhost" || isLoopbackHost(normalized) {
			for _, alias := range []string{"localhost", "127.0.0.1", "::1"} {
				if _, ok := seen[alias]; ok {
					continue
				}
				seen[alias] = struct{}{}
				expanded = append(expanded, alias)
			}
		}
	}
	return expanded
}

func isLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]"))
	return ip != nil && ip.IsLoopback()
}

func dedupeStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
