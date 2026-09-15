-- R4-05: party membership binding for GOTV tenancy enforcement.
-- gotvAuthMiddleware validates the client X-Party-ID header against the
-- user's OWN party (users.party_id) and rejects mismatches with 403.
-- The column previously did not exist anywhere in the schema, so the
-- middleware's membership query always errored and the (trusted) header was
-- the only way in — cross-party impersonation for any party_admin.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS party_id INTEGER REFERENCES parties(id);

CREATE INDEX IF NOT EXISTS idx_users_party ON users (party_id);
