package usecase

import (
	"context"
	"fmt"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
	"stageflow/pkg/clock"
)

type WorkspaceManagementService struct {
	workspaces repository.WorkspaceRepository
	clock      clock.Clock
}

func NewWorkspaceManagementService(workspaces repository.WorkspaceRepository, clk clock.Clock) (*WorkspaceManagementService, error) {
	switch {
	case workspaces == nil:
		return nil, fmt.Errorf("workspace repository is required")
	case clk == nil:
		return nil, fmt.Errorf("clock is required")
	}
	return &WorkspaceManagementService{workspaces: workspaces, clock: clk}, nil
}

func (s *WorkspaceManagementService) CreateWorkspace(ctx context.Context, cmd CreateWorkspaceCommand) (WorkspaceView, error) {
	now := s.clock.Now().UTC()
	workspace := domain.Workspace{
		ID:          cmd.WorkspaceID,
		Name:        strings.TrimSpace(cmd.Name),
		Slug:        strings.ToLower(strings.TrimSpace(cmd.Slug)),
		Description: strings.TrimSpace(cmd.Description),
		OwnerTeam:   strings.TrimSpace(cmd.OwnerTeam),
		Status:      cmd.Status,
		Policy:      cmd.Policy,
		CreatedAt:   now,
		UpdatedAt:   now,
	}.Normalized()
	if err := s.workspaces.Create(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("create workspace %q: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) UpdateWorkspace(ctx context.Context, cmd UpdateWorkspaceCommand) (WorkspaceView, error) {
	workspace, err := s.requireMutableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, err
	}
	workspace.Name = strings.TrimSpace(cmd.Name)
	workspace.Slug = strings.ToLower(strings.TrimSpace(cmd.Slug))
	workspace.Description = strings.TrimSpace(cmd.Description)
	workspace.OwnerTeam = strings.TrimSpace(cmd.OwnerTeam)
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("update workspace %q: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) ArchiveWorkspace(ctx context.Context, cmd ArchiveWorkspaceCommand) (WorkspaceView, error) {
	workspace, err := s.workspaces.GetByID(ctx, cmd.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, fmt.Errorf("get workspace %q: %w", cmd.WorkspaceID, err)
	}
	if workspace.Status == domain.WorkspaceStatusArchived {
		return WorkspaceView{Workspace: workspace}, nil
	}
	workspace.Status = domain.WorkspaceStatusArchived
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("archive workspace %q: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) UnarchiveWorkspace(ctx context.Context, cmd UnarchiveWorkspaceCommand) (WorkspaceView, error) {
	workspace, err := s.workspaces.GetByID(ctx, cmd.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, fmt.Errorf("get workspace %q: %w", cmd.WorkspaceID, err)
	}
	if workspace.Status == domain.WorkspaceStatusActive {
		return WorkspaceView{Workspace: workspace}, nil
	}
	workspace.Status = domain.WorkspaceStatusActive
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("unarchive workspace %q: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) UpdateWorkspacePolicy(ctx context.Context, cmd UpdateWorkspacePolicyCommand) (WorkspaceView, error) {
	workspace, err := s.requireMutableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, err
	}
	workspace.Policy = cmd.Policy
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("update workspace %q policy: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) UpdateWorkspaceVariables(ctx context.Context, cmd UpdateWorkspaceVariablesCommand) (WorkspaceView, error) {
	workspace, err := s.requireMutableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, err
	}
	workspace.Variables = cmd.Variables
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return WorkspaceView{}, fmt.Errorf("update workspace %q variables: %w", workspace.ID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) PutWorkspaceSecret(ctx context.Context, cmd PutWorkspaceSecretCommand) error {
	workspace, err := s.requireMutableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return err
	}
	secretName := strings.ToLower(strings.TrimSpace(cmd.SecretName))
	nextSecrets := make([]domain.WorkspaceSecret, 0, len(workspace.Secrets)+1)
	for _, secret := range workspace.Secrets {
		if strings.EqualFold(secret.Name, secretName) {
			continue
		}
		nextSecrets = append(nextSecrets, secret)
	}
	nextSecrets = append(nextSecrets, domain.WorkspaceSecret{Name: secretName, Value: cmd.SecretValue})
	workspace.Secrets = nextSecrets
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return fmt.Errorf("update workspace %q secrets: %w", workspace.ID, err)
	}
	return nil
}

func (s *WorkspaceManagementService) ListWorkspaceSecrets(ctx context.Context, query ListWorkspaceSecretsQuery) ([]domain.WorkspaceSecret, error) {
	workspace, err := s.workspaces.GetByID(ctx, query.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("get workspace %q: %w", query.WorkspaceID, err)
	}
	return workspace.Normalized().Secrets, nil
}

func (s *WorkspaceManagementService) DeleteWorkspaceSecret(ctx context.Context, cmd DeleteWorkspaceSecretCommand) error {
	workspace, err := s.requireMutableWorkspace(ctx, cmd.WorkspaceID)
	if err != nil {
		return err
	}
	secretName := strings.ToLower(strings.TrimSpace(cmd.SecretName))
	nextSecrets := make([]domain.WorkspaceSecret, 0, len(workspace.Secrets))
	removed := false
	for _, secret := range workspace.Secrets {
		if strings.EqualFold(secret.Name, secretName) {
			removed = true
			continue
		}
		nextSecrets = append(nextSecrets, secret)
	}
	if !removed {
		return &domain.NotFoundError{Entity: "workspace secret", ID: secretName}
	}
	workspace.Secrets = nextSecrets
	workspace.UpdatedAt = s.clock.Now().UTC()
	workspace = workspace.Normalized()
	if err := s.workspaces.Update(ctx, workspace); err != nil {
		return fmt.Errorf("delete workspace %q secret %q: %w", workspace.ID, secretName, err)
	}
	return nil
}

func (s *WorkspaceManagementService) GetWorkspace(ctx context.Context, query GetWorkspaceQuery) (WorkspaceView, error) {
	workspace, err := s.workspaces.GetByID(ctx, query.WorkspaceID)
	if err != nil {
		return WorkspaceView{}, fmt.Errorf("get workspace %q: %w", query.WorkspaceID, err)
	}
	return WorkspaceView{Workspace: workspace}, nil
}

func (s *WorkspaceManagementService) ListWorkspaces(ctx context.Context, query ListWorkspacesQuery) ([]domain.Workspace, error) {
	workspaces, err := s.workspaces.List(ctx, repository.WorkspaceListFilter{
		Statuses: query.Statuses,
		NameLike: query.NameLike,
		Limit:    query.Limit,
		Offset:   query.Offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}

func (s *WorkspaceManagementService) requireMutableWorkspace(ctx context.Context, workspaceID domain.WorkspaceID) (domain.Workspace, error) {
	workspace, err := s.workspaces.GetByID(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("get workspace %q: %w", workspaceID, err)
	}
	if !workspace.AllowsWrites() {
		return domain.Workspace{}, &domain.ConflictError{Entity: "workspace", Field: "status", Value: string(workspace.Status)}
	}
	return workspace, nil
}

var _ WorkspaceManagementUseCase = (*WorkspaceManagementService)(nil)
