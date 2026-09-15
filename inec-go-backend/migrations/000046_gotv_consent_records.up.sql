-- R5-096: GOTV supporter consent must be a provable record, not a free-text
-- string. Every gotv_contacts.consent_id must reference a consent record with
-- channel/purpose/legal_basis/timestamp/recorder, and opt-out withdraws it.
CREATE TABLE IF NOT EXISTS gotv_consent_records (
	consent_id TEXT PRIMARY KEY,
	party_id INTEGER NOT NULL,
	contact_id TEXT,
	channel TEXT NOT NULL,
	purpose TEXT NOT NULL DEFAULT 'campaign_outreach',
	legal_basis TEXT NOT NULL,
	proof_ref TEXT,
	status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','withdrawn')),
	granted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	withdrawn_at TIMESTAMP,
	recorded_by TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_gotv_consent_party ON gotv_consent_records(party_id);
CREATE INDEX IF NOT EXISTS idx_gotv_consent_contact ON gotv_consent_records(contact_id);

-- Backfill records for legacy free-text consent ids so the FK can be
-- enforced; legacy values are honestly labelled as caller-asserted.
INSERT INTO gotv_consent_records (consent_id, party_id, contact_id, channel, purpose, legal_basis, recorded_by)
SELECT DISTINCT c.consent_id, c.party_id, c.contact_id, 'legacy', 'campaign_outreach', 'legacy_asserted', 'migration_000046'
FROM gotv_contacts c
WHERE c.consent_id IS NOT NULL
ON CONFLICT (consent_id) DO NOTHING;

ALTER TABLE gotv_contacts
	ADD CONSTRAINT fk_gotv_contacts_consent
	FOREIGN KEY (consent_id) REFERENCES gotv_consent_records(consent_id);
