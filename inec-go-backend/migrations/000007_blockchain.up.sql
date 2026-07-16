-- Blockchain: Fabric, IPFS, merkle trees, smart contracts

CREATE TABLE IF NOT EXISTS blockchain_audit_trail (
    id SERIAL PRIMARY KEY,
    action text NOT NULL,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    actor text,
    prev_state text,
    new_state text,
    tx_hash text NOT NULL,
    block_ref integer,
    ip_address text,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS blockchain_results (
    id SERIAL PRIMARY KEY,
    result_id integer NOT NULL,
    ec8a_hash text NOT NULL,
    prev_hash text DEFAULT ''::text NOT NULL,
    block_index integer NOT NULL,
    nonce integer DEFAULT 0,
    block_hash text NOT NULL,
    merkle_root text,
    level text NOT NULL,
    smart_contract_id text,
    validation_status text DEFAULT 'pending'::text NOT NULL,
    validator_count integer DEFAULT 0,
    "timestamp" timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT blockchain_results_level_check CHECK ((level = ANY (ARRAY['polling_unit'::text, 'ward'::text, 'lga'::text, 'state'::text, 'national'::text]))),
    CONSTRAINT blockchain_results_validation_status_check CHECK ((validation_status = ANY (ARRAY['pending'::text, 'validated'::text, 'rejected'::text, 'disputed'::text])))
);

CREATE INDEX IF NOT EXISTS idx_blockchain_result ON blockchain_results USING btree (result_id);

CREATE TABLE IF NOT EXISTS chaincode_events (
    id SERIAL PRIMARY KEY,
    chaincode_id text NOT NULL,
    event_name text NOT NULL,
    tx_id text,
    payload text,
    block_number integer,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_blocks (
    block_number integer NOT NULL,
    channel_id text NOT NULL,
    prev_hash text NOT NULL,
    data_hash text NOT NULL,
    block_hash text NOT NULL,
    tx_count integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_chaincode (
    chaincode_id text NOT NULL,
    version text NOT NULL,
    channel_id text NOT NULL,
    endorsement_policy text NOT NULL,
    state_db text DEFAULT '{}'::text,
    install_date timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    status text DEFAULT 'active'::text
);

CREATE TABLE IF NOT EXISTS fabric_endorsement_log (
    id SERIAL PRIMARY KEY,
    tx_id text NOT NULL,
    peer_id text NOT NULL,
    msp_id text NOT NULL,
    signature text NOT NULL,
    proposal_hash text NOT NULL,
    response_status integer DEFAULT 200,
    response_payload text,
    endorsement_time_ms integer DEFAULT 0,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_endorse ON fabric_endorsement_log USING btree (tx_id);

CREATE TABLE IF NOT EXISTS fabric_orderers (
    orderer_id text NOT NULL,
    org text NOT NULL,
    endpoint text NOT NULL,
    consensus_type text DEFAULT 'raft'::text,
    status text DEFAULT 'active'::text
);

CREATE TABLE IF NOT EXISTS fabric_peers (
    peer_id text NOT NULL,
    org text NOT NULL,
    msp_id text NOT NULL,
    endpoint text NOT NULL,
    role text DEFAULT 'endorser'::text,
    status text DEFAULT 'active'::text,
    last_seen timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_signing_keys (
    key_id text NOT NULL,
    private_key_pem text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS fabric_state_db (
    composite_key text NOT NULL,
    channel_id text NOT NULL,
    chaincode_id text NOT NULL,
    key text NOT NULL,
    value text NOT NULL,
    version_block integer DEFAULT 0 NOT NULL,
    version_tx integer DEFAULT 0 NOT NULL,
    is_delete integer DEFAULT 0,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_state ON fabric_state_db USING btree (channel_id, chaincode_id, key);

CREATE TABLE IF NOT EXISTS fabric_transactions (
    tx_id text NOT NULL,
    block_number integer,
    channel_id text NOT NULL,
    chaincode_id text NOT NULL,
    function_name text NOT NULL,
    args text,
    creator_msp text NOT NULL,
    endorsers text,
    endorsement_policy text,
    rw_set text,
    validation_code text DEFAULT 'VALID'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fabric_tx_block ON fabric_transactions USING btree (block_number);
CREATE INDEX IF NOT EXISTS idx_fabric_tx_cc ON fabric_transactions USING btree (chaincode_id);

CREATE TABLE IF NOT EXISTS ipfs_dag_nodes (
    cid text NOT NULL,
    codec text DEFAULT 'dag-cbor'::text NOT NULL,
    multihash text NOT NULL,
    links text DEFAULT '[]'::text,
    data_size integer NOT NULL,
    raw_data bytea,
    pin_status text DEFAULT 'pinned'::text,
    replication_factor integer DEFAULT 3,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ipfs_dag ON ipfs_dag_nodes USING btree (codec, created_at);

CREATE TABLE IF NOT EXISTS ipfs_objects (
    cid text NOT NULL,
    content_type text NOT NULL,
    data_hash text NOT NULL,
    size_bytes integer NOT NULL,
    pinned integer DEFAULT 1,
    pin_count integer DEFAULT 1,
    references_to text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ipfs_type ON ipfs_objects USING btree (content_type);

CREATE TABLE IF NOT EXISTS ipfs_pins (
    cid text NOT NULL,
    node_id text NOT NULL,
    pin_type text DEFAULT 'recursive'::text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS merkle_trees (
    id SERIAL PRIMARY KEY,
    root_hash text NOT NULL,
    tree_type text NOT NULL,
    leaf_count integer NOT NULL,
    depth integer NOT NULL,
    leaves text NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS smart_contracts (
    id SERIAL PRIMARY KEY,
    contract_id text NOT NULL,
    contract_type text NOT NULL,
    level text NOT NULL,
    area_code text NOT NULL,
    election_id integer NOT NULL,
    conditions text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    executed_at timestamp without time zone,
    result_hash text,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT smart_contracts_contract_type_check CHECK ((contract_type = ANY (ARRAY['pu_validation'::text, 'ward_aggregation'::text, 'lga_aggregation'::text, 'state_aggregation'::text, 'national_declaration'::text]))),
    CONSTRAINT smart_contracts_status_check CHECK ((status = ANY (ARRAY['active'::text, 'executed'::text, 'failed'::text, 'expired'::text])))
);

CREATE INDEX IF NOT EXISTS idx_smart_contract ON smart_contracts USING btree (election_id, level);


