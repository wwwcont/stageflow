package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"stageflow/internal/domain"
)

// In-memory repositories keep the scaffold runnable without coupling the application
// to a concrete database before the storage contract is finalized.

type InMemoryFlowRepository struct {
	mu    sync.RWMutex
	flows map[domain.FlowID]domain.Flow
}

type InMemoryWorkspaceRepository struct {
	mu         sync.RWMutex
	workspaces map[domain.WorkspaceID]domain.Workspace
	bySlug     map[string]domain.WorkspaceID
}

type InMemorySavedRequestRepository struct {
	mu       sync.RWMutex
	requests map[domain.SavedRequestID]domain.SavedRequest
}

func NewInMemoryWorkspaceRepository(seed ...domain.Workspace) *InMemoryWorkspaceRepository {
	workspaces := make(map[domain.WorkspaceID]domain.Workspace, len(seed)+1)
	bySlug := make(map[string]domain.WorkspaceID, len(seed)+1)
	if len(seed) == 0 {
		now := time.Now().UTC()
		seed = []domain.Workspace{{
			ID:          "bootstrap",
			Name:        "Bootstrap workspace",
			Slug:        "bootstrap",
			Description: "Default workspace used by the in-memory scaffold.",
			OwnerTeam:   "platform",
			Status:      domain.WorkspaceStatusActive,
			Policy: domain.WorkspacePolicy{
				AllowedHosts:          []string{"example.internal", "host.docker.internal"},
				MaxSavedRequests:      100,
				MaxFlows:              100,
				MaxStepsPerFlow:       25,
				MaxRequestBodyBytes:   1 << 20,
				DefaultTimeoutMS:      10000,
				MaxRunDurationSeconds: 600,
				DefaultRetryPolicy:    domain.RetryPolicy{Enabled: false},
			},
			CreatedAt: now,
			UpdatedAt: now,
		}}
	}
	for _, workspace := range seed {
		workspace = workspace.Normalized()
		workspaces[workspace.ID] = workspace
		bySlug[workspace.Slug] = workspace.ID
	}
	return &InMemoryWorkspaceRepository{workspaces: workspaces, bySlug: bySlug}
}

func NewInMemoryFlowRepository() *InMemoryFlowRepository {
	now := time.Now().UTC()
	bootstrap := domain.Flow{
		WorkspaceID: "bootstrap",
		ID:          "bootstrap",
		Name:        "Bootstrap flow",
		Description: "A sample flow reserved for wiring and local smoke checks.",
		Version:     1,
		Status:      domain.FlowStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	return &InMemoryFlowRepository{flows: map[domain.FlowID]domain.Flow{bootstrap.ID: bootstrap}}
}

func NewInMemorySavedRequestRepository(seed ...domain.SavedRequest) *InMemorySavedRequestRepository {
	requests := make(map[domain.SavedRequestID]domain.SavedRequest, len(seed))
	for _, request := range seed {
		request = request.Normalized()
		requests[request.ID] = request
	}
	return &InMemorySavedRequestRepository{requests: requests}
}

func (r *InMemoryWorkspaceRepository) Create(_ context.Context, workspace domain.Workspace) error {
	workspace = workspace.Normalized()
	if err := workspace.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.workspaces[workspace.ID]; exists {
		return &domain.ConflictError{Entity: "workspace", Field: "id", Value: string(workspace.ID)}
	}
	if _, exists := r.bySlug[workspace.Slug]; exists {
		return &domain.ConflictError{Entity: "workspace", Field: "slug", Value: workspace.Slug}
	}
	r.workspaces[workspace.ID] = workspace
	r.bySlug[workspace.Slug] = workspace.ID
	return nil
}

func (r *InMemoryWorkspaceRepository) Update(_ context.Context, workspace domain.Workspace) error {
	workspace = workspace.Normalized()
	if err := workspace.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, exists := r.workspaces[workspace.ID]
	if !exists {
		return &domain.NotFoundError{Entity: "workspace", ID: string(workspace.ID)}
	}
	if ownerID, ok := r.bySlug[workspace.Slug]; ok && ownerID != workspace.ID {
		return &domain.ConflictError{Entity: "workspace", Field: "slug", Value: workspace.Slug}
	}
	if existing.Slug != workspace.Slug {
		delete(r.bySlug, existing.Slug)
	}
	r.workspaces[workspace.ID] = workspace
	r.bySlug[workspace.Slug] = workspace.ID
	return nil
}

func (r *InMemoryWorkspaceRepository) GetByID(_ context.Context, id domain.WorkspaceID) (domain.Workspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	workspace, ok := r.workspaces[id]
	if !ok {
		return domain.Workspace{}, &domain.NotFoundError{Entity: "workspace", ID: string(id)}
	}
	return workspace, nil
}

func (r *InMemoryWorkspaceRepository) GetBySlug(_ context.Context, slug string) (domain.Workspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.bySlug[strings.ToLower(strings.TrimSpace(slug))]
	if !ok {
		return domain.Workspace{}, &domain.NotFoundError{Entity: "workspace", ID: slug}
	}
	return r.workspaces[id], nil
}

func (r *InMemoryWorkspaceRepository) List(_ context.Context, filter WorkspaceListFilter) ([]domain.Workspace, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Workspace, 0, len(r.workspaces))
	for _, workspace := range r.workspaces {
		if filter.NameLike != "" && !strings.Contains(strings.ToLower(workspace.Name), strings.ToLower(filter.NameLike)) {
			continue
		}
		if len(filter.Statuses) > 0 && !containsWorkspaceStatus(filter.Statuses, workspace.Status) {
			continue
		}
		result = append(result, workspace)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return sliceWindow(result, filter.Offset, filter.Limit), nil
}

func (r *InMemoryFlowRepository) Create(_ context.Context, flow domain.Flow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.flows[flow.ID]; exists {
		return &domain.ConflictError{Entity: "flow", Field: "id", Value: string(flow.ID)}
	}
	r.flows[flow.ID] = flow
	return nil
}

func (r *InMemoryFlowRepository) Update(_ context.Context, flow domain.Flow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.flows[flow.ID]; !exists {
		return &domain.NotFoundError{Entity: "flow", ID: string(flow.ID)}
	}
	r.flows[flow.ID] = flow
	return nil
}

func (r *InMemoryFlowRepository) GetByID(_ context.Context, id domain.FlowID) (domain.Flow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	flow, ok := r.flows[id]
	if !ok {
		return domain.Flow{}, &domain.NotFoundError{Entity: "flow", ID: string(id)}
	}
	return flow, nil
}

func (r *InMemoryFlowRepository) List(_ context.Context, filter FlowListFilter) ([]domain.Flow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.Flow, 0, len(r.flows))
	for _, flow := range r.flows {
		if filter.WorkspaceID != "" && flow.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.NameLike != "" && !strings.Contains(strings.ToLower(flow.Name), strings.ToLower(filter.NameLike)) {
			continue
		}
		if len(filter.Statuses) > 0 && !containsFlowStatus(filter.Statuses, flow.Status) {
			continue
		}
		result = append(result, flow)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return sliceWindow(result, filter.Offset, filter.Limit), nil
}

func (r *InMemorySavedRequestRepository) Create(_ context.Context, request domain.SavedRequest) error {
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.requests[request.ID]; exists {
		return &domain.ConflictError{Entity: "saved request", Field: "id", Value: string(request.ID)}
	}
	r.requests[request.ID] = request
	return nil
}

func (r *InMemorySavedRequestRepository) Update(_ context.Context, request domain.SavedRequest) error {
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.requests[request.ID]; !exists {
		return &domain.NotFoundError{Entity: "saved request", ID: string(request.ID)}
	}
	r.requests[request.ID] = request
	return nil
}

func (r *InMemorySavedRequestRepository) GetByID(_ context.Context, id domain.SavedRequestID) (domain.SavedRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	request, ok := r.requests[id]
	if !ok {
		return domain.SavedRequest{}, &domain.NotFoundError{Entity: "saved request", ID: string(id)}
	}
	return request, nil
}

func (r *InMemorySavedRequestRepository) List(_ context.Context, filter SavedRequestListFilter) ([]domain.SavedRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.SavedRequest, 0, len(r.requests))
	for _, request := range r.requests {
		if filter.WorkspaceID != "" && request.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.NameLike != "" && !strings.Contains(strings.ToLower(request.Name), strings.ToLower(filter.NameLike)) {
			continue
		}
		result = append(result, request)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return sliceWindow(result, filter.Offset, filter.Limit), nil
}

type InMemoryFlowStepRepository struct {
	mu    sync.RWMutex
	steps map[domain.FlowID][]domain.FlowStep
}

func NewInMemoryFlowStepRepository() *InMemoryFlowStepRepository {
	now := time.Now().UTC()
	return &InMemoryFlowStepRepository{steps: map[domain.FlowID][]domain.FlowStep{
		"bootstrap": {
			{
				ID:         "bootstrap-step-1",
				FlowID:     "bootstrap",
				OrderIndex: 0,
				Name:       "bootstrap request",
				RequestSpec: domain.RequestSpec{
					Method:      "GET",
					URLTemplate: "https://example.internal/health",
					Timeout:     3 * time.Second,
				},
				ExtractionSpec: domain.ExtractionSpec{Rules: []domain.ExtractionRule{{Name: "status", Source: domain.ExtractionSourceStatus}}},
				AssertionSpec:  domain.AssertionSpec{Rules: []domain.AssertionRule{{Name: "expect-200", Target: domain.AssertionTargetStatusCode, Operator: domain.AssertionOperatorEquals, ExpectedValue: "200"}}},
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
	}}
}

func (r *InMemoryFlowStepRepository) CreateMany(_ context.Context, steps []domain.FlowStep) error {
	if len(steps) == 0 {
		return nil
	}
	flowID := steps[0].FlowID
	for _, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
		if step.FlowID != flowID {
			return &domain.ValidationError{Message: "flow step flow_id must match create batch flow_id"}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := make([]domain.FlowStep, len(steps))
	copy(cloned, steps)
	r.steps[flowID] = append(r.steps[flowID], cloned...)
	sort.Slice(r.steps[flowID], func(i, j int) bool { return r.steps[flowID][i].OrderIndex < r.steps[flowID][j].OrderIndex })
	return nil
}

func (r *InMemoryFlowStepRepository) ReplaceByFlowID(_ context.Context, flowID domain.FlowID, steps []domain.FlowStep) error {
	for _, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
		if step.FlowID != flowID {
			return &domain.ValidationError{Message: "flow step flow_id must match replace target flow_id"}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := make([]domain.FlowStep, len(steps))
	copy(cloned, steps)
	r.steps[flowID] = cloned
	sort.Slice(r.steps[flowID], func(i, j int) bool { return r.steps[flowID][i].OrderIndex < r.steps[flowID][j].OrderIndex })
	return nil
}

func (r *InMemoryFlowStepRepository) ListByFlowID(_ context.Context, flowID domain.FlowID) ([]domain.FlowStep, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	steps := r.steps[flowID]
	cloned := make([]domain.FlowStep, len(steps))
	copy(cloned, steps)
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].OrderIndex < cloned[j].OrderIndex })
	return cloned, nil
}

type InMemoryRunRepository struct {
	mu   sync.RWMutex
	runs map[domain.RunID]domain.FlowRun
}

func NewInMemoryRunRepository() *InMemoryRunRepository {
	return &InMemoryRunRepository{runs: make(map[domain.RunID]domain.FlowRun)}
}

func (r *InMemoryRunRepository) Create(_ context.Context, run domain.FlowRun) error {
	if err := run.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.ID]; exists {
		return &domain.ConflictError{Entity: "flow run", Field: "id", Value: string(run.ID)}
	}
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	r.runs[run.ID] = run
	return nil
}

func (r *InMemoryRunRepository) UpdateStatus(_ context.Context, runID domain.RunID, status domain.RunStatus, startedAt, finishedAt *time.Time, errorMessage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, exists := r.runs[runID]
	if !exists {
		return &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	run.Status = status
	run.StartedAt = startedAt
	run.FinishedAt = finishedAt
	if status != domain.RunStatusRunning {
		run.ClaimedBy = ""
		run.HeartbeatAt = nil
	}
	run.ErrorMessage = errorMessage
	run.UpdatedAt = time.Now().UTC()
	r.runs[runID] = run
	return nil
}

func (r *InMemoryRunRepository) TransitionStatus(_ context.Context, runID domain.RunID, expected []domain.RunStatus, transition RunStatusTransition) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, exists := r.runs[runID]
	if !exists {
		return &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	if len(expected) > 0 && !containsRunStatus(expected, run.Status) {
		return &domain.ConflictError{Entity: "flow run", Field: "status", Value: string(run.Status)}
	}
	run.Status = transition.Status
	if transition.StartedAt != nil {
		run.StartedAt = transition.StartedAt
	}
	if transition.FinishedAt != nil {
		run.FinishedAt = transition.FinishedAt
	}
	if transition.Status != domain.RunStatusRunning {
		run.ClaimedBy = ""
		run.HeartbeatAt = nil
	}
	run.ErrorMessage = transition.ErrorMessage
	run.UpdatedAt = time.Now().UTC()
	r.runs[runID] = run
	return nil
}

func (r *InMemoryRunRepository) ClaimForExecution(_ context.Context, runID domain.RunID, workerID string, now, staleBefore time.Time) (domain.FlowRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, exists := r.runs[runID]
	if !exists {
		return domain.FlowRun{}, &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	switch run.Status {
	case domain.RunStatusPending, domain.RunStatusQueued:
		run = run.Claim(workerID, now)
	case domain.RunStatusRunning:
		if run.ClaimedBy != "" && run.ClaimedBy != workerID && run.HeartbeatAt != nil && run.HeartbeatAt.After(staleBefore) {
			return domain.FlowRun{}, &domain.ConflictError{Entity: "flow run", Field: "claimed_by", Value: run.ClaimedBy}
		}
		run = run.Claim(workerID, now)
	case domain.RunStatusSucceeded, domain.RunStatusFailed, domain.RunStatusCanceled:
		return run, nil
	default:
		return domain.FlowRun{}, &domain.ValidationError{Message: "unsupported run status for claim"}
	}
	r.runs[runID] = run
	return run, nil
}

func (r *InMemoryRunRepository) Heartbeat(_ context.Context, runID domain.RunID, workerID string, heartbeatAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	run, exists := r.runs[runID]
	if !exists {
		return &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	if run.Status != domain.RunStatusRunning {
		return &domain.ConflictError{Entity: "flow run", Field: "status", Value: string(run.Status)}
	}
	if run.ClaimedBy != "" && run.ClaimedBy != workerID {
		return &domain.ConflictError{Entity: "flow run", Field: "claimed_by", Value: run.ClaimedBy}
	}
	run.ClaimedBy = workerID
	run.HeartbeatAt = &heartbeatAt
	run.UpdatedAt = heartbeatAt.UTC()
	r.runs[runID] = run
	return nil
}

func (r *InMemoryRunRepository) FindByIdempotencyKey(_ context.Context, workspaceID domain.WorkspaceID, flowID domain.FlowID, idempotencyKey string) (domain.FlowRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.runs {
		if run.WorkspaceID == workspaceID && run.FlowID == flowID && run.IdempotencyKey == idempotencyKey {
			return run, nil
		}
	}
	return domain.FlowRun{}, &domain.NotFoundError{Entity: "flow run", ID: idempotencyKey}
}

func (r *InMemoryRunRepository) GetByID(_ context.Context, runID domain.RunID) (domain.FlowRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[runID]
	if !ok {
		return domain.FlowRun{}, &domain.NotFoundError{Entity: "flow run", ID: string(runID)}
	}
	return run, nil
}

func (r *InMemoryRunRepository) List(_ context.Context, filter RunListFilter) ([]domain.FlowRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]domain.FlowRun, 0, len(r.runs))
	for _, run := range r.runs {
		if filter.WorkspaceID != "" && run.WorkspaceID != filter.WorkspaceID {
			continue
		}
		if filter.FlowID != "" && run.FlowID != filter.FlowID {
			continue
		}
		if filter.InitiatedBy != "" && run.InitiatedBy != filter.InitiatedBy {
			continue
		}
		if len(filter.Statuses) > 0 && !containsRunStatus(filter.Statuses, run.Status) {
			continue
		}
		result = append(result, run)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return sliceWindow(result, filter.Offset, filter.Limit), nil
}

type InMemoryRunStepRepository struct {
	mu    sync.RWMutex
	steps map[domain.RunID][]domain.FlowRunStep
}

type InMemoryRunEventRepository struct {
	mu       sync.RWMutex
	events   map[domain.RunID][]domain.RunEvent
	sequence map[domain.RunID]int64
}

func NewInMemoryRunStepRepository() *InMemoryRunStepRepository {
	return &InMemoryRunStepRepository{steps: make(map[domain.RunID][]domain.FlowRunStep)}
}

func NewInMemoryRunEventRepository() *InMemoryRunEventRepository {
	return &InMemoryRunEventRepository{events: make(map[domain.RunID][]domain.RunEvent), sequence: make(map[domain.RunID]int64)}
}

func (r *InMemoryRunStepRepository) Create(_ context.Context, step domain.FlowRunStep) error {
	if err := step.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.steps[step.RunID] {
		if existing.ID == step.ID || existing.StepOrder == step.StepOrder {
			return &domain.ConflictError{Entity: "flow run step", Field: "id", Value: string(step.ID)}
		}
	}
	if step.CreatedAt.IsZero() {
		step.CreatedAt = time.Now().UTC()
	}
	if step.UpdatedAt.IsZero() {
		step.UpdatedAt = step.CreatedAt
	}
	r.steps[step.RunID] = append(r.steps[step.RunID], step)
	sort.Slice(r.steps[step.RunID], func(i, j int) bool { return r.steps[step.RunID][i].StepOrder < r.steps[step.RunID][j].StepOrder })
	return nil
}

func (r *InMemoryRunStepRepository) Update(_ context.Context, step domain.FlowRunStep) error {
	if err := step.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	steps, ok := r.steps[step.RunID]
	if !ok {
		return &domain.NotFoundError{Entity: "flow run step", ID: string(step.ID)}
	}
	for idx := range steps {
		if steps[idx].ID == step.ID {
			step.UpdatedAt = time.Now().UTC()
			steps[idx] = step
			r.steps[step.RunID] = steps
			return nil
		}
	}
	return &domain.NotFoundError{Entity: "flow run step", ID: string(step.ID)}
}

func (r *InMemoryRunStepRepository) ListByRunID(_ context.Context, runID domain.RunID) ([]domain.FlowRunStep, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	steps := r.steps[runID]
	cloned := make([]domain.FlowRunStep, len(steps))
	copy(cloned, steps)
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].StepOrder < cloned[j].StepOrder })
	return cloned, nil
}

func (r *InMemoryRunEventRepository) Append(_ context.Context, event domain.RunEvent) (domain.RunEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	event.Message = strings.TrimSpace(event.Message)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	r.sequence[event.RunID]++
	event.Sequence = r.sequence[event.RunID]
	if event.ID == "" {
		event.ID = domain.RunEventID(fmt.Sprintf("%s-event-%06d", event.RunID, event.Sequence))
	}
	if err := event.Validate(); err != nil {
		return domain.RunEvent{}, err
	}
	r.events[event.RunID] = append(r.events[event.RunID], event)
	return event, nil
}

func (r *InMemoryRunEventRepository) ListByRunID(_ context.Context, runID domain.RunID, afterSequence int64) ([]domain.RunEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.events[runID]
	result := make([]domain.RunEvent, 0, len(items))
	for _, item := range items {
		if item.Sequence <= afterSequence {
			continue
		}
		result = append(result, item)
	}
	return result, nil
}

func containsFlowStatus(statuses []domain.FlowStatus, candidate domain.FlowStatus) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func containsRunStatus(statuses []domain.RunStatus, candidate domain.RunStatus) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func containsWorkspaceStatus(statuses []domain.WorkspaceStatus, candidate domain.WorkspaceStatus) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func sliceWindow[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	if offset < 0 {
		offset = 0
	}
	items = items[offset:]
	if limit <= 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}

var _ = json.Valid
var _ WorkspaceRepository = (*InMemoryWorkspaceRepository)(nil)
var _ SavedRequestRepository = (*InMemorySavedRequestRepository)(nil)
var _ FlowRepository = (*InMemoryFlowRepository)(nil)
var _ FlowStepRepository = (*InMemoryFlowStepRepository)(nil)
var _ RunRepository = (*InMemoryRunRepository)(nil)
var _ RunStepRepository = (*InMemoryRunStepRepository)(nil)
var _ RunEventRepository = (*InMemoryRunEventRepository)(nil)
