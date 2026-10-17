-- The installation owner is assigned explicitly by the local bootstrap command.
-- Registration, a global role or selecting a workspace never creates ownership.
CREATE TABLE product_installation (
    id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id = TRUE),
    owner_user_id BIGINT NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Product tenancy is independent of inherited commercial organization access.
CREATE TABLE workspaces (
    id BIGSERIAL PRIMARY KEY,
    uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_workspaces_updated_at BEFORE UPDATE ON workspaces
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE workspace_members (
    id BIGSERIAL PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('owner', 'member')),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workspace_id, user_id)
);
CREATE INDEX idx_workspace_members_user ON workspace_members(user_id, workspace_id);
CREATE TRIGGER trg_workspace_members_updated_at BEFORE UPDATE ON workspace_members
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TABLE workspace_tool_credentials (
    id BIGSERIAL PRIMARY KEY,
    uuid UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    workspace_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    token_hash BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    audience TEXT NOT NULL CHECK (audience IN ('api', 'mcp')),
    scopes TEXT[] NOT NULL CHECK (cardinality(scopes) BETWEEN 1 AND 32),
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    FOREIGN KEY (workspace_id, user_id) REFERENCES workspace_members(workspace_id, user_id) ON DELETE CASCADE,
    CHECK (expires_at > created_at),
    UNIQUE (workspace_id, uuid)
);
CREATE INDEX idx_workspace_tool_credentials_workspace ON workspace_tool_credentials(workspace_id, uuid);
