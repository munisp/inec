-- SEC-14 (2026-09 deep audit): extend users.role CHECK to the full set of
-- roles the authorization guards actually reference. Previously guards in
-- election_lifecycle.go / device_gateway.go / compliance.go /
-- dispute_resolution.go referenced returning_officer, ict_officer, security,
-- dpo and officer, but the CHECK rejected those values at INSERT/UPDATE —
-- making the corresponding endpoints unreachable by any user (fail-closed
-- dead features). Self-registration remains restricted to public/observer
-- (security.go allowedSelfRegRoles is unchanged); the new roles are
-- assignable only by admins via handlePromoteUser.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN (
        'admin',
        'presiding_officer',
        'collation_officer',
        'returning_officer',
        'ict_officer',
        'security',
        'dpo',
        'officer',
        'observer',
        'public'
    ));
