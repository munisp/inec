-- Middleware persistence tables

BEGIN;

-- Mojaloop transactions
CREATE TABLE IF NOT EXISTS moja_transactions (
    id SERIAL PRIMARY KEY,
    transaction_id TEXT UNIQUE NOT NULL,
    payer_fsp TEXT NOT NULL,
    payee_fsp TEXT NOT NULL,
    amount DOUBLE PRECISION NOT NULL,
    currency TEXT NOT NULL DEFAULT 'NGN',
    state TEXT NOT NULL DEFAULT 'RECEIVED',
    ilp_packet TEXT,
    condition TEXT,
    fulfilment TEXT,
    settlement_id TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- OpenSearch document index (DB-backed fallback)
CREATE TABLE IF NOT EXISTS search_documents (
    id SERIAL PRIMARY KEY,
    index_name TEXT NOT NULL,
    doc_id TEXT NOT NULL,
    content JSONB NOT NULL,
    text_content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(index_name, doc_id)
);

-- WAF threat log
CREATE TABLE IF NOT EXISTS waf_threats (
    id SERIAL PRIMARY KEY,
    source_ip TEXT NOT NULL,
    request_path TEXT,
    attack_type TEXT NOT NULL,
    severity TEXT NOT NULL,
    action TEXT NOT NULL DEFAULT 'blocked',
    details TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- WAF IP blocklist
CREATE TABLE IF NOT EXISTS waf_blocklist (
    id SERIAL PRIMARY KEY,
    ip_address TEXT UNIQUE NOT NULL,
    reason TEXT,
    blocked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_moja_transactions_state ON moja_transactions(state);
CREATE INDEX IF NOT EXISTS idx_search_documents_index ON search_documents(index_name);
CREATE INDEX IF NOT EXISTS idx_search_documents_text ON search_documents USING gin(to_tsvector('english', text_content));
CREATE INDEX IF NOT EXISTS idx_waf_threats_type ON waf_threats(attack_type);
CREATE INDEX IF NOT EXISTS idx_waf_threats_created ON waf_threats(created_at);

COMMIT;
