package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

var merkleBuilder *MerkleTreeBuilder

func initBlockchainProduction(database *sql.DB) {
	execMulti(database, `
	CREATE TABLE IF NOT EXISTS tb_accounts (
		id TEXT PRIMARY KEY,
		ledger INTEGER NOT NULL,
		code INTEGER NOT NULL,
		credits_posted INTEGER DEFAULT 0,
		debits_posted INTEGER DEFAULT 0,
		credits_pending INTEGER DEFAULT 0,
		debits_pending INTEGER DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS tb_transfers (
		id TEXT PRIMARY KEY,
		debit_account_id TEXT NOT NULL,
		credit_account_id TEXT NOT NULL,
		amount INTEGER NOT NULL,
		ledger INTEGER NOT NULL,
		code INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'PENDING',
		user_data TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		posted_at TIMESTAMP,
		FOREIGN KEY (debit_account_id) REFERENCES tb_accounts(id),
		FOREIGN KEY (credit_account_id) REFERENCES tb_accounts(id)
	);
	CREATE TABLE IF NOT EXISTS fabric_blocks (
		block_number INTEGER PRIMARY KEY,
		channel_id TEXT NOT NULL,
		prev_hash TEXT NOT NULL,
		data_hash TEXT NOT NULL,
		block_hash TEXT NOT NULL,
		tx_count INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS fabric_transactions (
		tx_id TEXT PRIMARY KEY,
		block_number INTEGER,
		channel_id TEXT NOT NULL,
		chaincode_id TEXT NOT NULL,
		function_name TEXT NOT NULL,
		args TEXT,
		creator_msp TEXT NOT NULL,
		endorsers TEXT,
		endorsement_policy TEXT,
		rw_set TEXT,
		validation_code TEXT DEFAULT 'VALID',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (block_number) REFERENCES fabric_blocks(block_number)
	);
	CREATE TABLE IF NOT EXISTS fabric_chaincode (
		chaincode_id TEXT PRIMARY KEY,
		version TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		endorsement_policy TEXT NOT NULL,
		state_db TEXT DEFAULT '{}',
		install_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		status TEXT DEFAULT 'active'
	);
	CREATE TABLE IF NOT EXISTS fabric_peers (
		peer_id TEXT PRIMARY KEY,
		org TEXT NOT NULL,
		msp_id TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		role TEXT DEFAULT 'endorser',
		status TEXT DEFAULT 'active',
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS fabric_orderers (
		orderer_id TEXT PRIMARY KEY,
		org TEXT NOT NULL,
		endpoint TEXT NOT NULL,
		consensus_type TEXT DEFAULT 'raft',
		status TEXT DEFAULT 'active'
	);
	CREATE TABLE IF NOT EXISTS ipfs_objects (
		cid TEXT PRIMARY KEY,
		content_type TEXT NOT NULL,
		data_hash TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		pinned INTEGER DEFAULT 1,
		pin_count INTEGER DEFAULT 1,
		references_to TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS ipfs_pins (
		cid TEXT NOT NULL,
		node_id TEXT NOT NULL,
		pin_type TEXT DEFAULT 'recursive',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (cid, node_id)
	);
	CREATE TABLE IF NOT EXISTS merkle_trees (
		id SERIAL PRIMARY KEY,
		root_hash TEXT NOT NULL,
		tree_type TEXT NOT NULL,
		leaf_count INTEGER NOT NULL,
		depth INTEGER NOT NULL,
		leaves TEXT NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS chaincode_events (
		id SERIAL PRIMARY KEY,
		chaincode_id TEXT NOT NULL,
		event_name TEXT NOT NULL,
		tx_id TEXT,
		payload TEXT,
		block_number INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_fabric_tx_block ON fabric_transactions(block_number);
	CREATE INDEX IF NOT EXISTS idx_fabric_tx_cc ON fabric_transactions(chaincode_id);
	CREATE INDEX IF NOT EXISTS idx_ipfs_type ON ipfs_objects(content_type);
	CREATE INDEX IF NOT EXISTS idx_tb_transfers_status ON tb_transfers(status);
	CREATE INDEX IF NOT EXISTS idx_tb_transfers_debit ON tb_transfers(debit_account_id);
	CREATE INDEX IF NOT EXISTS idx_tb_transfers_credit ON tb_transfers(credit_account_id);
	`)

	// The local Merkle builder is deterministic and database-backed. External
	// Fabric/IPFS clients are intentionally not instantiated until real gateway
	// configuration is supplied; the previous PostgreSQL simulations are disabled.
	merkleBuilder = NewMerkleTreeBuilder(database)

}

type MerkleTreeBuilder struct {
	db *sql.DB
}

func NewMerkleTreeBuilder(database *sql.DB) *MerkleTreeBuilder {
	return &MerkleTreeBuilder{db: database}
}

func (m *MerkleTreeBuilder) BuildTree(leaves []string, treeType string) M {
	if len(leaves) == 0 {
		return M{"root_hash": "", "depth": 0, "leaf_count": 0}
	}
	hashes := make([]string, len(leaves))
	for i, l := range leaves {
		h := sha256.Sum256([]byte(l))
		hashes[i] = hex.EncodeToString(h[:])
	}
	depth := 0
	for len(hashes) > 1 {
		depth++
		var next []string
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := sha256.Sum256([]byte(hashes[i] + hashes[i+1]))
				next = append(next, hex.EncodeToString(combined[:]))
			} else {
				next = append(next, hashes[i])
			}
		}
		hashes = next
	}
	rootHash := hashes[0]
	leavesJSON, _ := json.Marshal(leaves)
	dbExecLog("merkle_tree", `INSERT INTO merkle_trees (root_hash, tree_type, leaf_count, depth, leaves) VALUES (?,?,?,?,?)`,
		rootHash, treeType, len(leaves), depth, string(leavesJSON))
	return M{
		"root_hash": rootHash, "depth": depth, "leaf_count": len(leaves),
		"tree_type": treeType,
	}
}
func handleExternalBlockchainUnavailable(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusServiceUnavailable, "External Hyperledger Fabric/IPFS integration is not configured; simulated backends are disabled")
}

func nativeTigerBeetleClient(w http.ResponseWriter) TigerBeetleClient {
	if mwHub == nil || mwHub.TigerBeetle == nil {
		writeError(w, http.StatusServiceUnavailable, "native TigerBeetle client is unavailable")
		return nil
	}
	return mwHub.TigerBeetle
}

func handlePersistentTBStats(w http.ResponseWriter, r *http.Request) {
	client := nativeTigerBeetleClient(w)
	if client == nil {
		return
	}
	ids := strings.Split(strings.TrimSpace(r.URL.Query().Get("account_ids")), ",")
	if len(ids) == 0 || strings.TrimSpace(ids[0]) == "" {
		writeError(w, http.StatusBadRequest, "account_ids is required for native TigerBeetle statistics")
		return
	}
	accounts := make([]*TBAccount, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		account, err := client.GetAccount(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		accounts = append(accounts, account)
	}
	writeJSON(w, http.StatusOK, M{"mode": "native_tigerbeetle", "accounts": accounts, "account_count": len(accounts)})
}

func handlePersistentTBAccounts(w http.ResponseWriter, r *http.Request) {
	client := nativeTigerBeetleClient(w)
	if client == nil {
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required; TigerBeetle does not support unrestricted account enumeration")
		return
	}
	account, err := client.GetAccount(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, M{"mode": "native_tigerbeetle", "account": account})
}

func handlePersistentTBTransfers(w http.ResponseWriter, r *http.Request) {
	client := nativeTigerBeetleClient(w)
	if client == nil {
		return
	}
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	if accountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required")
		return
	}
	limit := queryParamInt(r, "limit", 50)
	transfers, err := client.LookupTransfers(r.Context(), accountID, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, M{"mode": "native_tigerbeetle", "transfers": transfers, "account_id": accountID})
}

func handlePersistentTBCreateTransfer(w http.ResponseWriter, r *http.Request) {
	client := nativeTigerBeetleClient(w)
	if client == nil {
		return
	}
	var req struct {
		ID             string `json:"id"`
		DebitAccount   string `json:"debit_account"`
		CreditAccount  string `json:"credit_account"`
		Amount         int64  `json:"amount"`
		Ledger         int    `json:"ledger"`
		Code           int    `json:"code"`
		UserData       string `json:"user_data"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.DebitAccount) == "" || strings.TrimSpace(req.CreditAccount) == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "debit_account, credit_account, and a positive amount are required")
		return
	}
	if req.Ledger <= 0 {
		req.Ledger = 1
	}
	if req.Code <= 0 {
		req.Code = 1
	}
	transfer, err := client.CreateTransfer(r.Context(), TBTransfer{
		ID: req.ID, DebitAccountID: req.DebitAccount, CreditAccountID: req.CreditAccount,
		Amount: req.Amount, Ledger: req.Ledger, Code: req.Code, Status: "PENDING",
		UserData: req.UserData, IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, M{"mode": "native_tigerbeetle", "transfer": transfer})
}

func handlePersistentTBPostTransfer(w http.ResponseWriter, r *http.Request) {
	client := nativeTigerBeetleClient(w)
	if client == nil {
		return
	}
	var req struct {
		TransferID string `json:"transfer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if strings.TrimSpace(req.TransferID) == "" {
		writeError(w, http.StatusBadRequest, "transfer_id is required")
		return
	}
	if err := client.PostTransfer(r.Context(), req.TransferID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, M{"mode": "native_tigerbeetle", "transfer_id": req.TransferID, "status": "POSTED"})
}

func handleMerkleTreeBuild(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Leaves   []string `json:"leaves"`
		TreeType string   `json:"tree_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	if len(req.Leaves) == 0 {
		writeError(w, 400, "leaves required")
		return
	}
	if req.TreeType == "" {
		req.TreeType = "custom"
	}
	result := merkleBuilder.BuildTree(req.Leaves, req.TreeType)
	writeJSON(w, 200, result)
}

func handleMerkleTreeList(w http.ResponseWriter, r *http.Request) {
	limit := queryParamInt(r, "limit", 20)
	rows, _ := merkleBuilder.db.Query(`SELECT id, root_hash, tree_type, leaf_count, depth, created_at FROM merkle_trees ORDER BY id DESC LIMIT ?`, limit)
	if rows == nil {
		writeJSON(w, 200, M{"trees": []M{}})
		return
	}
	defer rows.Close()
	trees := []M{}
	for rows.Next() {
		var id, leafCount, depth int
		var rootHash, treeType, created string
		rows.Scan(&id, &rootHash, &treeType, &leafCount, &depth, &created)
		trees = append(trees, M{
			"id": id, "root_hash": rootHash[:16] + "...", "tree_type": treeType,
			"leaf_count": leafCount, "depth": depth, "created_at": created,
		})
	}
	writeJSON(w, 200, M{"trees": trees})
}
