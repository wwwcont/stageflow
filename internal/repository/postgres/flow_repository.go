package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/repository"
)

type FlowRepository struct {
	db *sql.DB
}

type WorkspaceRepository struct {
	db        *sql.DB
	variables *WorkspaceVariableRepository
	secrets   *WorkspaceSecretRepository
}

type WorkspaceVariableRepository struct {
	db *sql.DB
}

type WorkspaceSecretRepository struct {
	db *sql.DB
}

type SavedRequestRepository struct {
	db *sql.DB
}

type FlowStepRepository struct {
	db *sql.DB
}

func NewWorkspaceRepository(db *sql.DB) *WorkspaceRepository {
	return &WorkspaceRepository{
		db:        db,
		variables: NewWorkspaceVariableRepository(db),
		secrets:   NewWorkspaceSecretRepository(db),
	}
}

func NewWorkspaceVariableRepository(db *sql.DB) *WorkspaceVariableRepository {
	return &WorkspaceVariableRepository{db: db}
}

func NewWorkspaceSecretRepository(db *sql.DB) *WorkspaceSecretRepository {
	return &WorkspaceSecretRepository{db: db}
}

func NewSavedRequestRepository(db *sql.DB) *SavedRequestRepository {
	return &SavedRequestRepository{db: db}
}

func NewFlowRepository(db *sql.DB) *FlowRepository {
	return &FlowRepository{db: db}
}

func NewFlowStepRepository(db *sql.DB) *FlowStepRepository {
	return &FlowStepRepository{db: db}
}

func (r *WorkspaceRepository) Create(ctx context.Context, workspace domain.Workspace) error {
	workspace = workspace.Normalized()
	if err := workspace.Validate(); err != nil {
		return err
	}
	policy, err := marshalJSON(workspace.Policy, "policy_json")
	if err != nil {
		return err
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const query = `
		INSERT INTO workspaces (id, name, slug, description, owner_team, status, policy_json, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`
	if _, err := tx.ExecContext(ctx, query, workspace.ID, workspace.Name, workspace.Slug, workspace.Description, workspace.OwnerTeam, workspace.Status, policy, workspace.CreatedAt, workspace.UpdatedAt); err != nil {
		return fmt.Errorf("insert workspace: %w", mapDBError(err, "workspace"))
	}
	if err := r.replaceWorkspaceChildren(ctx, tx, workspace); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workspace create tx: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Update(ctx context.Context, workspace domain.Workspace) error {
	workspace = workspace.Normalized()
	if err := workspace.Validate(); err != nil {
		return err
	}
	policy, err := marshalJSON(workspace.Policy, "policy_json")
	if err != nil {
		return err
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const query = `
		UPDATE workspaces
		SET name = $2, slug = $3, description = $4, owner_team = $5, status = $6, policy_json = $7, updated_at = $8
		WHERE id = $1`
	result, err := tx.ExecContext(ctx, query, workspace.ID, workspace.Name, workspace.Slug, workspace.Description, workspace.OwnerTeam, workspace.Status, policy, workspace.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update workspace %q: %w", workspace.ID, mapDBError(err, "workspace"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.NotFoundError{Entity: "workspace", ID: string(workspace.ID)}
	}
	if err := r.replaceWorkspaceChildren(ctx, tx, workspace); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit workspace update tx: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) GetByID(ctx context.Context, id domain.WorkspaceID) (domain.Workspace, error) {
	const query = `
		SELECT id, name, slug, description, owner_team, status, policy_json, created_at, updated_at
		FROM workspaces
		WHERE id = $1`
	workspace, err := r.scanWorkspace(ctx, query, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	return r.hydrateWorkspace(ctx, workspace)
}

func (r *WorkspaceRepository) GetBySlug(ctx context.Context, slug string) (domain.Workspace, error) {
	const query = `
		SELECT id, name, slug, description, owner_team, status, policy_json, created_at, updated_at
		FROM workspaces
		WHERE slug = $1`
	workspace, err := r.scanWorkspace(ctx, query, strings.ToLower(strings.TrimSpace(slug)))
	if err != nil {
		return domain.Workspace{}, err
	}
	return r.hydrateWorkspace(ctx, workspace)
}

func (r *WorkspaceRepository) List(ctx context.Context, filter repository.WorkspaceListFilter) ([]domain.Workspace, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, name, slug, description, owner_team, status, policy_json, created_at, updated_at
		FROM workspaces
		WHERE 1=1`)
	args := make([]any, 0, 4)
	if filter.NameLike != "" {
		args = append(args, "%"+strings.ToLower(filter.NameLike)+"%")
		query.WriteString(fmt.Sprintf(" AND LOWER(name) LIKE $%d", len(args)))
	}
	if len(filter.Statuses) > 0 {
		query.WriteString(" AND status IN (")
		for idx, status := range filter.Statuses {
			if idx > 0 {
				query.WriteString(", ")
			}
			args = append(args, string(status))
			query.WriteString(fmt.Sprintf("$%d", len(args)))
		}
		query.WriteString(")")
	}
	query.WriteString(" ORDER BY created_at DESC")
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query.WriteString(fmt.Sprintf(" LIMIT $%d", len(args)))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query.WriteString(fmt.Sprintf(" OFFSET $%d", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	defer rows.Close()
	workspaces := make([]domain.Workspace, 0)
	for rows.Next() {
		workspace, err := scanWorkspaceRow(rows)
		if err != nil {
			return nil, err
		}
		workspace, err = r.hydrateWorkspace(ctx, workspace)
		if err != nil {
			return nil, err
		}
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspaces: %w", err)
	}
	return workspaces, nil
}

func (r *WorkspaceRepository) replaceWorkspaceChildren(ctx context.Context, tx *sql.Tx, workspace domain.Workspace) error {
	if err := replaceWorkspaceVariables(ctx, tx, workspace.ID, workspace.Variables); err != nil {
		return err
	}
	if err := replaceWorkspaceSecrets(ctx, tx, workspace.ID, workspace.Secrets); err != nil {
		return err
	}
	return nil
}

func (r *WorkspaceRepository) hydrateWorkspace(ctx context.Context, workspace domain.Workspace) (domain.Workspace, error) {
	variables, err := r.variables.ListByWorkspaceID(ctx, workspace.ID)
	if err != nil {
		return domain.Workspace{}, err
	}
	secrets, err := r.secrets.ListByWorkspaceID(ctx, workspace.ID)
	if err != nil {
		return domain.Workspace{}, err
	}
	workspace.Variables = variables
	workspace.Secrets = secrets
	return workspace, nil
}

func (r *WorkspaceRepository) scanWorkspace(ctx context.Context, query string, arg any) (domain.Workspace, error) {
	row := r.db.QueryRowContext(ctx, query, arg)
	workspace, err := scanWorkspaceRow(row)
	if err != nil {
		return domain.Workspace{}, fmt.Errorf("select workspace: %w", mapDBError(err, "workspace"))
	}
	return workspace, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWorkspaceRow(row scanner) (domain.Workspace, error) {
	var (
		workspace domain.Workspace
		policyRaw []byte
	)
	if err := row.Scan(&workspace.ID, &workspace.Name, &workspace.Slug, &workspace.Description, &workspace.OwnerTeam, &workspace.Status, &policyRaw, &workspace.CreatedAt, &workspace.UpdatedAt); err != nil {
		return domain.Workspace{}, err
	}
	policy, err := unmarshalJSON[domain.WorkspacePolicy](policyRaw, "policy_json")
	if err != nil {
		return domain.Workspace{}, err
	}
	workspace.Policy = policy
	return workspace, nil
}

func (r *WorkspaceVariableRepository) ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, variables []domain.WorkspaceVariable) error {
	return replaceWorkspaceVariables(ctx, r.db, workspaceID, variables)
}

func replaceWorkspaceVariables(ctx context.Context, exec sqlExecutor, workspaceID domain.WorkspaceID, variables []domain.WorkspaceVariable) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM workspace_variables WHERE workspace_id = $1`, workspaceID); err != nil {
		return fmt.Errorf("delete workspace %q variables: %w", workspaceID, mapDBError(err, "workspace variable"))
	}
	const query = `
		INSERT INTO workspace_variables (workspace_id, name, value)
		VALUES ($1, $2, $3)`
	for _, variable := range (domain.Workspace{Variables: variables}).Normalized().Variables {
		if _, err := exec.ExecContext(ctx, query, workspaceID, variable.Name, variable.Value); err != nil {
			return fmt.Errorf("insert workspace %q variable %q: %w", workspaceID, variable.Name, mapDBError(err, "workspace variable"))
		}
	}
	return nil
}

func (r *WorkspaceVariableRepository) ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceVariable, error) {
	const query = `
		SELECT name, value
		FROM workspace_variables
		WHERE workspace_id = $1
		ORDER BY name ASC`
	rows, err := r.db.QueryContext(ctx, query, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace %q variables: %w", workspaceID, err)
	}
	defer rows.Close()
	variables := make([]domain.WorkspaceVariable, 0)
	for rows.Next() {
		var item domain.WorkspaceVariable
		if err := rows.Scan(&item.Name, &item.Value); err != nil {
			return nil, fmt.Errorf("scan workspace variable: %w", err)
		}
		variables = append(variables, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspace variables: %w", err)
	}
	return variables, nil
}

func (r *WorkspaceSecretRepository) ReplaceByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID, secrets []domain.WorkspaceSecret) error {
	return replaceWorkspaceSecrets(ctx, r.db, workspaceID, secrets)
}

func replaceWorkspaceSecrets(ctx context.Context, exec sqlExecutor, workspaceID domain.WorkspaceID, secrets []domain.WorkspaceSecret) error {
	if _, err := exec.ExecContext(ctx, `DELETE FROM workspace_secrets WHERE workspace_id = $1`, workspaceID); err != nil {
		return fmt.Errorf("delete workspace %q secrets: %w", workspaceID, mapDBError(err, "workspace secret"))
	}
	const query = `
		INSERT INTO workspace_secrets (workspace_id, name, value)
		VALUES ($1, $2, $3)`
	for _, secret := range (domain.Workspace{Secrets: secrets}).Normalized().Secrets {
		if _, err := exec.ExecContext(ctx, query, workspaceID, secret.Name, secret.Value); err != nil {
			return fmt.Errorf("insert workspace %q secret %q: %w", workspaceID, secret.Name, mapDBError(err, "workspace secret"))
		}
	}
	return nil
}

func (r *WorkspaceSecretRepository) ListByWorkspaceID(ctx context.Context, workspaceID domain.WorkspaceID) ([]domain.WorkspaceSecret, error) {
	const query = `
		SELECT name, value
		FROM workspace_secrets
		WHERE workspace_id = $1
		ORDER BY name ASC`
	rows, err := r.db.QueryContext(ctx, query, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace %q secrets: %w", workspaceID, err)
	}
	defer rows.Close()
	secrets := make([]domain.WorkspaceSecret, 0)
	for rows.Next() {
		var item domain.WorkspaceSecret
		if err := rows.Scan(&item.Name, &item.Value); err != nil {
			return nil, fmt.Errorf("scan workspace secret: %w", err)
		}
		secrets = append(secrets, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate workspace secrets: %w", err)
	}
	return secrets, nil
}

func (r *SavedRequestRepository) Create(ctx context.Context, request domain.SavedRequest) error {
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return err
	}
	requestSpec, err := marshalJSON(request.RequestSpec, "request_spec")
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO saved_requests (id, workspace_id, name, description, request_spec, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	if _, err := r.db.ExecContext(ctx, query, request.ID, request.WorkspaceID, request.Name, request.Description, requestSpec, request.CreatedAt, request.UpdatedAt); err != nil {
		return fmt.Errorf("insert saved request %q: %w", request.ID, mapDBError(err, "saved request"))
	}
	return nil
}

func (r *SavedRequestRepository) Update(ctx context.Context, request domain.SavedRequest) error {
	request = request.Normalized()
	if err := request.Validate(); err != nil {
		return err
	}
	requestSpec, err := marshalJSON(request.RequestSpec, "request_spec")
	if err != nil {
		return err
	}
	const query = `
		UPDATE saved_requests
		SET workspace_id = $2, name = $3, description = $4, request_spec = $5, updated_at = $6
		WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, request.ID, request.WorkspaceID, request.Name, request.Description, requestSpec, request.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update saved request %q: %w", request.ID, mapDBError(err, "saved request"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.NotFoundError{Entity: "saved request", ID: string(request.ID)}
	}
	return nil
}

func (r *SavedRequestRepository) GetByID(ctx context.Context, id domain.SavedRequestID) (domain.SavedRequest, error) {
	const query = `
		SELECT id, workspace_id, name, description, request_spec, created_at, updated_at
		FROM saved_requests
		WHERE id = $1`
	row := r.db.QueryRowContext(ctx, query, id)
	request, err := scanSavedRequestRow(row)
	if err != nil {
		return domain.SavedRequest{}, fmt.Errorf("select saved request %q: %w", id, mapDBError(err, "saved request"))
	}
	return request, nil
}

func (r *SavedRequestRepository) List(ctx context.Context, filter repository.SavedRequestListFilter) ([]domain.SavedRequest, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, workspace_id, name, description, request_spec, created_at, updated_at
		FROM saved_requests
		WHERE 1=1`)
	args := make([]any, 0, 4)
	if filter.WorkspaceID != "" {
		args = append(args, filter.WorkspaceID)
		query.WriteString(fmt.Sprintf(" AND workspace_id = $%d", len(args)))
	}
	if filter.NameLike != "" {
		args = append(args, "%"+strings.ToLower(filter.NameLike)+"%")
		query.WriteString(fmt.Sprintf(" AND LOWER(name) LIKE $%d", len(args)))
	}
	query.WriteString(" ORDER BY created_at DESC")
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query.WriteString(fmt.Sprintf(" LIMIT $%d", len(args)))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query.WriteString(fmt.Sprintf(" OFFSET $%d", len(args)))
	}
	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list saved requests: %w", err)
	}
	defer rows.Close()
	requests := make([]domain.SavedRequest, 0)
	for rows.Next() {
		request, err := scanSavedRequestRow(rows)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved requests: %w", err)
	}
	return requests, nil
}

func scanSavedRequestRow(row scanner) (domain.SavedRequest, error) {
	var (
		request    domain.SavedRequest
		requestRaw []byte
	)
	if err := row.Scan(&request.ID, &request.WorkspaceID, &request.Name, &request.Description, &requestRaw, &request.CreatedAt, &request.UpdatedAt); err != nil {
		return domain.SavedRequest{}, err
	}
	spec, err := unmarshalJSON[domain.RequestSpec](requestRaw, "request_spec")
	if err != nil {
		return domain.SavedRequest{}, err
	}
	request.RequestSpec = spec
	return request, nil
}

func (r *FlowRepository) Create(ctx context.Context, flow domain.Flow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const query = `
		INSERT INTO flows (id, workspace_id, name, description, version, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if _, err := tx.ExecContext(ctx, query, flow.ID, flow.WorkspaceID, flow.Name, flow.Description, flow.Version, flow.Status, flow.CreatedAt, flow.UpdatedAt); err != nil {
		return fmt.Errorf("insert flow: %w", mapDBError(err, "flow"))
	}
	if err := insertFlowVersion(ctx, tx, flow); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit flow create tx: %w", err)
	}
	return nil
}

func (r *FlowRepository) Update(ctx context.Context, flow domain.Flow) error {
	if err := flow.Validate(); err != nil {
		return err
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const query = `
		UPDATE flows
		SET workspace_id = $2, name = $3, description = $4, version = $5, status = $6, updated_at = $7
		WHERE id = $1`
	result, err := tx.ExecContext(ctx, query, flow.ID, flow.WorkspaceID, flow.Name, flow.Description, flow.Version, flow.Status, flow.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update flow %q: %w", flow.ID, mapDBError(err, "flow"))
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return &domain.NotFoundError{Entity: "flow", ID: string(flow.ID)}
	}
	if err := insertFlowVersion(ctx, tx, flow); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit flow update tx: %w", err)
	}
	return nil
}

func (r *FlowRepository) GetByID(ctx context.Context, id domain.FlowID) (domain.Flow, error) {
	const query = `
		SELECT id, workspace_id, name, description, version, status, created_at, updated_at
		FROM flows
		WHERE id = $1`
	var flow domain.Flow
	if err := r.db.QueryRowContext(ctx, query, id).Scan(&flow.ID, &flow.WorkspaceID, &flow.Name, &flow.Description, &flow.Version, &flow.Status, &flow.CreatedAt, &flow.UpdatedAt); err != nil {
		return domain.Flow{}, fmt.Errorf("select flow %q: %w", id, mapDBError(err, "flow"))
	}
	return flow, nil
}

func (r *FlowRepository) GetVersion(ctx context.Context, id domain.FlowID, version int) (domain.Flow, error) {
	const query = `
		SELECT flow_id, workspace_id, name, description, version, status, created_at, updated_at
		FROM flow_versions
		WHERE flow_id = $1 AND version = $2`
	var flow domain.Flow
	if err := r.db.QueryRowContext(ctx, query, id, version).Scan(&flow.ID, &flow.WorkspaceID, &flow.Name, &flow.Description, &flow.Version, &flow.Status, &flow.CreatedAt, &flow.UpdatedAt); err != nil {
		return domain.Flow{}, fmt.Errorf("select flow version %q@v%d: %w", id, version, mapDBError(err, "flow version"))
	}
	return flow, nil
}

func (r *FlowRepository) ListVersions(ctx context.Context, id domain.FlowID) ([]domain.Flow, error) {
	const query = `
		SELECT flow_id, workspace_id, name, description, version, status, created_at, updated_at
		FROM flow_versions
		WHERE flow_id = $1
		ORDER BY version DESC`
	rows, err := r.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("list flow versions for %q: %w", id, err)
	}
	defer rows.Close()

	flows := make([]domain.Flow, 0)
	for rows.Next() {
		var flow domain.Flow
		if err := rows.Scan(&flow.ID, &flow.WorkspaceID, &flow.Name, &flow.Description, &flow.Version, &flow.Status, &flow.CreatedAt, &flow.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan flow version: %w", err)
		}
		flows = append(flows, flow)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flow versions: %w", err)
	}
	return flows, nil
}

func (r *FlowRepository) List(ctx context.Context, filter repository.FlowListFilter) ([]domain.Flow, error) {
	query := strings.Builder{}
	query.WriteString(`
		SELECT id, workspace_id, name, description, version, status, created_at, updated_at
		FROM flows
		WHERE 1=1`)
	args := make([]any, 0, 4)
	if filter.WorkspaceID != "" {
		args = append(args, filter.WorkspaceID)
		query.WriteString(fmt.Sprintf(" AND workspace_id = $%d", len(args)))
	}
	if filter.NameLike != "" {
		args = append(args, "%"+strings.ToLower(filter.NameLike)+"%")
		query.WriteString(fmt.Sprintf(" AND LOWER(name) LIKE $%d", len(args)))
	}
	if len(filter.Statuses) > 0 {
		query.WriteString(" AND status IN (")
		for idx, status := range filter.Statuses {
			if idx > 0 {
				query.WriteString(", ")
			}
			args = append(args, string(status))
			query.WriteString(fmt.Sprintf("$%d", len(args)))
		}
		query.WriteString(")")
	}
	query.WriteString(" ORDER BY created_at DESC")
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query.WriteString(fmt.Sprintf(" LIMIT $%d", len(args)))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query.WriteString(fmt.Sprintf(" OFFSET $%d", len(args)))
	}

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list flows: %w", err)
	}
	defer rows.Close()

	flows := make([]domain.Flow, 0)
	for rows.Next() {
		var flow domain.Flow
		if err := rows.Scan(&flow.ID, &flow.WorkspaceID, &flow.Name, &flow.Description, &flow.Version, &flow.Status, &flow.CreatedAt, &flow.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan flow: %w", err)
		}
		flows = append(flows, flow)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flows: %w", err)
	}
	return flows, nil
}

func (r *FlowStepRepository) CreateMany(ctx context.Context, steps []domain.FlowStep) error {
	if len(steps) == 0 {
		return nil
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const query = `
		INSERT INTO flow_steps (id, flow_id, order_index, name, step_type, saved_request_id, request_spec, request_spec_override, extraction_spec, assertion_spec, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	for _, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
		requestSpec, err := marshalJSON(step.RequestSpec, "request_spec")
		if err != nil {
			return err
		}
		requestSpecOverride, err := marshalJSON(step.RequestSpecOverride, "request_spec_override")
		if err != nil {
			return err
		}
		extractionSpec, err := marshalJSON(step.ExtractionSpec, "extraction_spec")
		if err != nil {
			return err
		}
		assertionSpec, err := marshalJSON(step.AssertionSpec, "assertion_spec")
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, query, step.ID, step.FlowID, step.OrderIndex, step.Name, step.Type(), nullableString(step.SavedRequestID), requestSpec, requestSpecOverride, extractionSpec, assertionSpec, step.CreatedAt, step.UpdatedAt); err != nil {
			return fmt.Errorf("insert flow step %q: %w", step.ID, mapDBError(err, "flow step"))
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit flow steps tx: %w", err)
	}
	return nil
}

func (r *FlowStepRepository) ReplaceByFlowID(ctx context.Context, flowID domain.FlowID, steps []domain.FlowStep) error {
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM flow_steps WHERE flow_id = $1`, flowID); err != nil {
		return fmt.Errorf("delete flow %q steps: %w", flowID, err)
	}
	if len(steps) > 0 {
		const query = `
			INSERT INTO flow_steps (id, flow_id, order_index, name, step_type, saved_request_id, request_spec, request_spec_override, extraction_spec, assertion_spec, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
		for _, step := range steps {
			if err := step.Validate(); err != nil {
				return err
			}
			requestSpec, err := marshalJSON(step.RequestSpec, "request_spec")
			if err != nil {
				return err
			}
			requestSpecOverride, err := marshalJSON(step.RequestSpecOverride, "request_spec_override")
			if err != nil {
				return err
			}
			extractionSpec, err := marshalJSON(step.ExtractionSpec, "extraction_spec")
			if err != nil {
				return err
			}
			assertionSpec, err := marshalJSON(step.AssertionSpec, "assertion_spec")
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, query, step.ID, step.FlowID, step.OrderIndex, step.Name, step.Type(), nullableString(step.SavedRequestID), requestSpec, requestSpecOverride, extractionSpec, assertionSpec, step.CreatedAt, step.UpdatedAt); err != nil {
				return fmt.Errorf("replace flow step %q: %w", step.ID, mapDBError(err, "flow step"))
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace flow steps tx: %w", err)
	}
	return nil
}

func (r *FlowStepRepository) ReplaceByFlowVersion(ctx context.Context, flowID domain.FlowID, version int, steps []domain.FlowStep) error {
	if version < 1 {
		return &domain.ValidationError{Message: "flow version must be >= 1"}
	}
	tx, err := beginTx(ctx, r.db)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM flow_step_versions WHERE flow_id = $1 AND version = $2`, flowID, version); err != nil {
		return fmt.Errorf("delete flow version %q@v%d steps: %w", flowID, version, err)
	}
	if len(steps) > 0 {
		const query = `
			INSERT INTO flow_step_versions (id, flow_id, version, order_index, name, step_type, saved_request_id, request_spec, request_spec_override, extraction_spec, assertion_spec, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`
		for _, step := range steps {
			if err := step.Validate(); err != nil {
				return err
			}
			requestSpec, err := marshalJSON(step.RequestSpec, "request_spec")
			if err != nil {
				return err
			}
			requestSpecOverride, err := marshalJSON(step.RequestSpecOverride, "request_spec_override")
			if err != nil {
				return err
			}
			extractionSpec, err := marshalJSON(step.ExtractionSpec, "extraction_spec")
			if err != nil {
				return err
			}
			assertionSpec, err := marshalJSON(step.AssertionSpec, "assertion_spec")
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, query, step.ID, step.FlowID, version, step.OrderIndex, step.Name, step.Type(), nullableString(step.SavedRequestID), requestSpec, requestSpecOverride, extractionSpec, assertionSpec, step.CreatedAt, step.UpdatedAt); err != nil {
				return fmt.Errorf("replace flow version step %q: %w", step.ID, mapDBError(err, "flow step version"))
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace flow version steps tx: %w", err)
	}
	return nil
}

func (r *FlowStepRepository) ListByFlowID(ctx context.Context, flowID domain.FlowID) ([]domain.FlowStep, error) {
	const query = `
		SELECT id, flow_id, order_index, name, step_type, saved_request_id, request_spec, request_spec_override, extraction_spec, assertion_spec, created_at, updated_at
		FROM flow_steps
		WHERE flow_id = $1
		ORDER BY order_index ASC`
	rows, err := r.db.QueryContext(ctx, query, flowID)
	if err != nil {
		return nil, fmt.Errorf("select flow steps for %q: %w", flowID, err)
	}
	defer rows.Close()

	steps := make([]domain.FlowStep, 0)
	for rows.Next() {
		var (
			step               domain.FlowStep
			savedRequestID     sql.NullString
			requestRaw         []byte
			requestOverrideRaw []byte
			extractionRaw      []byte
			assertionRaw       []byte
		)
		if err := rows.Scan(&step.ID, &step.FlowID, &step.OrderIndex, &step.Name, &step.StepType, &savedRequestID, &requestRaw, &requestOverrideRaw, &extractionRaw, &assertionRaw, &step.CreatedAt, &step.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan flow step: %w", err)
		}
		step.SavedRequestID = domain.SavedRequestID(savedRequestID.String)
		if step.RequestSpec, err = unmarshalJSON[domain.RequestSpec](requestRaw, "request_spec"); err != nil {
			return nil, err
		}
		if step.RequestSpecOverride, err = unmarshalJSON[domain.RequestSpecOverride](requestOverrideRaw, "request_spec_override"); err != nil {
			return nil, err
		}
		if step.ExtractionSpec, err = unmarshalJSON[domain.ExtractionSpec](extractionRaw, "extraction_spec"); err != nil {
			return nil, err
		}
		if step.AssertionSpec, err = unmarshalJSON[domain.AssertionSpec](assertionRaw, "assertion_spec"); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flow steps: %w", err)
	}
	return steps, nil
}

func (r *FlowStepRepository) ListByFlowVersion(ctx context.Context, flowID domain.FlowID, version int) ([]domain.FlowStep, error) {
	const query = `
		SELECT id, flow_id, order_index, name, step_type, saved_request_id, request_spec, request_spec_override, extraction_spec, assertion_spec, created_at, updated_at
		FROM flow_step_versions
		WHERE flow_id = $1 AND version = $2
		ORDER BY order_index ASC`
	rows, err := r.db.QueryContext(ctx, query, flowID, version)
	if err != nil {
		return nil, fmt.Errorf("select flow version steps for %q@v%d: %w", flowID, version, err)
	}
	defer rows.Close()
	return scanFlowSteps(rows)
}

func scanFlowSteps(rows *sql.Rows) ([]domain.FlowStep, error) {
	steps := make([]domain.FlowStep, 0)
	for rows.Next() {
		var (
			step               domain.FlowStep
			savedRequestID     sql.NullString
			requestRaw         []byte
			requestOverrideRaw []byte
			extractionRaw      []byte
			assertionRaw       []byte
		)
		if err := rows.Scan(&step.ID, &step.FlowID, &step.OrderIndex, &step.Name, &step.StepType, &savedRequestID, &requestRaw, &requestOverrideRaw, &extractionRaw, &assertionRaw, &step.CreatedAt, &step.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan flow step: %w", err)
		}
		step.SavedRequestID = domain.SavedRequestID(savedRequestID.String)
		var err error
		if step.RequestSpec, err = unmarshalJSON[domain.RequestSpec](requestRaw, "request_spec"); err != nil {
			return nil, err
		}
		if step.RequestSpecOverride, err = unmarshalJSON[domain.RequestSpecOverride](requestOverrideRaw, "request_spec_override"); err != nil {
			return nil, err
		}
		if step.ExtractionSpec, err = unmarshalJSON[domain.ExtractionSpec](extractionRaw, "extraction_spec"); err != nil {
			return nil, err
		}
		if step.AssertionSpec, err = unmarshalJSON[domain.AssertionSpec](assertionRaw, "assertion_spec"); err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate flow steps: %w", err)
	}
	return steps, nil
}

var _ repository.FlowRepository = (*FlowRepository)(nil)
var _ repository.FlowStepRepository = (*FlowStepRepository)(nil)
var _ repository.WorkspaceRepository = (*WorkspaceRepository)(nil)
var _ repository.WorkspaceVariableRepository = (*WorkspaceVariableRepository)(nil)
var _ repository.WorkspaceSecretRepository = (*WorkspaceSecretRepository)(nil)
var _ repository.SavedRequestRepository = (*SavedRequestRepository)(nil)

func nullableString[T ~string](value T) any {
	if value == "" {
		return nil
	}
	return string(value)
}

type sqlExecutor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertFlowVersion(ctx context.Context, exec sqlExecutor, flow domain.Flow) error {
	const query = `
		INSERT INTO flow_versions (flow_id, workspace_id, version, name, description, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`
	if _, err := exec.ExecContext(ctx, query, flow.ID, flow.WorkspaceID, flow.Version, flow.Name, flow.Description, flow.Status, flow.CreatedAt, flow.UpdatedAt); err != nil {
		return fmt.Errorf("insert flow version %q@v%d: %w", flow.ID, flow.Version, mapDBError(err, "flow version"))
	}
	return nil
}
