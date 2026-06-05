package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ── Token Blacklist / Session Revocation ──

// tokenBlacklist maintains a set of revoked JWT tokens (by jti claim).
// Supports both in-memory (with periodic DB sync) and Redis-backed modes.
type tokenBlacklist struct {
	mu     sync.RWMutex
	tokens map[string]time.Time // jti -> expiry
}

var blacklist = &tokenBlacklist{
	tokens: make(map[string]time.Time),
}

func initTokenBlacklist(database *sql.DB) {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS token_blacklist (
		jti TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL,
		revoked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL,
		reason TEXT DEFAULT ''
	)`)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create token_blacklist table")
		return
	}

	// Load existing blacklisted tokens from DB
	rows, err := database.Query("SELECT jti, expires_at FROM token_blacklist WHERE expires_at > CURRENT_TIMESTAMP")
	if err != nil {
		log.Error().Err(err).Msg("Failed to load token blacklist from DB")
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var jti string
		var expiresAt time.Time
		if err := rows.Scan(&jti, &expiresAt); err == nil {
			blacklist.mu.Lock()
			blacklist.tokens[jti] = expiresAt
			blacklist.mu.Unlock()
			count++
		}
	}
	log.Info().Int("count", count).Msg("Token blacklist loaded from DB")

	// Start cleanup goroutine
	go blacklist.periodicCleanup()
}

// isBlacklisted checks if a token (by jti) has been revoked.
func (bl *tokenBlacklist) isBlacklisted(jti string) bool {
	// Try Redis first for distributed blacklist
	if mwHub != nil && mwHub.Redis != nil {
		ctx := context.Background()
		val, err := mwHub.Redis.Get(ctx, "blacklist:"+jti)
		if err == nil && val != "" {
			return true
		}
	}

	bl.mu.RLock()
	defer bl.mu.RUnlock()
	exp, exists := bl.tokens[jti]
	if !exists {
		return false
	}
	// Clean up expired entries lazily
	if time.Now().After(exp) {
		return false
	}
	return true
}

// revokeToken adds a token to the blacklist.
func (bl *tokenBlacklist) revokeToken(jti string, userID int, expiresAt time.Time, reason string) error {
	bl.mu.Lock()
	bl.tokens[jti] = expiresAt
	bl.mu.Unlock()

	// Persist to Redis for distributed awareness
	if mwHub != nil && mwHub.Redis != nil {
		ctx := context.Background()
		ttl := time.Until(expiresAt)
		if ttl > 0 {
			mwHub.Redis.Set(ctx, "blacklist:"+jti, "1", ttl)
		}
	}

	// Persist to DB
	_, err := db.Exec(convertPlaceholders(
		"INSERT OR REPLACE INTO token_blacklist (jti, user_id, revoked_at, expires_at, reason) VALUES (?, ?, CURRENT_TIMESTAMP, ?, ?)"),
		jti, userID, expiresAt, reason)
	if err != nil {
		log.Error().Err(err).Str("jti", jti).Msg("Failed to persist token revocation")
	}
	return err
}

// revokeAllForUser revokes all active tokens for a user.
func (bl *tokenBlacklist) revokeAllForUser(userID int) error {
	_, err := db.Exec(convertPlaceholders(
		"INSERT INTO token_blacklist (jti, user_id, expires_at, reason) SELECT jti, ?, expires_at, 'bulk_revocation' FROM active_sessions WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP"),
		userID, userID)
	return err
}

// periodicCleanup removes expired entries every 5 minutes.
func (bl *tokenBlacklist) periodicCleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		bl.mu.Lock()
		now := time.Now()
		for jti, exp := range bl.tokens {
			if now.After(exp) {
				delete(bl.tokens, jti)
			}
		}
		bl.mu.Unlock()

		// Clean DB too
		dbExecLog("token_blacklist", "DELETE FROM token_blacklist WHERE expires_at < CURRENT_TIMESTAMP")
	}
}

// generateJTI creates a unique token identifier.
func generateJTI() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Active Sessions Table ──

func initActiveSessions(database *sql.DB) {
	database.Exec(`CREATE TABLE IF NOT EXISTS active_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		jti TEXT UNIQUE NOT NULL,
		user_id INTEGER NOT NULL,
		ip_address TEXT,
		user_agent TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP NOT NULL,
		last_activity TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	database.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_user ON active_sessions(user_id)")
	database.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_jti ON active_sessions(jti)")
}

// recordSession stores a new session when a token is issued.
func recordSession(jti string, userID int, expiresAt time.Time, r *http.Request) {
	ip := stripPort(r.RemoteAddr)
	ua := r.UserAgent()
	dbExecLog("db_op", convertPlaceholders(
		"INSERT INTO active_sessions (jti, user_id, ip_address, user_agent, expires_at) VALUES (?, ?, ?, ?, ?)"),
		jti, userID, ip, ua, expiresAt)
}

// ── API Key Rotation ──

func initAPIKeyRotation(database *sql.DB) {
	database.Exec(`CREATE TABLE IF NOT EXISTS api_key_metadata (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key_hash TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		owner_id INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		expires_at TIMESTAMP,
		rotated_from TEXT,
		is_active BOOLEAN DEFAULT 1,
		last_used_at TIMESTAMP,
		usage_count INTEGER DEFAULT 0
	)`)
}

// rotateAPIKey creates a new key and marks the old one as expired.
func rotateAPIKey(oldKeyHash string, ownerID int, name string) (string, error) {
	// Generate new key
	b := make([]byte, 32)
	rand.Read(b)
	newKey := hex.EncodeToString(b)
	newKeyHash := hashAPIKey(newKey)
	expiresAt := time.Now().Add(90 * 24 * time.Hour) // 90-day expiry

	tx, err := db.Begin()
	if err != nil {
		return "", err
	}

	// Deactivate old key
	if oldKeyHash != "" {
		_, err = tx.Exec(convertPlaceholders(
			"UPDATE api_key_metadata SET is_active = 0 WHERE key_hash = ?"), oldKeyHash)
		if err != nil {
			tx.Rollback()
			return "", err
		}
	}

	// Insert new key
	_, err = tx.Exec(convertPlaceholders(
		"INSERT INTO api_key_metadata (key_hash, name, owner_id, expires_at, rotated_from) VALUES (?, ?, ?, ?, ?)"),
		newKeyHash, name, ownerID, expiresAt, oldKeyHash)
	if err != nil {
		tx.Rollback()
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	log.Info().Str("name", name).Int("owner_id", ownerID).Msg("API key rotated")
	return newKey, nil
}

// isAPIKeyValid checks if an API key is active and not expired.
func isAPIKeyValid(keyHash string) bool {
	var isActive bool
	var expiresAt sql.NullTime
	err := db.QueryRow(convertPlaceholders(
		"SELECT is_active, expires_at FROM api_key_metadata WHERE key_hash = ?"), keyHash).Scan(&isActive, &expiresAt)
	if err != nil {
		return false
	}
	if !isActive {
		return false
	}
	if expiresAt.Valid && time.Now().After(expiresAt.Time) {
		return false
	}
	// Update last used
	dbExecLog("db_op", convertPlaceholders(
		"UPDATE api_key_metadata SET last_used_at = CURRENT_TIMESTAMP, usage_count = usage_count + 1 WHERE key_hash = ?"), keyHash)
	return true
}

// ── Session Management Handlers ──

func handleListSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin")
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	var userID int
	fmt.Sscanf(userIDStr, "%d", &userID)

	rows, err := db.Query(convertPlaceholders(
		"SELECT id, jti, ip_address, user_agent, created_at, last_activity FROM active_sessions WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP ORDER BY last_activity DESC"), userID)
	if err != nil {
		writeError(w, 500, "failed to list sessions")
		return
	}
	defer rows.Close()

	var sessions []map[string]interface{}
	for rows.Next() {
		var id int
		var jti, ip, ua, createdAt, lastActivity string
		if err := rows.Scan(&id, &jti, &ip, &ua, &createdAt, &lastActivity); err == nil {
			sessions = append(sessions, map[string]interface{}{
				"id": id, "jti": jti, "ip_address": ip, "user_agent": ua,
				"created_at": createdAt, "last_activity": lastActivity,
			})
		}
	}
	writeJSON(w, 200, map[string]interface{}{"sessions": sessions})
}

func handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	var req struct {
		JTI    string `json:"jti" validate:"required"`
		Reason string `json:"reason"`
	}
	if !decodeAndValidateBody(w, r, &req) {
		return
	}

	userIDStr, _ := claims["sub"].(string)
	var userID int
	fmt.Sscanf(userIDStr, "%d", &userID)

	expiresAt := time.Now().Add(24 * time.Hour) // Token max lifetime
	if err := blacklist.revokeToken(req.JTI, userID, expiresAt, req.Reason); err != nil {
		writeError(w, 500, "failed to revoke session")
		return
	}

	// Remove from active sessions
	dbExecLog("active_sessions", convertPlaceholders("DELETE FROM active_sessions WHERE jti = ? AND user_id = ?"), req.JTI, userID)

	auditWrite("session_revoked", "session", req.JTI, r, map[string]interface{}{"reason": req.Reason})
	writeJSON(w, 200, map[string]interface{}{"message": "session revoked"})
}

func handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardAuth(w, r)
	if !ok {
		return
	}
	userIDStr, _ := claims["sub"].(string)
	var userID int
	fmt.Sscanf(userIDStr, "%d", &userID)

	blacklist.revokeAllForUser(userID)
	dbExecLog("active_sessions", convertPlaceholders("DELETE FROM active_sessions WHERE user_id = ?"), userID)

	auditWrite("all_sessions_revoked", "user", userIDStr, r, nil)
	writeJSON(w, 200, map[string]interface{}{"message": "all sessions revoked"})
}

func handleRotateAPIKey(w http.ResponseWriter, r *http.Request) {
	claims, ok := guardRole(w, r, "admin")
	if !ok {
		return
	}
	var req struct {
		OldKeyHash string `json:"old_key_hash"`
		Name       string `json:"name" validate:"required"`
	}
	if !decodeAndValidateBody(w, r, &req) {
		return
	}

	userIDStr, _ := claims["sub"].(string)
	var userID int
	fmt.Sscanf(userIDStr, "%d", &userID)

	newKey, err := rotateAPIKey(req.OldKeyHash, userID, req.Name)
	if err != nil {
		writeError(w, 500, "failed to rotate API key")
		return
	}

	auditWrite("api_key_rotated", "api_key", req.Name, r, nil)
	writeJSON(w, 200, map[string]interface{}{
		"message": "API key rotated successfully",
		"api_key": newKey,
		"note":    "Store this key securely — it won't be shown again",
	})
}

// ── mTLS Configuration ──

// MTLSConfig holds TLS configuration for inter-service communication.
type MTLSConfig struct {
	Enabled    bool   `json:"enabled"`
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	ServerName string `json:"server_name"`
}
func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	return v == "true" || v == "1" || (v == "" && fallback)
}

func envString(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
