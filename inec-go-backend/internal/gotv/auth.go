// Package gotv — Auth middleware with JWT validation + API key verification.
package gotv

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	AuthServiceURL string // URL of auth-svc for JWT validation
	DevMode        bool   // Skip JWT validation in development
}

// AuthMiddleware validates requests via JWT or API key.
type AuthMiddleware struct {
	db     *sql.DB
	config AuthConfig
	client *http.Client
	// internalToken is the shared secret (env INTERNAL_SERVICE_SECRET, with
	// GOTV_GATEWAY_SECRET accepted as a legacy alias) that the API gateway
	// MUST present via the X-Internal-Token header before
	// X-Party-ID/X-Internal-Service trust headers are honored. SECURITY:
	// when empty, those trust headers are rejected outright (fail closed) —
	// otherwise any direct client could impersonate any party by setting
	// two headers.
	internalToken string
	// Rate limiter: party_id -> (count, window_start)
	rateMap map[int]*rateEntry
	rateMu  sync.RWMutex
}

type rateEntry struct {
	count     int
	windowEnd time.Time
}

// NewAuthMiddleware creates auth middleware with JWT + API key support.
func NewAuthMiddleware(db *sql.DB, config AuthConfig) *AuthMiddleware {
	secret := os.Getenv("INTERNAL_SERVICE_SECRET")
	if secret == "" {
		// Legacy alias kept for existing deployments.
		secret = os.Getenv("GOTV_GATEWAY_SECRET")
	}
	if secret == "" {
		// SECURITY: gateway trust headers fail closed. In production this
		// means the X-Party-ID inter-service path is dead until the shared
		// secret is configured — by design.
		log.Warn().Msg("GOTV auth: INTERNAL_SERVICE_SECRET is not set — X-Party-ID/X-Internal-Service gateway trust headers will be REJECTED (fail closed). Set a strong shared secret on the gateway and this service to enable inter-service trust.")
	}
	return &AuthMiddleware{
		db:            db,
		config:        config,
		client:        &http.Client{Timeout: 5 * time.Second},
		internalToken: secret,
		rateMap:       make(map[int]*rateEntry),
	}
}

// Wrap returns an http.HandlerFunc that validates auth before calling next.
func (am *AuthMiddleware) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		partyID, userID, err := am.authenticate(r)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Rate limiting
		if err := am.checkRateLimit(partyID); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		r.Header.Set("X-GOTV-Party-ID", strconv.Itoa(partyID))
		r.Header.Set("X-GOTV-User", userID)
		next(w, r)
	}
}

// Authenticate validates the request and returns (partyID, userID, error).
// Exported for use by WebSocket handler which can't use the Wrap middleware.
func (am *AuthMiddleware) Authenticate(r *http.Request) (int, string, error) {
	return am.authenticate(r)
}

func (am *AuthMiddleware) authenticate(r *http.Request) (int, string, error) {
	// Method 1: X-API-Key header (party API key)
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return am.validateAPIKey(apiKey)
	}

	// Method 2: Bearer JWT token (from header or query param for SSE/EventSource)
	auth := r.Header.Get("Authorization")
	if auth == "" {
		if qToken := r.URL.Query().Get("token"); qToken != "" {
			auth = "Bearer " + qToken
		}
	}
	if auth != "" && strings.HasPrefix(auth, "Bearer ") {
		token := strings.TrimPrefix(auth, "Bearer ")
		return am.validateJWT(r.Context(), token, r)
	}

	// Method 3: X-Party-ID from gateway (inter-service trust)
	if pid := r.Header.Get("X-Party-ID"); pid != "" {
		if r.Header.Get("X-Internal-Service") == "gateway" {
			// SECURITY: trust headers are spoofable by any direct client.
			// They are honored ONLY when the caller also proves possession of
			// the inter-service shared secret (INTERNAL_SERVICE_SECRET) via
			// the X-Internal-Token header, compared in constant time. When
			// the secret is not configured on this service (e.g. forgotten
			// in production), fail CLOSED — never trust caller-supplied
			// identity headers.
			if am.internalToken == "" {
				return 0, "", fmt.Errorf("unauthorized: gateway trust headers rejected (INTERNAL_SERVICE_SECRET not configured)")
			}
			provided := r.Header.Get("X-Internal-Token")
			if provided == "" {
				// Legacy header name kept for existing gateway deployments.
				provided = r.Header.Get("X-Gateway-Secret")
			}
			if provided == "" ||
				subtle.ConstantTimeCompare([]byte(provided), []byte(am.internalToken)) != 1 {
				return 0, "", fmt.Errorf("unauthorized: invalid internal service token")
			}
			partyID, err := strconv.Atoi(pid)
			if err != nil {
				return 0, "", fmt.Errorf("invalid party_id")
			}
			user := r.Header.Get("X-User")
			if user == "" {
				user = "gateway"
			}
			return partyID, user, nil
		}
	}

	return 0, "", fmt.Errorf("unauthorized: provide Bearer token or X-API-Key")
}

func (am *AuthMiddleware) validateAPIKey(apiKey string) (int, string, error) {
	hash := sha256.Sum256([]byte(apiKey))
	hashHex := hex.EncodeToString(hash[:])

	var partyID int
	var isActive bool
	var expiresAt sql.NullTime
	var createdBy string

	err := am.db.QueryRow(
		`SELECT party_id, is_active, expires_at, created_by
		 FROM gotv_party_access WHERE api_key_hash=$1`,
		hashHex,
	).Scan(&partyID, &isActive, &expiresAt, &createdBy)

	if err == sql.ErrNoRows {
		return 0, "", fmt.Errorf("invalid API key")
	}
	if err != nil {
		return 0, "", fmt.Errorf("auth error: %w", err)
	}

	if !isActive {
		return 0, "", fmt.Errorf("API key is disabled")
	}
	if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
		return 0, "", fmt.Errorf("API key expired")
	}

	// The constant-time property is provided by looking the key up by its
	// SHA-256 hash (no secret material is compared byte-by-byte here); a
	// matching active row is proof of validity.
	return partyID, createdBy, nil
}

func (am *AuthMiddleware) validateJWT(ctx context.Context, token string, r *http.Request) (int, string, error) {
	if am.config.AuthServiceURL == "" {
		// No auth service configured — extract from token claims if possible
		if am.config.DevMode {
			return 1, "dev-user", nil
		}
		return 0, "", fmt.Errorf("auth service not configured")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", am.config.AuthServiceURL+"/me", nil)
	if err != nil {
		return 0, "", fmt.Errorf("auth service request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := am.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("auth service unavailable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("invalid token (auth service returned %d)", resp.StatusCode)
	}

	var user struct {
		ID      int    `json:"id"`
		Email   string `json:"email"`
		PartyID int    `json:"party_id"`
		Role    string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return 0, "", fmt.Errorf("auth service response invalid: %w", err)
	}

	if user.PartyID == 0 {
		return 0, "", fmt.Errorf("user not associated with a party")
	}

	return user.PartyID, user.Email, nil
}

func (am *AuthMiddleware) checkRateLimit(partyID int) error {
	var limit int
	err := am.db.QueryRow(
		`SELECT COALESCE(rate_limit_per_hour, 1000) FROM gotv_party_access WHERE party_id=$1 AND is_active=TRUE`,
		partyID,
	).Scan(&limit)
	if err != nil {
		limit = 1000 // default
	}

	am.rateMu.Lock()
	defer am.rateMu.Unlock()

	now := time.Now()
	entry, ok := am.rateMap[partyID]
	if !ok || now.After(entry.windowEnd) {
		am.rateMap[partyID] = &rateEntry{count: 1, windowEnd: now.Add(time.Hour)}
		return nil
	}

	entry.count++
	if entry.count > limit {
		return fmt.Errorf("rate limit exceeded (%d requests/hour)", limit)
	}
	return nil
}
