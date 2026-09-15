-- R5-036: server-side GOTV RBAC membership.
-- Previously the GOTV role was read from the client-supplied X-GOTV-Role
-- header, so any holder of a valid party credential could self-assert
-- party_admin. Roles are now resolved server-side:
--   * JWT / gateway identities -> gotv_party_members (party_id, user_email)
--   * party API keys           -> gotv_party_access.role
-- Fail closed: no membership row (or an unrecognized role) yields NO role,
-- and requirePermission denies.
CREATE TABLE IF NOT EXISTS gotv_party_members (
    id SERIAL PRIMARY KEY,
    party_id INTEGER NOT NULL REFERENCES parties(id),
    user_email TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('party_admin','coordinator','team_lead','field_worker','observer','analyst')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (party_id, user_email)
);
CREATE INDEX IF NOT EXISTS idx_gotv_party_members_party ON gotv_party_members(party_id);

-- API-key credentials carry a server-assigned role. Default is the least
-- privileged operational role; operators elevate explicitly via SQL/migration.
ALTER TABLE gotv_party_access
    ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'field_worker'
    CHECK (role IN ('party_admin','coordinator','team_lead','field_worker','observer','analyst'));
