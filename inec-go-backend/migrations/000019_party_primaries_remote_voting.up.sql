-- ═══════════════════════════════════════════════════════════════════════════
-- Migration 000019: Party Primaries & Remote Voting Infrastructure
-- ═══════════════════════════════════════════════════════════════════════════

-- 1. Extend election types to include primaries
ALTER TABLE elections DROP CONSTRAINT IF EXISTS elections_election_type_check;
ALTER TABLE elections ADD CONSTRAINT elections_election_type_check CHECK (
    election_type = ANY (ARRAY[
        'presidential','gubernatorial','senatorial','house_of_reps',
        'state_assembly','local_government',
        'party_primary','convention','ward_congress','state_congress',
        'national_convention','special_election','by_election','referendum'
    ])
);

-- 2. Aspirant Registry
CREATE TABLE IF NOT EXISTS aspirants (
    id SERIAL PRIMARY KEY,
    aspirant_id TEXT NOT NULL UNIQUE DEFAULT 'asp-' || gen_random_uuid()::text,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    party_code TEXT NOT NULL,
    full_name TEXT NOT NULL,
    position_sought TEXT NOT NULL, -- e.g. 'presidential','gubernatorial','senatorial'
    nin_number TEXT,
    date_of_birth TEXT,
    gender TEXT CHECK (gender IN ('M','F')),
    state_of_origin TEXT,
    lga_of_origin TEXT,
    photo_url TEXT,
    manifesto_url TEXT,
    screening_status TEXT NOT NULL DEFAULT 'pending'
        CHECK (screening_status IN ('pending','documents_submitted','screened','cleared','disqualified','withdrawn','appealing')),
    screening_notes TEXT,
    screening_officer_id INTEGER,
    screening_date TIMESTAMP,
    declaration_form_hash TEXT, -- SHA-256 of nomination form
    affidavit_hash TEXT,
    deposit_paid BOOLEAN DEFAULT FALSE,
    deposit_amount_kobo BIGINT DEFAULT 0,
    deposit_tb_transfer_id TEXT, -- TigerBeetle transfer for deposit
    endorsement_count INTEGER DEFAULT 0,
    delegate_votes INTEGER DEFAULT 0,
    is_winner BOOLEAN DEFAULT FALSE,
    withdrawn_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_aspirants_election ON aspirants(election_id);
CREATE INDEX idx_aspirants_party ON aspirants(party_code);
CREATE INDEX idx_aspirants_status ON aspirants(screening_status);
CREATE INDEX idx_aspirants_position ON aspirants(position_sought);

-- 3. Delegate Registry
CREATE TABLE IF NOT EXISTS delegates (
    id SERIAL PRIMARY KEY,
    delegate_id TEXT NOT NULL UNIQUE DEFAULT 'del-' || gen_random_uuid()::text,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    party_code TEXT NOT NULL,
    full_name TEXT NOT NULL,
    phone_hash TEXT, -- SHA-256 of phone for lookup
    nin_hash TEXT,   -- SHA-256 of NIN for verification
    delegate_type TEXT NOT NULL DEFAULT 'elected'
        CHECK (delegate_type IN ('elected','statutory','ex_officio','special','automatic')),
    state_code TEXT,
    lga_code TEXT,
    ward_code TEXT,
    credential_number TEXT UNIQUE,
    credential_verified BOOLEAN DEFAULT FALSE,
    credential_verified_at TIMESTAMP,
    credential_verified_by INTEGER,
    accreditation_status TEXT NOT NULL DEFAULT 'registered'
        CHECK (accreditation_status IN ('registered','credential_issued','accredited','revoked','absent')),
    accredited_at TIMESTAMP,
    biometric_hash TEXT,
    device_id TEXT,
    voting_weight INTEGER DEFAULT 1, -- some delegates have weighted votes
    has_voted BOOLEAN DEFAULT FALSE,
    vote_round INTEGER, -- which round they last voted in
    vote_timestamp TIMESTAMP,
    check_in_at TIMESTAMP,
    check_out_at TIMESTAMP,
    floor_access BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_delegates_election ON delegates(election_id);
CREATE INDEX idx_delegates_party ON delegates(party_code);
CREATE INDEX idx_delegates_state ON delegates(state_code);
CREATE INDEX idx_delegates_accreditation ON delegates(accreditation_status);
CREATE INDEX idx_delegates_credential ON delegates(credential_number);

-- 4. Convention/Primary Venues
CREATE TABLE IF NOT EXISTS convention_venues (
    id SERIAL PRIMARY KEY,
    venue_id TEXT NOT NULL UNIQUE DEFAULT 'ven-' || gen_random_uuid()::text,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    name TEXT NOT NULL,
    address TEXT,
    state_code TEXT,
    lga_code TEXT,
    latitude REAL,
    longitude REAL,
    capacity INTEGER DEFAULT 0,
    venue_type TEXT DEFAULT 'main' CHECK (venue_type IN ('main','overflow','satellite','virtual')),
    is_active BOOLEAN DEFAULT TRUE,
    streaming_url TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 5. Ballots & Voting Rounds
CREATE TABLE IF NOT EXISTS voting_rounds (
    id SERIAL PRIMARY KEY,
    round_id TEXT NOT NULL UNIQUE DEFAULT 'rnd-' || gen_random_uuid()::text,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    round_number INTEGER NOT NULL DEFAULT 1,
    round_type TEXT NOT NULL DEFAULT 'regular'
        CHECK (round_type IN ('regular','runoff','tiebreaker','voice_vote','show_of_hands')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','open','voting','closed','tallying','certified','voided')),
    quorum_required INTEGER DEFAULT 0,
    quorum_present INTEGER DEFAULT 0,
    quorum_met BOOLEAN DEFAULT FALSE,
    total_eligible_voters INTEGER DEFAULT 0,
    total_votes_cast INTEGER DEFAULT 0,
    total_valid_votes INTEGER DEFAULT 0,
    total_invalid_votes INTEGER DEFAULT 0,
    voting_method TEXT DEFAULT 'secret_ballot'
        CHECK (voting_method IN ('secret_ballot','open_ballot','electronic','show_of_hands','voice_vote','remote_electronic')),
    opened_at TIMESTAMP,
    closed_at TIMESTAMP,
    certified_at TIMESTAMP,
    certified_by INTEGER,
    blockchain_hash TEXT,
    merkle_root TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(election_id, round_number)
);
CREATE INDEX idx_voting_rounds_election ON voting_rounds(election_id);

-- 6. Individual Ballots (cast votes)
CREATE TABLE IF NOT EXISTS ballots (
    id SERIAL PRIMARY KEY,
    ballot_id TEXT NOT NULL UNIQUE DEFAULT 'bal-' || gen_random_uuid()::text,
    round_id TEXT NOT NULL REFERENCES voting_rounds(round_id),
    delegate_id TEXT REFERENCES delegates(delegate_id),
    aspirant_id TEXT REFERENCES aspirants(aspirant_id),
    vote_type TEXT NOT NULL DEFAULT 'for'
        CHECK (vote_type IN ('for','against','abstain','spoiled')),
    -- Remote voting fields
    is_remote BOOLEAN DEFAULT FALSE,
    device_fingerprint TEXT,
    ip_hash TEXT, -- SHA-256 of voter IP for audit without PII
    -- ElectionGuard-style E2E verifiability
    encrypted_ballot TEXT, -- Encrypted ballot ciphertext
    ballot_proof TEXT,     -- Zero-knowledge proof of valid ballot
    confirmation_code TEXT, -- Receipt code for voter verification
    verification_hash TEXT, -- Hash chain for E2E verifiability
    -- Coercion resistance
    is_decoy BOOLEAN DEFAULT FALSE, -- Fake vote cast under duress
    -- Audit
    cast_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    tallied BOOLEAN DEFAULT FALSE,
    tallied_at TIMESTAMP,
    blockchain_tx_id TEXT,
    tb_transfer_id TEXT -- TigerBeetle transfer for vote audit
);
CREATE INDEX idx_ballots_round ON ballots(round_id);
CREATE INDEX idx_ballots_delegate ON ballots(delegate_id);
CREATE INDEX idx_ballots_aspirant ON ballots(aspirant_id);
CREATE INDEX idx_ballots_confirmation ON ballots(confirmation_code);

-- 7. Vote Tallies (per-round, per-aspirant)
CREATE TABLE IF NOT EXISTS vote_tallies (
    id SERIAL PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES voting_rounds(round_id),
    aspirant_id TEXT NOT NULL REFERENCES aspirants(aspirant_id),
    votes_received INTEGER DEFAULT 0,
    vote_percentage NUMERIC(5,2) DEFAULT 0,
    rank_position INTEGER,
    is_eliminated BOOLEAN DEFAULT FALSE,
    is_winner BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(round_id, aspirant_id)
);

-- 8. Remote Voting Device Registry
CREATE TABLE IF NOT EXISTS remote_voting_devices (
    id SERIAL PRIMARY KEY,
    device_id TEXT NOT NULL UNIQUE DEFAULT 'rvd-' || gen_random_uuid()::text,
    delegate_id TEXT NOT NULL REFERENCES delegates(delegate_id),
    device_type TEXT NOT NULL CHECK (device_type IN ('mobile','tablet','desktop','kiosk')),
    device_fingerprint TEXT NOT NULL,
    os_version TEXT,
    app_version TEXT,
    imei_hash TEXT,
    public_key TEXT, -- Device public key for encrypted ballot
    is_registered BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    registered_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    revoked_at TIMESTAMP,
    revoke_reason TEXT
);
CREATE INDEX idx_rvd_delegate ON remote_voting_devices(delegate_id);

-- 9. Voting Session Tokens (for remote voting)
CREATE TABLE IF NOT EXISTS voting_sessions (
    id SERIAL PRIMARY KEY,
    session_id TEXT NOT NULL UNIQUE DEFAULT 'vs-' || gen_random_uuid()::text,
    delegate_id TEXT NOT NULL REFERENCES delegates(delegate_id),
    round_id TEXT NOT NULL REFERENCES voting_rounds(round_id),
    device_id TEXT REFERENCES remote_voting_devices(device_id),
    otp_hash TEXT, -- SHA-256 of OTP sent to delegate
    otp_expires_at TIMESTAMP,
    biometric_verified BOOLEAN DEFAULT FALSE,
    keycloak_session_id TEXT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','authenticated','voting','voted','expired','revoked')),
    ip_hash TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP,
    completed_at TIMESTAMP
);
CREATE INDEX idx_vsessions_delegate ON voting_sessions(delegate_id);
CREATE INDEX idx_vsessions_round ON voting_sessions(round_id);

-- 10. Convention Quorum Tracking
CREATE TABLE IF NOT EXISTS quorum_snapshots (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    round_id TEXT REFERENCES voting_rounds(round_id),
    total_registered INTEGER DEFAULT 0,
    total_accredited INTEGER DEFAULT 0,
    total_present INTEGER DEFAULT 0,
    quorum_threshold NUMERIC(5,2) DEFAULT 66.67, -- 2/3 default
    quorum_met BOOLEAN DEFAULT FALSE,
    snapshot_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    recorded_by INTEGER
);

-- 11. Convention Audit Log
CREATE TABLE IF NOT EXISTS convention_audit_log (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    event_type TEXT NOT NULL, -- 'delegate_accredited','ballot_cast','round_opened','round_closed','aspirant_eliminated','winner_declared'
    actor_id TEXT,
    actor_role TEXT,
    entity_type TEXT,
    entity_id TEXT,
    details JSONB,
    ip_hash TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_convention_audit_election ON convention_audit_log(election_id);
CREATE INDEX idx_convention_audit_type ON convention_audit_log(event_type);
CREATE INDEX idx_convention_audit_time ON convention_audit_log(created_at);

-- 12. Cryptographic Key Store (for E2E verifiable voting)
CREATE TABLE IF NOT EXISTS voting_crypto_keys (
    id SERIAL PRIMARY KEY,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    key_type TEXT NOT NULL CHECK (key_type IN ('election_public','election_private','guardian','trustee','device')),
    key_purpose TEXT NOT NULL, -- 'encryption','signing','verification'
    public_key TEXT NOT NULL,
    encrypted_private_key TEXT, -- encrypted with master key
    guardian_index INTEGER, -- for threshold key sharing
    guardian_total INTEGER,
    threshold INTEGER, -- k-of-n threshold for decryption
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMP
);
CREATE INDEX idx_crypto_keys_election ON voting_crypto_keys(election_id);

-- 13. Encrypted Tally (homomorphic aggregation)
CREATE TABLE IF NOT EXISTS encrypted_tallies (
    id SERIAL PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES voting_rounds(round_id),
    aspirant_id TEXT NOT NULL REFERENCES aspirants(aspirant_id),
    encrypted_count TEXT NOT NULL, -- Homomorphic ciphertext
    partial_decryptions JSONB, -- Guardian partial decryptions
    decrypted_count INTEGER, -- Final plaintext count after threshold decryption
    proof_of_decryption TEXT, -- ZK proof that decryption is correct
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(round_id, aspirant_id)
);

-- 14. Mix-Net Shuffle Records (for ballot anonymization)
CREATE TABLE IF NOT EXISTS shuffle_records (
    id SERIAL PRIMARY KEY,
    round_id TEXT NOT NULL REFERENCES voting_rounds(round_id),
    shuffle_index INTEGER NOT NULL,
    input_ciphertexts TEXT NOT NULL, -- JSON array of input ballots
    output_ciphertexts TEXT NOT NULL, -- JSON array of shuffled ballots
    proof_of_shuffle TEXT NOT NULL, -- ZK proof of correct shuffle
    shuffler_id TEXT,
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 15. Dispute/Challenge for primary results
CREATE TABLE IF NOT EXISTS primary_disputes (
    id SERIAL PRIMARY KEY,
    dispute_id TEXT NOT NULL UNIQUE DEFAULT 'pdisp-' || gen_random_uuid()::text,
    election_id INTEGER NOT NULL REFERENCES elections(id),
    round_id TEXT REFERENCES voting_rounds(round_id),
    filed_by TEXT NOT NULL, -- aspirant_id or delegate_id
    filed_by_type TEXT NOT NULL CHECK (filed_by_type IN ('aspirant','delegate','observer','party_official')),
    dispute_type TEXT NOT NULL CHECK (dispute_type IN ('credential_fraud','vote_manipulation','quorum_violation','process_irregularity','result_challenge','coercion')),
    description TEXT NOT NULL,
    evidence_urls TEXT[], -- Array of evidence document URLs
    status TEXT NOT NULL DEFAULT 'filed'
        CHECK (status IN ('filed','under_review','hearing_scheduled','upheld','dismissed','withdrawn')),
    resolution TEXT,
    resolved_by INTEGER,
    filed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP
);
