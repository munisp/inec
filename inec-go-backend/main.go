package main

import (
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db        *sql.DB
	wsClients = struct {
		sync.RWMutex
		conns map[*websocket.Conn]bool
	}{conns: make(map[*websocket.Conn]bool)}
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	rateLimiter = newRateLimiter()
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/app.db"
	}
	if _, err := os.Stat("/data"); os.IsNotExist(err) && strings.HasPrefix(dbPath, "/data") {
		dbPath = "app.db"
	}

	var err error
	db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON&cache=shared&_busy_timeout=5000")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	defer db.Close()

	initDB(db)
	seedDatabase(db)
	seedBVASDevices(db)
	initAIProxy()

	mwHub = initMiddlewareHub()

	r := mux.NewRouter()

	// Health
	r.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, M{"status": "ok"})
	}).Methods("GET")
	r.HandleFunc("/readiness", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, M{"ready": true})
	}).Methods("GET")

	// Auth
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")
	r.HandleFunc("/auth/me", handleMe).Methods("GET")

	// Elections
	r.HandleFunc("/elections", handleListElections).Methods("GET")
	r.HandleFunc("/elections/{id:[0-9]+}", handleGetElection).Methods("GET")
	r.HandleFunc("/elections", handleCreateElection).Methods("POST")
	r.HandleFunc("/elections/{id:[0-9]+}", handleUpdateElection).Methods("PATCH")
	r.HandleFunc("/elections/{id:[0-9]+}/stats", handleElectionStats).Methods("GET")

	// Results
	r.HandleFunc("/results/ws/updates", handleWSUpdates)
	r.HandleFunc("/results/submit", handleSubmitResult).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/validate", handleValidateResult).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/finalize", handleFinalizeResult).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/dispute", handleDisputeResult).Methods("POST")
	r.HandleFunc("/results", handleListResults).Methods("GET")
	r.HandleFunc("/results/{id:[0-9]+}", handleGetResult).Methods("GET")

	// Geo
	r.HandleFunc("/geo/states", handleListStates).Methods("GET")
	r.HandleFunc("/geo/states/{code}", handleGetState).Methods("GET")
	r.HandleFunc("/geo/lgas", handleListLGAs).Methods("GET")
	r.HandleFunc("/geo/wards", handleListWards).Methods("GET")
	r.HandleFunc("/geo/polling-units", handleListPollingUnits).Methods("GET")
	r.HandleFunc("/geo/polling-units/{code}", handleGetPollingUnit).Methods("GET")
	r.HandleFunc("/geo/map-data", handleMapData).Methods("GET")
	r.HandleFunc("/geo/tiles/pus/{z:[0-9]+}/{x:[0-9]+}/{y:[0-9]+}.mvt", handlePUTile).Methods("GET")
	r.HandleFunc("/geo/reports/polling-units.csv", handleExportCSV).Methods("GET")
	r.HandleFunc("/geo/reports/polling-units.geojson", handleExportGeoJSON).Methods("GET")

	// Dashboard
	r.HandleFunc("/dashboard/stats", handleDashboardStats).Methods("GET")
	r.HandleFunc("/dashboard/live-feed", handleLiveFeed).Methods("GET")
	r.HandleFunc("/dashboard/collation", handleCollation).Methods("GET")
	r.HandleFunc("/dashboard/metrics/client", handlePostClientMetric).Methods("POST")
	r.HandleFunc("/dashboard/metrics/client/recent", handleRecentClientMetrics).Methods("GET")

	// Audit
	r.HandleFunc("/audit/trail", handleAuditTrail).Methods("GET")
	r.HandleFunc("/audit/verify/{id:[0-9]+}", handleVerifyResult).Methods("GET")
	r.HandleFunc("/audit/stats", handleAuditStats).Methods("GET")

	// Incidents
	r.HandleFunc("/incidents", handleCreateIncident).Methods("POST")
	r.HandleFunc("/incidents", handleListIncidents).Methods("GET")
	r.HandleFunc("/incidents/{id:[0-9]+}", handleUpdateIncident).Methods("PATCH")

	// Parties
	r.HandleFunc("/parties", handleListParties).Methods("GET")

	// BVAS
	r.HandleFunc("/bvas/devices", handleListBVASDevices).Methods("GET")
	r.HandleFunc("/bvas/devices/{id}", handleGetBVASDevice).Methods("GET")
	r.HandleFunc("/bvas/devices", handleRegisterBVASDevice).Methods("POST")
	r.HandleFunc("/bvas/devices/{id}", handleUpdateBVASDevice).Methods("PATCH")
	r.HandleFunc("/bvas/accreditation", handleBVASAccreditation).Methods("POST")
	r.HandleFunc("/bvas/accreditation/feed", handleBVASAccreditationFeed).Methods("GET")
	r.HandleFunc("/bvas/accreditation/timeline", handleBVASAccreditationTimeline).Methods("GET")
	r.HandleFunc("/bvas/reconciliation", handleBVASReconciliation).Methods("GET")
	r.HandleFunc("/bvas/summary", handleBVASSummary).Methods("GET")

	// Ingestion Engine
	r.HandleFunc("/ingestion/submit", handleIngestionSubmit).Methods("POST")
	r.HandleFunc("/ingestion/batch", handleBatchUpload).Methods("POST")
	r.HandleFunc("/ingestion/offline-sync", handleOfflineSync).Methods("POST")
	r.HandleFunc("/ingestion/stats", handleIngestionStats).Methods("GET")
	r.HandleFunc("/ingestion/jobs", handleIngestionJobs).Methods("GET")
	r.HandleFunc("/ingestion/dead-letter", handleDeadLetterQueue).Methods("GET")
	r.HandleFunc("/ingestion/dead-letter/{id}/reprocess", handleReprocessDLQ).Methods("POST")
	r.HandleFunc("/ingestion/offline-queue", handleOfflineSyncQueue).Methods("GET")

	// SMS/USSD Gateway
	r.HandleFunc("/sms/verify", handleSMSVerify).Methods("POST")
	r.HandleFunc("/sms/stats", handleSMSStats).Methods("GET")
	r.HandleFunc("/ussd/gateway", handleUSSDGateway).Methods("POST")

	// AI Analytics (proxy to Python service)
	r.HandleFunc("/ai/anomalies", handleAIAnomalies).Methods("GET")
	r.HandleFunc("/ai/benford", handleAIBenford).Methods("GET")
	r.HandleFunc("/ai/integrity", handleAIIntegrity).Methods("GET")
	r.HandleFunc("/ai/methods", handleAIMethods).Methods("GET")

	// Public API v1 (API key authenticated)
	r.HandleFunc("/api/v1/docs", handlePublicAPIDocs).Methods("GET")
	r.HandleFunc("/api/v1/docs.json", handlePublicAPIDocs).Methods("GET")
	r.HandleFunc("/api/v1/keys", handlePublicAPIKeys).Methods("GET", "POST")
	r.HandleFunc("/api/v1/usage", handlePublicAPIUsage).Methods("GET")
	r.HandleFunc("/api/v1/elections", apiKeyAuth(handlePublicAPIElections)).Methods("GET")
	r.HandleFunc("/api/v1/results", apiKeyAuth(handlePublicAPIResults)).Methods("GET")
	r.HandleFunc("/api/v1/results/{id:[0-9]+}", apiKeyAuth(handlePublicAPIResultDetail)).Methods("GET")
	r.HandleFunc("/api/v1/states", apiKeyAuth(handlePublicAPIStates)).Methods("GET")
	r.HandleFunc("/api/v1/polling-units", apiKeyAuth(handlePublicAPIPollingUnits)).Methods("GET")
	r.HandleFunc("/api/v1/collation", apiKeyAuth(handlePublicAPICollation)).Methods("GET")
	r.HandleFunc("/api/v1/ai/anomalies", apiKeyAuth(handleAIAnomalies)).Methods("GET")
	r.HandleFunc("/api/v1/ai/integrity", apiKeyAuth(handleAIIntegrity)).Methods("GET")

	// Middleware status & management
	r.HandleFunc("/middleware/status", handleMiddlewareStatus).Methods("GET")
	r.HandleFunc("/middleware/health", handleMiddlewareHealth).Methods("GET")
	r.HandleFunc("/middleware/kafka/topics", handleKafkaTopics).Methods("GET")
	r.HandleFunc("/middleware/temporal/workflows", handleTemporalWorkflows).Methods("GET")
	r.HandleFunc("/middleware/temporal/workflows/{id}", handleTemporalWorkflowStatus).Methods("GET")
	r.HandleFunc("/middleware/tigerbeetle/accounts", handleTBAccounts).Methods("GET")
	r.HandleFunc("/middleware/tigerbeetle/transfers", handleTBTransfers).Methods("GET")
	r.HandleFunc("/middleware/apisix/routes", handleAPISIXRoutes).Methods("GET")
	r.HandleFunc("/middleware/apisix/config", handleAPISIXConfig).Methods("GET")
	r.HandleFunc("/middleware/permify/check", handlePermifyCheck).Methods("POST")
	r.HandleFunc("/middleware/fluvio/topics", handleFluvioTopics).Methods("GET")
	r.HandleFunc("/middleware/fluvio/consume/{topic}", handleFluvioConsume).Methods("GET")
	r.HandleFunc("/middleware/lakehouse/analytics/{election_id}/{type}", handleLakehouseAnalytics).Methods("GET")
	r.HandleFunc("/middleware/lakehouse/tables", handleLakehouseTables).Methods("GET")
	r.HandleFunc("/middleware/redis/stats", handleRedisStats).Methods("GET")

	handler := corsMiddleware(securityHeaders(rateLimitMiddleware(gzipMiddleware(r))))

	addr := ":8088"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	fmt.Println("INEC Go Backend listening on", addr)
	log.Fatal(http.ListenAndServe(addr, handler))
}

type M map[string]interface{}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, detail string) {
	writeJSON(w, code, M{"detail": detail})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer-when-downgrade")
		w.Header().Set("Permissions-Policy", "geolocation=(), camera=()")
		next.ServeHTTP(w, r)
	})
}

type rateLimiterStore struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newRateLimiter() *rateLimiterStore {
	return &rateLimiterStore{entries: make(map[string][]time.Time)}
}

func (rl *rateLimiterStore) allow(key string, limit int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	arr := rl.entries[key]
	filtered := arr[:0]
	for _, t := range arr {
		if now.Sub(t) < window {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= limit {
		rl.entries[key] = filtered
		return false
	}
	rl.entries[key] = append(filtered, now)
	return true
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	limits := []struct {
		prefix string
		limit  int
	}{
		{"/geo/tiles", 60},
		{"/dashboard/metrics", 10},
		{"/results", 20},
		{"/geo/reports", 5},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		for _, l := range limits {
			if strings.HasPrefix(r.URL.Path, l.prefix) {
				if !rateLimiter.allow(ip+":"+l.prefix, l.limit, time.Second) {
					writeError(w, 429, "rate_limited")
					return
				}
				break
			}
		}
		next.ServeHTTP(w, r)
	})
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz, _ := gzip.NewWriterLevel(w, gzip.DefaultCompression)
		defer gz.Close()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		next.ServeHTTP(gzipResponseWriter{Writer: gz, ResponseWriter: w}, r)
	})
}

func broadcastWS(msg M) {
	data, _ := json.Marshal(msg)
	wsClients.RLock()
	defer wsClients.RUnlock()
	for conn := range wsClients.conns {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
}

func handleWSUpdates(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	wsClients.Lock()
	wsClients.conns[conn] = true
	wsClients.Unlock()
	defer func() {
		wsClients.Lock()
		delete(wsClients.conns, conn)
		wsClients.Unlock()
		conn.Close()
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func queryParam(r *http.Request, key string, def string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	return v
}

func queryParamInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	var i int
	fmt.Sscanf(v, "%d", &i)
	return i
}
