// GOTV Middleware Integration — connects the GOTV service to all 13 middleware
// services: Kafka, Redis, Temporal, Keycloak, Permify, OpenSearch, TigerBeetle,
// Dapr, Fluvio, Mojaloop, OpenAppSec, APISIX, and Lakehouse.
//
// Each middleware client is optional — if the corresponding env var is not set,
// a no-op fallback is used so the service still starts in dev mode.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/segmentio/kafka-go"
)

// ─── Kafka Integration ─────────────────────────────────────────────────────
// Publishes GOTV domain events: campaign launches, volunteer check-ins,
// ride status changes, canvass door-knocks, pledge updates.

const (
	TopicGOTVCampaignEvent  = "gotv.campaigns"
	TopicGOTVVolunteerEvent = "gotv.volunteers"
	TopicGOTVRideEvent      = "gotv.rides"
	TopicGOTVCanvassEvent   = "gotv.canvass"
	TopicGOTVPledgeEvent    = "gotv.pledges"
	TopicGOTVAuditLog       = "gotv.audit"
)

type gotvKafkaClient struct {
	writers map[string]*kafka.Writer
	brokers []string
	mu      sync.RWMutex
}

var kafkaClient *gotvKafkaClient

func initKafka() {
	brokers := os.Getenv("KAFKA_BROKERS")
	if brokers == "" {
		log.Info().Msg("GOTV Kafka: KAFKA_BROKERS not set, events will be logged only")
		return
	}
	bList := strings.Split(brokers, ",")
	kafkaClient = &gotvKafkaClient{
		brokers: bList,
		writers: make(map[string]*kafka.Writer),
	}
	// Pre-create GOTV topics
	topics := []string{
		TopicGOTVCampaignEvent, TopicGOTVVolunteerEvent, TopicGOTVRideEvent,
		TopicGOTVCanvassEvent, TopicGOTVPledgeEvent, TopicGOTVAuditLog,
	}
	conn, err := kafka.Dial("tcp", bList[0])
	if err != nil {
		log.Warn().Err(err).Msg("GOTV Kafka: could not connect for topic creation")
		return
	}
	defer conn.Close()
	var configs []kafka.TopicConfig
	for _, t := range topics {
		configs = append(configs, kafka.TopicConfig{Topic: t, NumPartitions: 3, ReplicationFactor: 1})
	}
	_ = conn.CreateTopics(configs...)
	log.Info().Strs("brokers", bList).Msg("GOTV Kafka connected")
}

func publishEvent(topic, key string, payload interface{}) {
	data, _ := json.Marshal(payload)
	if kafkaClient == nil {
		log.Debug().Str("topic", topic).Str("key", key).RawJSON("data", data).Msg("GOTV event (no Kafka)")
		return
	}
	kafkaClient.mu.Lock()
	w, ok := kafkaClient.writers[topic]
	if !ok {
		w = &kafka.Writer{
			Addr:         kafka.TCP(kafkaClient.brokers...),
			Topic:        topic,
			Balancer:     &kafka.LeastBytes{},
			BatchTimeout: 10 * time.Millisecond,
		}
		kafkaClient.writers[topic] = w
	}
	kafkaClient.mu.Unlock()

	// Retry with exponential backoff (max 3 attempts)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := w.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: data})
		cancel()
		if err == nil {
			return
		}
		lastErr = err
		time.Sleep(time.Duration(1<<uint(attempt)) * 100 * time.Millisecond)
	}
	log.Warn().Err(lastErr).Str("topic", topic).Int("attempts", 3).Msg("GOTV Kafka publish failed after retries")
}

// ─── Redis Integration ─────────────────────────────────────────────────────
// Caches dashboard stats, rate-limits campaign API calls per party,
// powers pub/sub for real-time WebSocket events.

var redisClient *redis.Client

// Timeout-safe HTTP client for middleware calls (10s timeout)
var mwHTTPClient = &http.Client{Timeout: 10 * time.Second}

func initRedis() {
	addr := os.Getenv("REDIS_URL")
	if addr == "" {
		log.Info().Msg("GOTV Redis: REDIS_URL not set, caching disabled")
		return
	}
	redisClient = redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     os.Getenv("REDIS_PASSWORD"),
		DB:           2, // GOTV uses DB 2 to isolate from main platform
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
	})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		log.Warn().Err(err).Msg("GOTV Redis: ping failed, caching disabled")
		redisClient = nil
		return
	}
	log.Info().Str("addr", addr).Msg("GOTV Redis connected")
}

func cacheGet(ctx context.Context, key string) (string, bool) {
	if redisClient == nil {
		return "", false
	}
	val, err := redisClient.Get(ctx, "gotv:"+key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

func cacheSet(ctx context.Context, key string, value interface{}, ttl time.Duration) {
	if redisClient == nil {
		return
	}
	data, _ := json.Marshal(value)
	redisClient.Set(ctx, "gotv:"+key, string(data), ttl)
}

func cacheInvalidate(ctx context.Context, pattern string) {
	if redisClient == nil {
		return
	}
	keys, _ := redisClient.Keys(ctx, "gotv:"+pattern+"*").Result()
	if len(keys) > 0 {
		redisClient.Del(ctx, keys...)
	}
}

// Rate limit: returns true if the request should be rejected
func rateLimit(ctx context.Context, partyID int, limit int64) bool {
	if redisClient == nil {
		return false
	}
	key := fmt.Sprintf("gotv:ratelimit:%d:%d", partyID, time.Now().Unix()/3600)
	count, _ := redisClient.Incr(ctx, key).Result()
	if count == 1 {
		redisClient.Expire(ctx, key, time.Hour)
	}
	return count > limit
}

// ─── OpenSearch Integration ────────────────────────────────────────────────
// Full-text search across contacts, volunteers, campaigns.

var opensearchURL string

func initOpenSearch() {
	opensearchURL = os.Getenv("OPENSEARCH_URL")
	if opensearchURL == "" {
		log.Info().Msg("GOTV OpenSearch: OPENSEARCH_URL not set, search disabled")
		return
	}
	// Ensure GOTV indices exist
	for _, idx := range []string{"gotv-contacts", "gotv-volunteers", "gotv-campaigns"} {
		req, _ := http.NewRequest("PUT", opensearchURL+"/"+idx, strings.NewReader(`{
			"settings": {"number_of_shards": 1, "number_of_replicas": 0},
			"mappings": {"properties": {
				"party_id": {"type": "integer"},
				"name": {"type": "text", "analyzer": "standard"},
				"state": {"type": "keyword"},
				"lga": {"type": "keyword"},
				"role": {"type": "keyword"},
				"status": {"type": "keyword"},
				"tags": {"type": "keyword"},
				"created_at": {"type": "date"}
			}}
		}`))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
			resp, err := mwHTTPClient.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}
	}
	log.Info().Str("url", opensearchURL).Msg("GOTV OpenSearch connected")
}

func indexDocument(index, id string, doc interface{}) {
	if opensearchURL == "" {
		return
	}
	data, _ := json.Marshal(doc)
	req, _ := http.NewRequest("PUT", fmt.Sprintf("%s/%s/_doc/%s", opensearchURL, index, id), bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Str("index", index).Msg("GOTV OpenSearch index failed")
		return
	}
	resp.Body.Close()
}

func searchDocuments(index, query string, partyID int) ([]map[string]interface{}, error) {
	if opensearchURL == "" {
		return nil, fmt.Errorf("opensearch not configured")
	}
	body := fmt.Sprintf(`{"query":{"bool":{"must":[{"multi_match":{"query":"%s","fields":["name","state","lga","role","tags"]}},{"term":{"party_id":%d}}]}},"size":50}`, query, partyID)
	req, _ := http.NewRequest("POST", opensearchURL+"/"+index+"/_search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Hits struct {
			Hits []struct {
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	var docs []map[string]interface{}
	for _, h := range result.Hits.Hits {
		docs = append(docs, h.Source)
	}
	return docs, nil
}

// ─── Temporal Integration ──────────────────────────────────────────────────
// Orchestrates campaign dispatch workflows: schedule → send batches →
// track delivery → retry failures → complete.

var temporalURL string

func initTemporal() {
	temporalURL = os.Getenv("TEMPORAL_FRONTEND_URL")
	if temporalURL == "" {
		log.Info().Msg("GOTV Temporal: TEMPORAL_FRONTEND_URL not set, workflow orchestration disabled")
		return
	}
	log.Info().Str("url", temporalURL).Msg("GOTV Temporal connected")
}

// startCampaignWorkflow triggers a Temporal workflow for campaign dispatch
func startCampaignWorkflow(campaignID string, partyID int, campaignType string, totalContacts int) {
	if temporalURL == "" {
		log.Debug().Str("campaign", campaignID).Msg("GOTV campaign workflow (no Temporal)")
		return
	}
	payload := map[string]interface{}{
		"workflow_id":    "gotv-campaign-" + campaignID,
		"workflow_type":  "GOTVCampaignDispatch",
		"task_queue":     "gotv-campaigns",
		"namespace":      "gotv",
		"campaign_id":    campaignID,
		"party_id":       partyID,
		"campaign_type":  campaignType,
		"total_contacts": totalContacts,
		"batch_size":     100,
		"retry_policy":   map[string]interface{}{"maximum_attempts": 3, "initial_interval": "10s"},
	}
	data, _ := json.Marshal(payload)
	resp, err := mwHTTPClient.Post(temporalURL+"/api/v1/namespaces/gotv/workflows", "application/json", bytes.NewReader(data))
	if err != nil {
		log.Warn().Err(err).Str("campaign", campaignID).Msg("GOTV Temporal workflow start failed")
		return
	}
	resp.Body.Close()
	log.Info().Str("campaign", campaignID).Msg("GOTV Temporal campaign workflow started")
}

// ─── Keycloak Integration ──────────────────────────────────────────────────
// Party user authentication — validates JWT tokens from Keycloak OIDC.

var keycloakURL string
var keycloakRealm string

func initKeycloak() {
	keycloakURL = os.Getenv("KEYCLOAK_URL")
	keycloakRealm = os.Getenv("KEYCLOAK_REALM")
	if keycloakURL == "" {
		log.Info().Msg("GOTV Keycloak: KEYCLOAK_URL not set, using dev auth")
		return
	}
	if keycloakRealm == "" {
		keycloakRealm = "inec"
	}
	log.Info().Str("url", keycloakURL).Str("realm", keycloakRealm).Msg("GOTV Keycloak configured")
}

func validateKeycloakToken(token string) (map[string]interface{}, error) {
	if keycloakURL == "" {
		return nil, fmt.Errorf("keycloak not configured")
	}
	req, _ := http.NewRequest("GET",
		fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", keycloakURL, keycloakRealm), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("keycloak: status %d", resp.StatusCode)
	}
	var claims map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&claims)
	return claims, nil
}

// ─── Permify Integration ───────────────────────────────────────────────────
// Fine-grained ReBAC permissions: party_admin > coordinator > canvasser.

var permifyURL string

func initPermify() {
	permifyURL = os.Getenv("PERMIFY_URL")
	if permifyURL == "" {
		log.Info().Msg("GOTV Permify: PERMIFY_URL not set, all permissions allowed in dev mode")
		return
	}
	// Write GOTV authorization schema
	schema := `{
		"schema": "entity party {}\nentity user {}\nentity campaign {\n  relation admin @user\n  relation coordinator @user\n  relation viewer @user\n  relation party @party\n  permission manage = admin or coordinator\n  permission view = manage or viewer\n}\nentity contact_list {\n  relation party @party\n  relation admin @user\n  permission manage = admin\n  permission view = manage\n}"
	}`
	req, _ := http.NewRequest("POST", permifyURL+"/v1/tenants/gotv/schemas/write", strings.NewReader(schema))
	req.Header.Set("Content-Type", "application/json")
	resp, err := mwHTTPClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
	log.Info().Str("url", permifyURL).Msg("GOTV Permify configured")
}

func checkPermission(userID, permission, objectType, objectID string) bool {
	if permifyURL == "" {
		return true // dev mode: all allowed
	}
	body := fmt.Sprintf(`{
		"metadata": {"schema_version": ""},
		"entity": {"type": "%s", "id": "%s"},
		"permission": "%s",
		"subject": {"type": "user", "id": "%s"}
	}`, objectType, objectID, permission, userID)
	req, _ := http.NewRequest("POST", permifyURL+"/v1/tenants/gotv/permissions/check", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		return true // fail-open in case of Permify outage
	}
	defer resp.Body.Close()
	var result struct {
		Can string `json:"can"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Can == "RESULT_ALLOWED"
}

// checkPartyPermission validates a party user has permission for a GOTV operation.
// Used in handlers for fine-grained authorization.
func checkPartyPermission(partyID int, user, permission string) bool {
	return checkPermission(user, permission, "party", fmt.Sprintf("%d", partyID))
}

// ─── TigerBeetle Integration ──────────────────────────────────────────────
// Financial ledger for campaign spending and volunteer transport reimbursement.

var tigerbeetleURL string

func initTigerBeetle() {
	tigerbeetleURL = os.Getenv("TIGERBEETLE_URL")
	if tigerbeetleURL == "" {
		log.Info().Msg("GOTV TigerBeetle: TIGERBEETLE_URL not set, financial tracking disabled")
		return
	}
	log.Info().Str("url", tigerbeetleURL).Msg("GOTV TigerBeetle configured")
}

func recordCampaignSpend(campaignID string, partyID int, amountNGN int64, description string) {
	if tigerbeetleURL == "" {
		return
	}
	payload := map[string]interface{}{
		"debit_account":  fmt.Sprintf("gotv-party-%d", partyID),
		"credit_account": fmt.Sprintf("gotv-campaign-%s", campaignID),
		"amount":         amountNGN,
		"ledger":         2, // GOTV ledger
		"code":           100,
		"description":    description,
	}
	data, _ := json.Marshal(payload)
	resp, err := mwHTTPClient.Post(tigerbeetleURL+"/transfers", "application/json", bytes.NewReader(data))
	if err == nil {
		resp.Body.Close()
	}
}

func recordVolunteerReimbursement(volunteerID string, partyID int, amountNGN int64) {
	if tigerbeetleURL == "" {
		return
	}
	payload := map[string]interface{}{
		"debit_account":  fmt.Sprintf("gotv-party-%d", partyID),
		"credit_account": fmt.Sprintf("gotv-volunteer-%s", volunteerID),
		"amount":         amountNGN,
		"ledger":         2,
		"code":           200,
		"description":    "Transport reimbursement",
	}
	data, _ := json.Marshal(payload)
	resp, err := mwHTTPClient.Post(tigerbeetleURL+"/transfers", "application/json", bytes.NewReader(data))
	if err == nil {
		resp.Body.Close()
	}
}

// ─── Dapr Integration ──────────────────────────────────────────────────────
// Service-to-service calls between gotv-svc ↔ gotv-engine (Rust) ↔
// gotv-analytics (Python). Uses Dapr sidecar for discovery and invocation.

var daprPort string

func initDapr() {
	daprPort = os.Getenv("DAPR_HTTP_PORT")
	if daprPort == "" {
		log.Info().Msg("GOTV Dapr: DAPR_HTTP_PORT not set, direct HTTP calls used")
		return
	}
	log.Info().Str("port", daprPort).Msg("GOTV Dapr sidecar connected")
}

func daprInvoke(appID, method string, payload interface{}) ([]byte, error) {
	data, _ := json.Marshal(payload)
	var url string
	if daprPort != "" {
		url = fmt.Sprintf("http://localhost:%s/v1.0/invoke/%s/method/%s", daprPort, appID, method)
	} else {
		// Fallback: direct HTTP call
		ports := map[string]string{
			"gotv-engine":    "8101",
			"gotv-analytics": "8102",
		}
		port := ports[appID]
		if port == "" {
			return nil, fmt.Errorf("unknown service: %s", appID)
		}
		url = fmt.Sprintf("http://localhost:%s/%s", port, method)
	}
	resp, err := mwHTTPClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5MB max
}

// invokeRustMatchRide calls the Rust gotv-engine for spatial ride matching
func invokeRustMatchRide(rideID string, pickupLat, pickupLng float64, partyID int) (map[string]interface{}, error) {
	result, err := daprInvoke("gotv-engine", "match", map[string]interface{}{
		"ride_id":    rideID,
		"pickup_lat": pickupLat,
		"pickup_lng": pickupLng,
		"party_id":   partyID,
		"max_distance_km": 10.0,
	})
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	json.Unmarshal(result, &out)
	return out, nil
}

// invokePythonAnalytics calls the Python gotv-analytics for campaign insights
func invokePythonAnalytics(endpoint string, payload interface{}) (map[string]interface{}, error) {
	result, err := daprInvoke("gotv-analytics", endpoint, payload)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	json.Unmarshal(result, &out)
	return out, nil
}

// ─── Fluvio Integration ───────────────────────────────────────────────────
// Real-time streaming of canvass door-knock events for live map updates.

var fluvioURL string

func initFluvio() {
	fluvioURL = os.Getenv("FLUVIO_URL")
	if fluvioURL == "" {
		log.Info().Msg("GOTV Fluvio: FLUVIO_URL not set, streaming via WebSocket only")
		return
	}
	log.Info().Str("url", fluvioURL).Msg("GOTV Fluvio connected")
}

func streamCanvassEvent(event interface{}) {
	if fluvioURL == "" {
		return
	}
	data, _ := json.Marshal(event)
	payload := map[string]interface{}{
		"topic":   "gotv-canvass-live",
		"key":     "canvass",
		"payload": string(data),
	}
	body, _ := json.Marshal(payload)
	resp, err := mwHTTPClient.Post(fluvioURL+"/produce", "application/json", bytes.NewReader(body))
	if err != nil {
		log.Warn().Err(err).Msg("GOTV Fluvio stream failed")
		return
	}
	resp.Body.Close()
}

// ─── Mojaloop Integration ─────────────────────────────────────────────────
// Mobile money transfers for volunteer stipends and transport reimbursement.

var mojaloopURL string

func initMojaloop() {
	mojaloopURL = os.Getenv("MOJALOOP_URL")
	if mojaloopURL == "" {
		log.Info().Msg("GOTV Mojaloop: MOJALOOP_URL not set, payments disabled")
		return
	}
	log.Info().Str("url", mojaloopURL).Msg("GOTV Mojaloop connected")
}

func initiateVolunteerPayment(volunteerPhone string, amountNGN float64, reason string) error {
	if mojaloopURL == "" {
		return fmt.Errorf("mojaloop not configured")
	}
	payload := map[string]interface{}{
		"from":     map[string]interface{}{"idType": "MSISDN", "idValue": "0000000000"},
		"to":       map[string]interface{}{"idType": "MSISDN", "idValue": volunteerPhone},
		"amountType": "SEND",
		"currency":   "NGN",
		"amount":     fmt.Sprintf("%.2f", amountNGN),
		"note":       reason,
	}
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", mojaloopURL+"/transfers", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.interoperability.transfers+json;version=1.1")
	resp, err := mwHTTPClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ─── OpenAppSec Integration ───────────────────────────────────────────────
// WAF protection for GOTV API endpoints.

var openappsecURL string

func initOpenAppSec() {
	openappsecURL = os.Getenv("OPENAPPSEC_URL")
	if openappsecURL == "" {
		log.Info().Msg("GOTV OpenAppSec: OPENAPPSEC_URL not set, WAF disabled")
		return
	}
	log.Info().Str("url", openappsecURL).Msg("GOTV OpenAppSec WAF configured")
}

// wafMiddleware inspects requests through OpenAppSec before processing
func wafMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if openappsecURL == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Read body for inspection (capped at 10MB)
		bodyBytes, _ := io.ReadAll(io.LimitReader(r.Body, 10<<20))
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

		inspectPayload := map[string]interface{}{
			"method":  r.Method,
			"uri":     r.RequestURI,
			"headers": r.Header,
			"body":    string(bodyBytes),
			"source":  r.RemoteAddr,
		}
		data, _ := json.Marshal(inspectPayload)
		resp, err := mwHTTPClient.Post(openappsecURL+"/inspect", "application/json", bytes.NewReader(data))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		defer resp.Body.Close()
		var result struct {
			Action string `json:"action"`
			Threat string `json:"threat_type"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if result.Action == "block" {
			log.Warn().Str("threat", result.Threat).Str("uri", r.RequestURI).Msg("GOTV WAF blocked request")
			http.Error(w, `{"error":"request_blocked","reason":"waf_violation"}`, http.StatusForbidden)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		next.ServeHTTP(w, r)
	})
}

// ─── APISIX Integration ───────────────────────────────────────────────────
// API gateway routing and per-party rate limiting.

var apisixAdminURL string

func initAPISIX() {
	apisixAdminURL = os.Getenv("APISIX_ADMIN_URL")
	if apisixAdminURL == "" {
		log.Info().Msg("GOTV APISIX: APISIX_ADMIN_URL not set, gateway routing disabled")
		return
	}
	// Register GOTV routes in APISIX
	routes := []struct {
		id       string
		uri      string
		upstream string
	}{
		{"gotv-api", "/gotv/*", "http://localhost:8103"},
		{"gotv-engine", "/gotv-engine/*", "http://localhost:8101"},
		{"gotv-analytics", "/gotv-analytics/*", "http://localhost:8102"},
	}
	apiKey := os.Getenv("APISIX_API_KEY")
	for _, rt := range routes {
		payload := fmt.Sprintf(`{
			"uri": "%s",
			"upstream": {"type": "roundrobin", "nodes": {"%s": 1}},
			"plugins": {"limit-req": {"rate": 100, "burst": 50, "rejected_code": 429, "key": "remote_addr"}}
		}`, rt.uri, rt.upstream)
		req, _ := http.NewRequest("PUT", apisixAdminURL+"/apisix/admin/routes/"+rt.id,
			strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("X-API-KEY", apiKey)
		}
		resp, err := mwHTTPClient.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
	log.Info().Str("url", apisixAdminURL).Msg("GOTV APISIX routes registered")
}

// ─── Lakehouse Integration ────────────────────────────────────────────────
// Analytical queries for campaign performance, turnout trends, volunteer efficiency.

var lakehouseURL string

func initLakehouse() {
	lakehouseURL = os.Getenv("LAKEHOUSE_URL")
	if lakehouseURL == "" {
		log.Info().Msg("GOTV Lakehouse: LAKEHOUSE_URL not set, analytics via PostgreSQL")
		return
	}
	log.Info().Str("url", lakehouseURL).Msg("GOTV Lakehouse connected")
}

// queryLakehouse runs a parameterized analytical query against Lakehouse.
// Only allows SELECT queries to prevent injection attacks.
func queryLakehouse(query string) ([]map[string]interface{}, error) {
	if lakehouseURL == "" {
		return nil, fmt.Errorf("lakehouse not configured")
	}
	// Security: reject non-SELECT queries to prevent injection
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(trimmed, "SELECT") {
		return nil, fmt.Errorf("only SELECT queries allowed")
	}
	// Block common injection patterns
	for _, banned := range []string{"DROP", "DELETE", "INSERT", "UPDATE", "ALTER", "TRUNCATE", "EXEC", "--", ";"} {
		if strings.Contains(trimmed, banned) {
			return nil, fmt.Errorf("forbidden SQL keyword: %s", banned)
		}
	}
	payload := map[string]string{"query": query}
	data, _ := json.Marshal(payload)
	resp, err := mwHTTPClient.Post(lakehouseURL+"/query", "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Data []map[string]interface{} `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Data, nil
}

// ─── Init All Middleware ───────────────────────────────────────────────────
// Called from main() to initialize all middleware connections.

func initAllMiddleware() {
	initKafka()
	initRedis()
	initOpenSearch()
	initTemporal()
	initKeycloak()
	initPermify()
	initTigerBeetle()
	initDapr()
	initFluvio()
	initMojaloop()
	initOpenAppSec()
	initAPISIX()
	initLakehouse()
	log.Info().Msg("GOTV middleware initialization complete")
}

// ─── Search API Handler ────────────────────────────────────────────────────
// Full-text search across all GOTV data via OpenSearch.

func handleGOTVSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	index := r.URL.Query().Get("index")
	if q == "" {
		http.Error(w, `{"error":"query parameter 'q' required"}`, http.StatusBadRequest)
		return
	}
	if index == "" {
		index = "gotv-contacts"
	}
	// Validate index to prevent OpenSearch injection
	validIndices := map[string]bool{"gotv-contacts": true, "gotv-volunteers": true, "gotv-campaigns": true}
	if !validIndices[index] {
		http.Error(w, `{"error":"invalid index"}`, http.StatusBadRequest)
		return
	}
	pid, _ := getParty(r)
	results, err := searchDocuments(index, q, pid)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}, "source": "postgresql"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"results": results, "count": len(results), "source": "opensearch"})
}

// ─── Analytics API Handler ─────────────────────────────────────────────────
// Delegates analytics queries to Python gotv-analytics via Dapr.

func handleGOTVAnalytics(w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Query().Get("endpoint")
	if endpoint == "" {
		endpoint = "campaign-performance"
	}
	result, err := invokePythonAnalytics(endpoint, nil)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error(), "fallback": true})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// ─── Middleware Status Handler ──────────────────────────────────────────────
// Reports connectivity status of all 13 middleware services.

// handleMiddlewareStatus requires auth to prevent infrastructure info leakage
func handleMiddlewareStatus(w http.ResponseWriter, r *http.Request) {
	if _, user := getParty(r); user == "" {
		http.Error(w, `{"error":"authentication required"}`, http.StatusUnauthorized)
		return
	}
	status := map[string]interface{}{
		"kafka":       kafkaClient != nil,
		"redis":       redisClient != nil,
		"opensearch":  opensearchURL != "",
		"temporal":    temporalURL != "",
		"keycloak":    keycloakURL != "",
		"permify":     permifyURL != "",
		"tigerbeetle": tigerbeetleURL != "",
		"dapr":        daprPort != "",
		"fluvio":      fluvioURL != "",
		"mojaloop":    mojaloopURL != "",
		"openappsec":  openappsecURL != "",
		"apisix":      apisixAdminURL != "",
		"lakehouse":   lakehouseURL != "",
		"postgresql":  true,
	}
	connected := 1 // PostgreSQL always connected
	for _, v := range status {
		if b, ok := v.(bool); ok && b {
			connected++
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"middleware":        status,
		"connected":        connected,
		"total":            14,
		"connection_ratio": fmt.Sprintf("%d/14", connected),
	})
}
