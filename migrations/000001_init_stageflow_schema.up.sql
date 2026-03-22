CREATE TABLE workspaces (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    owner_team TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'archived')),
    policy_json JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_workspaces_name_lower ON workspaces (LOWER(name));
CREATE INDEX idx_workspaces_status ON workspaces (status);

CREATE TABLE workspace_variables (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (workspace_id, name)
);

CREATE INDEX idx_workspace_variables_workspace_id ON workspace_variables (workspace_id);

CREATE TABLE workspace_secrets (
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    PRIMARY KEY (workspace_id, name)
);

CREATE INDEX idx_workspace_secrets_workspace_id ON workspace_secrets (workspace_id);

CREATE TABLE saved_requests (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    request_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_saved_requests_workspace_id ON saved_requests (workspace_id);
CREATE INDEX idx_saved_requests_name_lower ON saved_requests (LOWER(name));

CREATE TABLE flows (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL CHECK (version >= 1),
    status TEXT NOT NULL CHECK (status IN ('draft', 'active', 'disabled', 'archived')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_flows_name_lower ON flows (LOWER(name));
CREATE INDEX idx_flows_status ON flows (status);
CREATE INDEX idx_flows_workspace_id ON flows (workspace_id);

CREATE TABLE flow_steps (
    id TEXT PRIMARY KEY,
    flow_id TEXT NOT NULL REFERENCES flows(id) ON DELETE CASCADE,
    order_index INTEGER NOT NULL CHECK (order_index >= 0),
    name TEXT NOT NULL,
    step_type TEXT NOT NULL DEFAULT 'inline_request' CHECK (step_type IN ('inline_request', 'saved_request_ref')),
    saved_request_id TEXT NULL REFERENCES saved_requests(id) ON DELETE RESTRICT,
    request_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_spec_override JSONB NOT NULL DEFAULT '{}'::jsonb,
    extraction_spec JSONB NOT NULL,
    assertion_spec JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (flow_id, order_index)
);

CREATE INDEX idx_flow_steps_flow_id_order ON flow_steps (flow_id, order_index);
CREATE INDEX idx_flow_steps_saved_request_id ON flow_steps (saved_request_id);

CREATE TABLE flow_runs (
    id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE RESTRICT,
    target_type TEXT NOT NULL CHECK (target_type IN ('flow', 'saved_request')),
    flow_id TEXT NULL REFERENCES flows(id) ON DELETE RESTRICT,
    flow_version INTEGER NULL CHECK (flow_version IS NULL OR flow_version >= 1),
    saved_request_id TEXT NULL REFERENCES saved_requests(id) ON DELETE RESTRICT,
    status TEXT NOT NULL CHECK (status IN ('pending', 'queued', 'running', 'succeeded', 'failed', 'canceled')),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    input_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    initiated_by TEXT NOT NULL,
    queue_name TEXT NOT NULL DEFAULT '',
    idempotency_key TEXT NOT NULL DEFAULT '',
    claimed_by TEXT NOT NULL DEFAULT '',
    heartbeat_at TIMESTAMPTZ NULL,
    error_message TEXT NOT NULL DEFAULT '',
    CHECK (
        (target_type = 'flow' AND flow_id IS NOT NULL AND flow_version IS NOT NULL AND saved_request_id IS NULL)
        OR
        (target_type = 'saved_request' AND flow_id IS NULL AND flow_version IS NULL AND saved_request_id IS NOT NULL)
    )
);

CREATE INDEX idx_flow_runs_flow_status_created_at ON flow_runs (flow_id, status, created_at DESC);
CREATE INDEX idx_flow_runs_created_at ON flow_runs (created_at DESC);
CREATE INDEX idx_flow_runs_flow_idempotency_key ON flow_runs (flow_id, idempotency_key);
CREATE INDEX idx_flow_runs_workspace_id ON flow_runs (workspace_id);
CREATE INDEX idx_flow_runs_saved_request_id ON flow_runs (saved_request_id);
CREATE INDEX idx_flow_runs_target_type ON flow_runs (target_type);

CREATE TABLE flow_run_steps (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES flow_runs(id) ON DELETE CASCADE,
    step_name TEXT NOT NULL,
    step_order INTEGER NOT NULL CHECK (step_order >= 0),
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped', 'canceled')),
    request_snapshot_json JSONB NULL,
    response_snapshot_json JSONB NULL,
    extracted_values_json JSONB NULL,
    attempt_history_json JSONB NULL,
    retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    error_message TEXT NOT NULL DEFAULT '',
    UNIQUE (run_id, step_order)
);

CREATE INDEX idx_flow_run_steps_run_id_order ON flow_run_steps (run_id, step_order);
