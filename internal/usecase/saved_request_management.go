package usecase

import (
	"context"
	"fmt"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type SavedRequestManagementService struct {
	workspaces    repository.WorkspaceRepository
	savedRequests repository.SavedRequestRepository
	clock         clock.Clock
}

func NewSavedRequestManagementService(workspaces repository.WorkspaceRepository, savedRequests repository.SavedRequestRepository, clk clock.Clock) (*SavedRequestManagementService, error) {
	switch {
	case workspaces == nil:
		return nil, fmt.Errorf("workspace repository is required")
	case savedRequests == nil:
		return nil, fmt.Errorf("saved request repository is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &SavedRequestManagementService{workspaces: workspaces, savedRequests: savedRequests, clock: clk}, nil
}

func (s *SavedRequestManagementService) CreateSavedRequest(ctx context.Context, cmd CreateSavedRequestCommand) (SavedRequestView, error) {
	workspace, err := s.requireWritableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return SavedRequestView{}, err
	}
	request := domain.SavedRequest{
		ID:          cmd.SavedRequestID,
		WorkspaceID: workspace.ID,
		Name:        strings.TrimSpace(cmd.Name),
		Description: strings.TrimSpace(cmd.Description),
		RequestSpec: cmd.RequestSpec,
		CreatedAt:   s.clock.Now().UTC(),
		UpdatedAt:   s.clock.Now().UTC(),
	}.Normalized()
	if err := s.savedRequests.Create(ctx, request); err != nil {
		return SavedRequestView{}, fmt.Errorf("create saved request %q: %w", request.ID, err)
	}
	return SavedRequestView{SavedRequest: request}, nil
}

func (s *SavedRequestManagementService) UpdateSavedRequest(ctx context.Context, cmd UpdateSavedRequestCommand) (SavedRequestView, error) {
	request, err := s.savedRequests.GetByID(ctx, cmd.SavedRequestID)
	if err != nil {
		return SavedRequestView{}, fmt.Errorf("get saved request %q: %w", cmd.SavedRequestID, err)
	}
	if cmd.WorkspaceID != "" && request.WorkspaceID != cmd.WorkspaceID {
		return SavedRequestView{}, &domain.NotFoundError{Entity: "saved request", ID: string(cmd.SavedRequestID)}
	}
	if _, err := s.requireWritableWorkspace(ctx, request.WorkspaceID); err != nil {
		return SavedRequestView{}, err
	}
	request = domain.SavedRequest{
		ID:          request.ID,
		WorkspaceID: request.WorkspaceID,
		Name:        strings.TrimSpace(cmd.Name),
		Description: strings.TrimSpace(cmd.Description),
		RequestSpec: cmd.RequestSpec,
		CreatedAt:   request.CreatedAt,
		UpdatedAt:   s.clock.Now().UTC(),
	}.Normalized()
	if err := s.savedRequests.Update(ctx, request); err != nil {
		return SavedRequestView{}, fmt.Errorf("update saved request %q: %w", request.ID, err)
	}
	return SavedRequestView{SavedRequest: request}, nil
}

func (s *SavedRequestManagementService) GetSavedRequest(ctx context.Context, query GetSavedRequestQuery) (SavedRequestView, error) {
	request, err := s.savedRequests.GetByID(ctx, query.SavedRequestID)
	if err != nil {
		return SavedRequestView{}, fmt.Errorf("get saved request %q: %w", query.SavedRequestID, err)
	}
	if query.WorkspaceID != "" && request.WorkspaceID != query.WorkspaceID {
		return SavedRequestView{}, &domain.NotFoundError{Entity: "saved request", ID: string(query.SavedRequestID)}
	}
	return SavedRequestView{SavedRequest: request}, nil
}

func (s *SavedRequestManagementService) ListSavedRequests(ctx context.Context, query ListSavedRequestsQuery) ([]domain.SavedRequest, error) {
	requests, err := s.savedRequests.List(ctx, repository.SavedRequestListFilter{
		WorkspaceID: query.WorkspaceID,
		NameLike:    query.NameLike,
		Limit:       query.Limit,
		Offset:      query.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list saved requests: %w", err)
	}
	return requests, nil
}

func (s *SavedRequestManagementService) requireWritableWorkspace(ctx context.Context, workspaceID domain.WorkspaceID) (domain.Workspace, error) {
	workspace, err := s.workspaces.GetByID(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace %q: %w", workspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.Workspace{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	return workspace, nil
}

var _ SavedRequestManagementUseCase = (*SavedRequestManagementService)(nil)
