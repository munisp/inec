package main

import (
	"go.opentelemetry.io/otel"

	"compress/gzip"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"
)

var (
	db        *sql.DB
	wsClients = struct {
		sync.RWMutex
		conns map[*websocket.Conn]bool
	}{conns: make(map[*websocket.Conn]bool)}
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // same-origin (no Origin header)
			}
			allowed := os.Getenv("ALLOWED_ORIGINS")
			if allowed == "" {
				allowed = os.Getenv("CORS_ORIGINS")
			}
			if allowed == "*" {
				return true // explicitly allow all (dev mode)
			}
			if allowed == "" {
				// Default: allow localhost in dev, reject unknown in prod
				return strings.HasPrefix(origin, "http://localhost") ||
					strings.HasPrefix(origin, "https://localhost")
			}
			for _, a := range strings.Split(allowed, ",") {
				if strings.TrimSpace(a) == origin {
					return true
				}
			}
			log.Warn().Str("origin", origin).Msg("WebSocket connection rejected: origin not in allow-list")
			return false
		},
	}
	rateLimiter = newRateLimiter()
)

func main() {
	// Initialize OpenTelemetry Tracing
	_ = otel.Tracer("inec-backend")

	// Initialize structured logging
	initLogger()
	// Initialize input validation
	initValidator()
	// Initialize Prometheus metrics
	initMetrics()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		if os.Getenv("INEC_ENV") == "development" || os.Getenv("INEC_ENV") == "" {
			dsn = "postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable"
			log.Warn().Msg("DATABASE_URL not set — using local dev default")
		} else {
			log.Fatal().Msg("DATABASE_URL environment variable is required in production")
		}
	}

	db = openDatabase(dsn)
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("Database connection failed")
	}
	log.Info().Msg("Database connected")

	initScaledDB(db)
	initPgBouncerAwarePooling(db)
	initPgpool()
	go periodicPoolStats()
	initRingBufferQueue()
	initShardedWSHub()
	initCollationCache()
	go trackIngestionThroughput()

	// Initialize schema first (CREATE TABLE IF NOT EXISTS), then run incremental migrations
	initDB(db)
	initGORM(os.Getenv("DATABASE_URL"))

	if err := runMigrations(db); err != nil {
		log.Warn().Err(err).Msg("Migration runner encountered issues (non-fatal — initDB already created schema)")
	}

	seedDatabase(db)
	seedBVASDevices(db)
	initEMSTables(db)
	seedEMSData(db)
	initPhase7Tables(db)
	seedPhase7Data(db)
	initBiometricEngine(db)
	seedBiometricEngine(db)
	initBiometricAdvanced(db)
	seedBiometricAdvanced(db)
	initAIProxy()
	initBlockchainProduction(db)
	initProductionUpgrades(db)
	initMiddlewareTables(db)

	// Security infrastructure
	initTokenBlacklist(db)
	initActiveSessions(db)
	initAPIKeyRotation(db)
	initTracing()
	initObserverTables()
	initDocumentAISchema()
	initKYBSchema()
	initDataSecuritySchema()
	initElectionFSMSchema()
	initWebhookSchema()
	initDisputeSchema()
	initPushNotificationSchema()
	initPlatformEnhancements(db)
	initPlatformImprovements(db)
	initComplianceTables()
	initGOTVTables()
	initGOTVEncryption()
	initMFA()
	seedComprehensive(db)
	seedAllTables(db)
	runGeoMigrations()
	runGeoAdvancedMigrations()
	initSchemaCompatibility(db)
	initOpenAPIRoutes()

	// Production compliance, stablecoin, and USSD engines
	initComplianceEngine()
	initStablecoinEngine()
	initUSSDEngine()

	mwHub = initMiddlewareHub()
	wsHub = newWebSocketHub()
	go wsHub.run()
	initDistributedRateLimiter()
	detectMiddlewareModes()

	// Initialize service-oriented architecture layer (circuit breakers, event bus, tracing)
	initArchitecture()

	// Seed search indices after hub is ready
	go seedSearchIndices(db)
	// Start background cache cleanup
	go cleanupExpiredCache()

	r := mux.NewRouter()

	// Health — deep checks
	r.HandleFunc("/healthz", handleDeepHealthCheck).Methods("GET")
	r.HandleFunc("/readiness", handleReadinessCheck).Methods("GET")
	// Architecture health — circuit breakers, event bus, service registry
	r.HandleFunc("/architecture/health", adminOnly(handleArchitectureHealth)).Methods("GET")
	r.HandleFunc("/architecture/circuit-breakers", adminOnly(handleCircuitBreakers)).Methods("GET")
	r.HandleFunc("/db/metrics", adminOnly(handleDBMetrics)).Methods("GET")
	r.HandleFunc("/db/pool", adminOnly(handleDBPoolStats)).Methods("GET")
	r.HandleFunc("/scale/health", readAuth(handleScaleHealth)).Methods("GET")
	r.HandleFunc("/middleware/modes", readAuth(handleMiddlewareModes)).Methods("GET")

	// Auth
	r.HandleFunc("/auth/login", handleLogin).Methods("POST")
	r.HandleFunc("/auth/register", handleRegister).Methods("POST")
	r.HandleFunc("/auth/refresh", handleRefreshToken).Methods("POST")
	r.HandleFunc("/auth/me", readAuth(handleMe)).Methods("GET")
	r.HandleFunc("/auth/logout", writeAuth(handleLogout)).Methods("POST")
	r.HandleFunc("/auth/sessions", readAuth(handleListSessions)).Methods("GET")
	r.HandleFunc("/auth/sessions/revoke", writeAuth(handleRevokeSession)).Methods("POST")
	r.HandleFunc("/auth/sessions/revoke-all", writeAuth(handleRevokeAllSessions)).Methods("POST")
	r.HandleFunc("/auth/api-keys/rotate", adminOnly(handleRotateAPIKey)).Methods("POST")
	r.HandleFunc("/auth/csrf-token", handleCSRFToken).Methods("GET")

	// Geo-fencing
	r.HandleFunc("/geofence/check", writeAuth(handleGeofenceCheck)).Methods("POST")
	r.HandleFunc("/geofence/stats/{election_id}", readAuth(handleGeofenceStats)).Methods("GET")

	// Elections — read auth for lists, write auth for mutations
	r.HandleFunc("/elections", readAuth(handleListElections)).Methods("GET")
	r.HandleFunc("/elections/{id:[0-9]+}", readAuth(handleGetElection)).Methods("GET")
	r.HandleFunc("/elections", writeAuth(handleCreateElection)).Methods("POST")
	r.HandleFunc("/elections/{id:[0-9]+}", writeAuth(handleUpdateElection)).Methods("PATCH")
	r.HandleFunc("/elections/{id:[0-9]+}/stats", readAuth(handleElectionStats)).Methods("GET")

	// Results
	r.HandleFunc("/results/ws/updates", handleWSUpdates)
	r.HandleFunc("/results/ws/updates/sharded", handleWSUpdatesSharded)
	r.HandleFunc("/results/submit", writeAuth(handleSubmitResult)).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/validate", writeAuth(handleValidateResult)).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/finalize", adminOnly(handleFinalizeResult)).Methods("POST")
	r.HandleFunc("/results/{id:[0-9]+}/dispute", writeAuth(handleDisputeResult)).Methods("POST")
	r.HandleFunc("/results", readAuth(handleListResults)).Methods("GET")
	r.HandleFunc("/results/{id:[0-9]+}", readAuth(handleGetResult)).Methods("GET")

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

	// Enhanced Geospatial (PostGIS, Landmarks, Heatmaps, Street View)
	r.HandleFunc("/geo/nearby-pus", readAuth(handleNearbyPUs)).Methods("GET")
	r.HandleFunc("/geo/landmarks", readAuth(handleLandmarks)).Methods("GET")
	r.HandleFunc("/geo/landmarks", writeAuth(handleCreateLandmark)).Methods("POST")
	r.HandleFunc("/geo/landmarks/seed", writeAuth(handleSeedLandmarks)).Methods("POST")
	r.HandleFunc("/geo/heatmap", readAuth(handleGeoHeatmap)).Methods("GET")
	r.HandleFunc("/geo/clusters", readAuth(handleGeoCluster)).Methods("GET")
	r.HandleFunc("/geo/street-view", readAuth(handleStreetViewProxy)).Methods("GET")
	r.HandleFunc("/geo/boundary", readAuth(handleGeoBoundary)).Methods("GET")
	r.HandleFunc("/geo/live-stream", readAuth(handleGeoLiveStream)).Methods("GET")
	r.HandleFunc("/geo/spatial-stats", readAuth(handleGeoSpatialStats)).Methods("GET")
	r.HandleFunc("/geo/sedona/analysis", readAuth(handleSedonaAnalysis)).Methods("GET")
	// Real-time tracking & crowd density
	r.HandleFunc("/geo/tracking/update", writeAuth(handleOfficialLocationUpdate)).Methods("POST")
	r.HandleFunc("/geo/tracking/officials", readAuth(handleGetOfficialLocations)).Methods("GET")
	r.HandleFunc("/geo/tracking/stream", readAuth(handleLiveTrackingStream)).Methods("GET")
	r.HandleFunc("/geo/crowd/report", writeAuth(handleReportCrowdDensity)).Methods("POST")
	r.HandleFunc("/geo/crowd/density", readAuth(handleGetCrowdDensity)).Methods("GET")
	r.HandleFunc("/geo/tracking/seed", writeAuth(handleSeedTrackingData)).Methods("POST")

	// Advanced Geospatial (#2-#30): tracking history, geofence viz, PostGIS spatial, crowd alerts,
	// routing, weather, photos, incident hotspots, predictive crowd, drones, simulation,
	// blockchain attestation, mesh network, H3 hex grid, offline tiles, MVT tiles
	registerGeoAdvancedRoutes(r)
	registerComplianceRoutes(r)
	registerGOTVRoutes(r)

	// GeoLibre GIS integration — GeoJSON endpoints, spatial queries, project export
	registerGeoLibreRoutes(r)

	// Dashboard — read auth for data, write auth for metrics
	r.HandleFunc("/dashboard/stats", readAuth(handleDashboardStats)).Methods("GET")
	r.HandleFunc("/dashboard/live-feed", readAuth(handleLiveFeed)).Methods("GET")
	r.HandleFunc("/dashboard/collation", readAuth(handleCollation)).Methods("GET")
	r.HandleFunc("/dashboard/metrics/client", readAuth(handlePostClientMetric)).Methods("POST")
	r.HandleFunc("/dashboard/metrics/client/recent", readAuth(handleRecentClientMetrics)).Methods("GET")

	// Audit — read auth for viewing
	r.HandleFunc("/audit/trail", readAuth(handleAuditTrail)).Methods("GET")
	r.HandleFunc("/audit/verify/{id:[0-9]+}", readAuth(handleVerifyResult)).Methods("GET")
	r.HandleFunc("/audit/stats", readAuth(handleAuditStats)).Methods("GET")

	// Incidents — write auth for create/update, read for listing
	r.HandleFunc("/incidents", writeAuth(handleCreateIncident)).Methods("POST")
	r.HandleFunc("/incidents", readAuth(handleListIncidents)).Methods("GET")
	r.HandleFunc("/incidents/{id:[0-9]+}", writeAuth(handleUpdateIncident)).Methods("PATCH")

	// Parties
	r.HandleFunc("/parties", readAuth(handleListParties)).Methods("GET")

	// BVAS — auth required
	r.HandleFunc("/bvas/devices", readAuth(handleListBVASDevices)).Methods("GET")
	r.HandleFunc("/bvas/devices/{id}", readAuth(handleGetBVASDevice)).Methods("GET")
	r.HandleFunc("/bvas/devices", writeAuth(handleRegisterBVASDevice)).Methods("POST")
	r.HandleFunc("/bvas/devices/{id}", writeAuth(handleUpdateBVASDevice)).Methods("PATCH")
	r.HandleFunc("/bvas/accreditation", writeAuth(handleBVASAccreditation)).Methods("POST")
	r.HandleFunc("/bvas/accreditation/feed", readAuth(handleBVASAccreditationFeed)).Methods("GET")
	r.HandleFunc("/bvas/accreditation/timeline", readAuth(handleBVASAccreditationTimeline)).Methods("GET")
	r.HandleFunc("/bvas/reconciliation", readAuth(handleBVASReconciliation)).Methods("GET")
	r.HandleFunc("/bvas/summary", readAuth(handleBVASSummary)).Methods("GET")

	// Ingestion Engine — auth required
	r.HandleFunc("/ingestion/submit", writeAuth(handleIngestionSubmit)).Methods("POST")
	r.HandleFunc("/ingestion/batch", writeAuth(handleBatchUpload)).Methods("POST")
	r.HandleFunc("/ingestion/offline-sync", writeAuth(handleOfflineSync)).Methods("POST")
	r.HandleFunc("/ingestion/stats", readAuth(handleIngestionStats)).Methods("GET")
	r.HandleFunc("/ingestion/jobs", readAuth(handleIngestionJobs)).Methods("GET")
	r.HandleFunc("/ingestion/dead-letter", readAuth(handleDeadLetterQueue)).Methods("GET")
	r.HandleFunc("/ingestion/dead-letter/{id}/reprocess", adminOnly(handleReprocessDLQ)).Methods("POST")
	r.HandleFunc("/ingestion/offline-queue", readAuth(handleOfflineSyncQueue)).Methods("GET")

	// Observer Monitoring — party agents, real-time streaming, photo uploads
	r.HandleFunc("/observer/stream", handleSSEStream).Methods("GET")
	r.HandleFunc("/observer/reports", adminOrOfficer(handleObserverPhotoUpload)).Methods("POST")
	r.HandleFunc("/observer/reports", readAuth(handleListObserverReports)).Methods("GET")
	r.HandleFunc("/observer/reports/{id:[0-9]+}/review", adminOnly(handleReviewObserverReport)).Methods("PATCH")
	r.HandleFunc("/observer/alerts", adminOrOfficer(handleCreateAlertRule)).Methods("POST")
	r.HandleFunc("/observer/alerts", readAuth(handleListAlertRules)).Methods("GET")
	r.HandleFunc("/observer/alerts/{id:[0-9]+}", adminOrOfficer(handleDeleteAlertRule)).Methods("DELETE")
	r.HandleFunc("/observer/check-in", adminOrOfficer(handleObserverCheckIn)).Methods("POST")
	r.HandleFunc("/observer/stats", readAuth(handleObserverStats)).Methods("GET")
	r.HandleFunc("/observer/party-dashboard", readAuth(handlePartyDashboard)).Methods("GET")
	r.HandleFunc("/observer/video", adminOrOfficer(handleUploadVideo)).Methods("POST")

	// Document AI — PaddleOCR, VLM, DocLing analysis
	r.HandleFunc("/document-ai/analyze", adminOnly(handleAnalyzePhoto)).Methods("POST")
	r.HandleFunc("/document-ai/status", readAuth(handleDocumentAnalysisStatus)).Methods("GET")

	// KYC & Liveness — identity verification for platform users
	r.HandleFunc("/kyc/verify", adminOnly(handleKYCVerify)).Methods("POST")
	r.HandleFunc("/kyc/liveness", readAuth(handleLivenessCheck)).Methods("POST")
	r.HandleFunc("/kyc/status", readAuth(handleKYCStatus)).Methods("GET")
	r.HandleFunc("/kyc/events", readAuth(handleKYCEvents)).Methods("GET")
	r.HandleFunc("/kyc/triggers", readAuth(handleKYCTriggerCheck)).Methods("GET")

	// KYB — business/entity verification
	r.HandleFunc("/kyb/verify", adminOnly(handleKYBVerify)).Methods("POST")
	r.HandleFunc("/kyb/status", readAuth(handleKYBStatus)).Methods("GET")

	// NIN/NIMC — national identity verification
	r.HandleFunc("/compliance/nin/verify", adminOnly(handleNINVerify)).Methods("POST")
	r.HandleFunc("/compliance/bvn/verify", adminOnly(handleBVNVerify)).Methods("POST")
	r.HandleFunc("/compliance/cac/verify", adminOnly(handleCACVerify)).Methods("POST")
	r.HandleFunc("/compliance/sanctions/screen", adminOnly(handleSanctionsScreen)).Methods("POST")
	r.HandleFunc("/compliance/dashboard", adminOnly(handleComplianceDashboard)).Methods("GET")

	// Stablecoin / CBDC / eNaira — election fund management
	r.HandleFunc("/stablecoin/wallets", adminOnly(handleListWallets)).Methods("GET")
	r.HandleFunc("/stablecoin/wallets/create", adminOnly(handleCreateWallet)).Methods("POST")
	r.HandleFunc("/stablecoin/wallets/get", adminOnly(handleGetWallet)).Methods("GET")
	r.HandleFunc("/stablecoin/transfer", adminOnly(handleTransfer)).Methods("POST")
	r.HandleFunc("/stablecoin/transactions", adminOnly(handleWalletTransactions)).Methods("GET")
	r.HandleFunc("/stablecoin/dashboard", adminOnly(handleStablecoinDashboard)).Methods("GET")

	// Data Security — encryption status, classification, audit events
	r.HandleFunc("/security/data-status", adminOnly(handleDataSecurityStatus)).Methods("GET")
	r.HandleFunc("/security/data-classification", adminOnly(handleDataClassificationList)).Methods("GET")
	r.HandleFunc("/security/events", adminOnly(handleSecurityEvents)).Methods("GET")

	// SMS/USSD Gateway — auth required
	r.HandleFunc("/sms/verify", authRequired(handleSMSVerify)).Methods("POST")
	r.HandleFunc("/sms/stats", readAuth(handleSMSStats)).Methods("GET")
	r.HandleFunc("/ussd/gateway", authRequired(handleUSSDGateway)).Methods("POST")
	r.HandleFunc("/ussd/session", authRequired(handleUSSDSession)).Methods("POST")
	r.HandleFunc("/ussd/dashboard", adminOnly(handleUSSDDashboard)).Methods("GET")

	// AI Analytics (proxy to Python service) — auth required
	r.HandleFunc("/ai/anomalies", readAuth(handleAIAnomalies)).Methods("GET")
	r.HandleFunc("/ai/benford", readAuth(handleAIBenford)).Methods("GET")
	r.HandleFunc("/ai/integrity", readAuth(handleAIIntegrity)).Methods("GET")
	r.HandleFunc("/ai/methods", readAuth(handleAIMethods)).Methods("GET")
	r.HandleFunc("/ai/gnn/score", readAuth(handleGNNScore)).Methods("GET")
	r.HandleFunc("/ai/proxy/anomalies", readAuth(handleAIProxy)).Methods("GET")
	r.HandleFunc("/ai/fallback/anomalies", readAuth(handleAIFallbackAnomalies)).Methods("GET")

	// Lakehouse Pipeline (Bronze→Silver→Gold)
	r.HandleFunc("/ai/lakehouse/ingest", writeAuth(handleLakehouseIngest)).Methods("POST")
	r.HandleFunc("/ai/lakehouse/pipeline", writeAuth(handleLakehousePipeline)).Methods("POST")
	r.HandleFunc("/ai/lakehouse/status", readAuth(handleLakehouseStatus)).Methods("GET")

	// Ray Distributed Compute
	r.HandleFunc("/ai/ray/batch-predict", readAuth(handleRayBatchPredict)).Methods("POST")
	r.HandleFunc("/ai/ray/train", writeAuth(handleRayTrain)).Methods("POST")

	// Continuous Training Pipeline
	r.HandleFunc("/ai/training/status", readAuth(handleTrainingStatus)).Methods("GET")
	r.HandleFunc("/ai/training/check-drift", readAuth(handleCheckDrift)).Methods("GET")
	r.HandleFunc("/ai/training/retrain", writeAuth(handleTriggerRetrain)).Methods("POST")
	r.HandleFunc("/ai/registry/models", readAuth(handleModelRegistry)).Methods("GET")

	// Public API v1 (API key authenticated)
	r.HandleFunc("/api/v1/docs", apiVersionMiddleware(handlePublicAPIDocs)).Methods("GET")
	r.HandleFunc("/api/v1/docs.json", apiVersionMiddleware(handlePublicAPIDocs)).Methods("GET")
	r.HandleFunc("/api/v1/keys", readAuth(handlePublicAPIKeys)).Methods("GET", "POST")
	r.HandleFunc("/api/v1/usage", readAuth(handlePublicAPIUsage)).Methods("GET")
	r.HandleFunc("/api/v1/elections", apiKeyAuth(handlePublicAPIElections)).Methods("GET")
	r.HandleFunc("/api/v1/results", apiKeyAuth(handlePublicAPIResults)).Methods("GET")
	r.HandleFunc("/api/v1/results/{id:[0-9]+}", apiKeyAuth(handlePublicAPIResultDetail)).Methods("GET")
	r.HandleFunc("/api/v1/states", apiKeyAuth(handlePublicAPIStates)).Methods("GET")
	r.HandleFunc("/api/v1/polling-units", apiKeyAuth(handlePublicAPIPollingUnits)).Methods("GET")
	r.HandleFunc("/api/v1/collation", apiKeyAuth(handlePublicAPICollation)).Methods("GET")
	r.HandleFunc("/api/v1/ai/anomalies", apiKeyAuth(handleAIAnomalies)).Methods("GET")
	r.HandleFunc("/api/v1/ai/integrity", apiKeyAuth(handleAIIntegrity)).Methods("GET")

	// EMS - Voter Registration — auth required
	r.HandleFunc("/ems/voters", readAuth(handleListVoters)).Methods("GET")
	r.HandleFunc("/ems/voters/stats", readAuth(handleVoterStats)).Methods("GET")
	r.HandleFunc("/ems/voters/register", writeAuth(handleRegisterVoter)).Methods("POST")
	r.HandleFunc("/ems/voters/{vin}", readAuth(handleGetVoter)).Methods("GET")
	r.HandleFunc("/ems/voters/{vin}/verify", writeAuth(handleVoterVerify)).Methods("POST")
	r.HandleFunc("/ems/voters/{vin}/transfer", writeAuth(handleVoterTransfer)).Methods("POST")
	r.HandleFunc("/ems/registration-centers", readAuth(handleRegistrationCenters)).Methods("GET")

	// EMS - Workflow Engine — auth required
	r.HandleFunc("/ems/workflows", readAuth(handleListWorkflows)).Methods("GET")
	r.HandleFunc("/ems/workflows", adminOnly(handleCreateWorkflow)).Methods("POST")
	r.HandleFunc("/ems/workflows/{id}", readAuth(handleGetWorkflow)).Methods("GET")
	r.HandleFunc("/ems/workflows/{id}/advance", writeAuth(handleAdvanceWorkflow)).Methods("POST")

	// EMS - BVAS Sync Engine — auth required
	r.HandleFunc("/ems/sync/submit", writeAuth(handleBVASSyncSubmit)).Methods("POST")
	r.HandleFunc("/ems/sync/heartbeat", writeAuth(handleBVASHeartbeat)).Methods("POST")
	r.HandleFunc("/ems/sync/stats", readAuth(handleBVASSyncStats)).Methods("GET")
	r.HandleFunc("/ems/sync/queue", readAuth(handleBVASSyncQueue)).Methods("GET")
	r.HandleFunc("/ems/sync/conflicts/{id}/resolve", adminOnly(handleBVASConflictResolve)).Methods("POST")

	// EMS - Portal Integration Hub — auth required
	r.HandleFunc("/ems/portals", readAuth(handleListPortals)).Methods("GET")
	r.HandleFunc("/ems/portals/status", readAuth(handlePortalHubStatus)).Methods("GET")
	r.HandleFunc("/ems/portals/{id}", readAuth(handleGetPortal)).Methods("GET")
	r.HandleFunc("/ems/portals/{id}/sync", adminOnly(handlePortalSync)).Methods("POST")
	r.HandleFunc("/ems/portals/sync-log", readAuth(handlePortalSyncLog)).Methods("GET")
	r.HandleFunc("/ems/portals/webhooks", readAuth(handlePortalWebhooks)).Methods("GET")

	// EMS - Data Validation Pipeline — auth required
	r.HandleFunc("/ems/validation/rules", readAuth(handleListValidationRules)).Methods("GET")
	r.HandleFunc("/ems/validation/validate", writeAuth(handleValidateEntity)).Methods("POST")
	r.HandleFunc("/ems/validation/stats", readAuth(handleValidationStats)).Methods("GET")
	r.HandleFunc("/ems/validation/history", readAuth(handleValidationHistory)).Methods("GET")

	// EMS - Admin Console / Election Lifecycle — auth required
	r.HandleFunc("/ems/elections/{election_id}/lifecycle", readAuth(handleElectionLifecycle)).Methods("GET")
	r.HandleFunc("/ems/elections/{election_id}/transition", adminOnly(handleTransitionElection)).Methods("POST")
	r.HandleFunc("/ems/elections/{id}/fsm/transition", adminOnly(handleElectionFSMTransition)).Methods("POST")
	r.HandleFunc("/ems/elections/{id}/fsm/diagram", readAuth(handleElectionFSMDiagram)).Methods("GET")
	r.HandleFunc("/ems/elections/{id}/fsm/history", readAuth(handleElectionStateHistory)).Methods("GET")

	// Duplicate Voter Detection
	r.HandleFunc("/voters/duplicates/scan", adminOnly(handleDuplicateVoterScan)).Methods("POST")
	r.HandleFunc("/voters/duplicates/resolve", adminOnly(handleDuplicateVoterResolve)).Methods("POST")

	// GPS Spoofing Detection
	r.HandleFunc("/geo/spoof-check", writeAuth(handleGPSSpoofCheck)).Methods("POST")

	// Live Dashboard SSE
	r.HandleFunc("/dashboard/stream", handleDashboardSSE).Methods("GET")

	// OAuth2/OIDC
	r.HandleFunc("/.well-known/openid-configuration", handleOIDCDiscovery).Methods("GET")
	r.HandleFunc("/auth/oidc/callback", handleOIDCCallback).Methods("GET")

	// Webhook Subscriptions
	r.HandleFunc("/api/v1/webhooks", adminOnly(handleWebhookCreate)).Methods("POST")
	r.HandleFunc("/api/v1/webhooks", readAuth(handleWebhookList)).Methods("GET")
	r.HandleFunc("/api/v1/webhooks/{id}", adminOnly(handleWebhookDelete)).Methods("DELETE")
	r.HandleFunc("/api/v1/webhooks/{id}", adminOnly(handleUpdateWebhook)).Methods("PATCH")

	// Export endpoints (CSV/JSON)
	r.HandleFunc("/export/results", readAuth(handleExportResults)).Methods("GET")
	r.HandleFunc("/export/voters", adminOnly(handleExportVoters)).Methods("GET")
	r.HandleFunc("/export/collation", readAuth(handleExportCollation)).Methods("GET")
	r.HandleFunc("/export/audit", adminOnly(handleAuditExport)).Methods("GET")

	// Dispute Resolution
	r.HandleFunc("/disputes", writeAuth(handleFileDispute)).Methods("POST")
	r.HandleFunc("/disputes", readAuth(handleListDisputes)).Methods("GET")
	r.HandleFunc("/disputes/{id}/resolve", adminOnly(handleResolveDispute)).Methods("POST")
	r.HandleFunc("/disputes/{id}/comments", writeAuth(handleDisputeComment)).Methods("POST")
	r.HandleFunc("/disputes/stats", readAuth(handleDisputeStats)).Methods("GET")

	// Push Notifications
	r.HandleFunc("/push/devices", writeAuth(handleRegisterDevice)).Methods("POST")
	r.HandleFunc("/push/send-targeted", adminOnly(handleSendPushNotification)).Methods("POST")
	r.HandleFunc("/push/history", readAuth(handleNotificationHistory)).Methods("GET")

	r.HandleFunc("/ems/staff", readAuth(handleListStaffAssignments)).Methods("GET")
	r.HandleFunc("/ems/staff", adminOnly(handleAssignStaff)).Methods("POST")
	r.HandleFunc("/ems/materials", readAuth(handleListMaterials)).Methods("GET")
	r.HandleFunc("/ems/materials/{id}/dispatch", adminOnly(handleDispatchMaterial)).Methods("PATCH")
	r.HandleFunc("/ems/materials/stats", readAuth(handleMaterialStats)).Methods("GET")
	r.HandleFunc("/ems/dashboard", readAuth(handleEMSDashboard)).Methods("GET")

	// Phase 7 - Enhanced Biometric Verification — auth required
	r.HandleFunc("/biometric/stats", readAuth(handleBiometricStats)).Methods("GET")
	r.HandleFunc("/biometric/verify", writeAuth(handleBiometricVerify)).Methods("POST")
	r.HandleFunc("/biometric/profiles", readAuth(handleBiometricProfiles)).Methods("GET")
	r.HandleFunc("/biometric/abis/duplicates", readAuth(handleABISDuplicates)).Methods("GET")
	r.HandleFunc("/biometric/abis/{id}/resolve", adminOnly(handleABISResolve)).Methods("POST")

	// Biometric Engine - Production-Grade — auth required
	r.HandleFunc("/biometric/engine/stats", readAuth(handleBiometricEngineStats)).Methods("GET")
	r.HandleFunc("/biometric/engine/enroll", writeAuth(handleABISEnroll)).Methods("POST")
	r.HandleFunc("/biometric/engine/verify", writeAuth(handleABISVerify)).Methods("POST")
	r.HandleFunc("/biometric/engine/verify-multimodal", writeAuth(handleMultiModalVerify)).Methods("POST")
	r.HandleFunc("/biometric/engine/identify", readAuth(handleABISIdentify)).Methods("GET")
	r.HandleFunc("/biometric/engine/pad-check", writeAuth(handlePADCheck)).Methods("POST")
	r.HandleFunc("/biometric/engine/pad-history", readAuth(handlePADHistory)).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/jobs", readAuth(handleDedupJobs)).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/start", adminOnly(handleDedupStart)).Methods("POST")
	r.HandleFunc("/biometric/engine/dedup/{job_id}/candidates", readAuth(handleDedupCandidates)).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/resolve/{id}", adminOnly(handleDedupResolve)).Methods("POST")
	r.HandleFunc("/biometric/engine/vault/stats", adminOnly(handleVaultStats)).Methods("GET")
	r.HandleFunc("/biometric/engine/vault/rotate-key", adminOnly(handleVaultRotateKey)).Methods("POST")
	r.HandleFunc("/biometric/engine/vault/audit", adminOnly(handleVaultAudit)).Methods("GET")
	r.HandleFunc("/biometric/engine/devices", readAuth(handleBVASDeviceCapabilities)).Methods("GET")
	r.HandleFunc("/biometric/engine/devices/register", writeAuth(handleBVASRegisterDevice)).Methods("POST")
	r.HandleFunc("/biometric/engine/capture-sessions", readAuth(handleBVASCaptureSessions)).Methods("GET")
	r.HandleFunc("/biometric/engine/pipeline", readAuth(handleABISPipelineStatus)).Methods("GET")
	r.HandleFunc("/biometric/engine/config", adminOnly(handleABISConfig)).Methods("GET", "POST")
	r.HandleFunc("/biometric/engine/template-integrity", readAuth(handleTemplateIntegrity)).Methods("GET")

	// Biometric Advanced - 15 Improvements — auth required
	r.HandleFunc("/biometric/advanced/stats", readAuth(handleAdvancedBiometricStats)).Methods("GET")
	r.HandleFunc("/biometric/advanced/hsm/stats", adminOnly(handleHSMStats)).Methods("GET")
	r.HandleFunc("/biometric/advanced/hsm/generate-key", adminOnly(handleHSMGenerateKey)).Methods("POST")
	r.HandleFunc("/biometric/advanced/sdk/providers", readAuth(handleSDKProviders)).Methods("GET")
	r.HandleFunc("/biometric/advanced/aging", readAuth(handleTemplateAging)).Methods("GET")
	r.HandleFunc("/biometric/advanced/cancelable", readAuth(handleCancelableStatus)).Methods("GET")
	r.HandleFunc("/biometric/advanced/cancelable/revoke", adminOnly(handleCancelableRevoke)).Methods("POST")
	r.HandleFunc("/biometric/advanced/threshold-tuning", adminOnly(handleThresholdTuning)).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/distributed-dedup", adminOnly(handleDistributedDedup)).Methods("POST")
	r.HandleFunc("/biometric/advanced/pad-models", readAuth(handlePADModels)).Methods("GET")
	r.HandleFunc("/biometric/advanced/pad-models/update", adminOnly(handlePADModelUpdate)).Methods("POST")
	r.HandleFunc("/biometric/advanced/quality-gateway", readAuth(handleQualityGateway)).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/offline-queue", readAuth(handleOfflineQueue)).Methods("GET")
	r.HandleFunc("/biometric/advanced/offline-queue/sync", writeAuth(handleBioOfflineSync)).Methods("POST")
	r.HandleFunc("/biometric/advanced/score-normalize", writeAuth(handleScoreNormalize)).Methods("POST")
	r.HandleFunc("/biometric/advanced/score-cohorts", readAuth(handleScoreCohorts)).Methods("GET")
	r.HandleFunc("/biometric/advanced/nist-benchmark", adminOnly(handleNISTBenchmark)).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/audit/timeline", readAuth(handleBioAuditTimeline)).Methods("GET")
	r.HandleFunc("/biometric/advanced/audit/summary", readAuth(handleBioAuditSummary)).Methods("GET")
	r.HandleFunc("/biometric/advanced/kiosk/start", writeAuth(handleKioskStart)).Methods("POST")
	r.HandleFunc("/biometric/advanced/kiosk/{session_id}/advance", writeAuth(handleKioskAdvance)).Methods("POST")
	r.HandleFunc("/biometric/advanced/kiosk/sessions", readAuth(handleKioskSessions)).Methods("GET")
	r.HandleFunc("/biometric/advanced/multi-finger/enroll", writeAuth(handleMultiFingerEnroll)).Methods("POST")
	r.HandleFunc("/biometric/advanced/multi-finger", readAuth(handleMultiFingerStatus)).Methods("GET")
	r.HandleFunc("/biometric/advanced/privacy-match", writeAuth(handlePrivacyMatch)).Methods("POST")
	r.HandleFunc("/biometric/advanced/privacy-stats", readAuth(handlePrivacyStats)).Methods("GET")

	// Phase 7 - Blockchain-Enhanced Result Transmission — auth required
	r.HandleFunc("/blockchain/stats", readAuth(handleBlockchainStats)).Methods("GET")
	r.HandleFunc("/blockchain/chain", readAuth(handleBlockchainChain)).Methods("GET")
	r.HandleFunc("/blockchain/contracts", readAuth(handleSmartContracts)).Methods("GET")
	r.HandleFunc("/blockchain/verify/{result_id}", readAuth(handleBlockchainVerifyResult)).Methods("GET")
	r.HandleFunc("/blockchain/audit", readAuth(handleBlockchainAuditTrail)).Methods("GET")

	// Production Blockchain & Ledger — auth required
	r.HandleFunc("/blockchain/production/stats", readAuth(handleBlockchainProductionStats)).Methods("GET")
	r.HandleFunc("/blockchain/fabric/network", readAuth(handleFabricNetworkStats)).Methods("GET")
	r.HandleFunc("/blockchain/fabric/blocks", readAuth(handleFabricBlocks)).Methods("GET")
	r.HandleFunc("/blockchain/fabric/transactions", readAuth(handleFabricTransactions)).Methods("GET")
	r.HandleFunc("/blockchain/fabric/verify-chain", readAuth(handleFabricVerifyChain)).Methods("GET")
	r.HandleFunc("/blockchain/fabric/submit", adminOnly(handleFabricSubmitTx)).Methods("POST")
	r.HandleFunc("/blockchain/chaincode/validate-result", writeAuth(handleChaincodeValidateResult)).Methods("POST")
	r.HandleFunc("/blockchain/chaincode/aggregate", writeAuth(handleChaincodeAggregate)).Methods("POST")
	r.HandleFunc("/blockchain/ipfs/stats", readAuth(handleIPFSStats)).Methods("GET")
	r.HandleFunc("/blockchain/ipfs/store", writeAuth(handleIPFSStore)).Methods("POST")
	r.HandleFunc("/blockchain/ipfs/verify", readAuth(handleIPFSVerify)).Methods("GET")
	r.HandleFunc("/blockchain/ipfs/objects", readAuth(handleIPFSObjects)).Methods("GET")
	r.HandleFunc("/blockchain/ledger/stats", readAuth(handlePersistentTBStats)).Methods("GET")
	r.HandleFunc("/blockchain/ledger/accounts", readAuth(handlePersistentTBAccounts)).Methods("GET")
	r.HandleFunc("/blockchain/ledger/transfers", readAuth(handlePersistentTBTransfers)).Methods("GET")
	r.HandleFunc("/blockchain/ledger/transfer", adminOnly(handlePersistentTBCreateTransfer)).Methods("POST")
	r.HandleFunc("/blockchain/ledger/transfer/post", adminOnly(handlePersistentTBPostTransfer)).Methods("POST")
	r.HandleFunc("/blockchain/merkle/build", adminOnly(handleMerkleTreeBuild)).Methods("POST")
	r.HandleFunc("/blockchain/merkle/trees", readAuth(handleMerkleTreeList)).Methods("GET")

	// Phase 7 - Training & Capacity Building — auth required
	r.HandleFunc("/training/courses", readAuth(handleTrainingCourses)).Methods("GET")
	r.HandleFunc("/training/stats", readAuth(handleTrainingStats)).Methods("GET")
	r.HandleFunc("/training/enrollments", readAuth(handleTrainingEnrollments)).Methods("GET")
	r.HandleFunc("/training/enrollments", writeAuth(handleEnrollTraining)).Methods("POST")
	r.HandleFunc("/training/enrollments/{id:[0-9]+}/complete", writeAuth(handleCompleteTraining)).Methods("POST")
	r.HandleFunc("/training/certificates", readAuth(handleTrainingCertificates)).Methods("GET")
	r.HandleFunc("/training/vr-scenarios", readAuth(handleVRScenarios)).Methods("GET")
	r.HandleFunc("/training/courses", adminOnly(handleCreateCourse)).Methods("POST")

	// Phase 7 - Stakeholder Engagement — auth required
	r.HandleFunc("/stakeholders/stats", readAuth(handleStakeholderStats)).Methods("GET")
	r.HandleFunc("/stakeholders", readAuth(handleListStakeholders)).Methods("GET")
	r.HandleFunc("/stakeholders", adminOnly(handleCreateStakeholder)).Methods("POST")
	r.HandleFunc("/stakeholders/incidents", readAuth(handleStakeholderIncidents)).Methods("GET")
	r.HandleFunc("/stakeholders/grievances", readAuth(handleListGrievances)).Methods("GET")
	r.HandleFunc("/stakeholders/grievances", writeAuth(handleCreateGrievance)).Methods("POST")
	r.HandleFunc("/stakeholders/grievances/{id:[0-9]+}", adminOnly(handleResolveGrievance)).Methods("PATCH")
	r.HandleFunc("/stakeholders/notifications", readAuth(handlePushNotifications)).Methods("GET")
	r.HandleFunc("/stakeholders/notifications", adminOnly(handleSendNotification)).Methods("POST")
	r.HandleFunc("/stakeholders/notifications/push", adminOnly(handleSendPushNotification)).Methods("POST")

	// Phase 7 - AI Election Monitoring & Analytics — auth required
	r.HandleFunc("/ai-monitoring/dashboard", readAuth(handleAIMonitoringDashboard)).Methods("GET")
	r.HandleFunc("/ai-monitoring/predictions", readAuth(handleAIPredictions)).Methods("GET")
	r.HandleFunc("/ai-monitoring/predictions", writeAuth(handleCreateAIPrediction)).Methods("POST")
	r.HandleFunc("/ai-monitoring/sentiment", readAuth(handleSentimentAnalysis)).Methods("GET")
	r.HandleFunc("/ai-monitoring/sentiment", writeAuth(handleCreateSentimentEntry)).Methods("POST")
	r.HandleFunc("/ai-monitoring/misinformation", readAuth(handleMisinformationAlerts)).Methods("GET")
	r.HandleFunc("/ai-monitoring/misinformation", writeAuth(handleCreateMisinformationAlert)).Methods("POST")
	r.HandleFunc("/ai-monitoring/misinformation/{id:[0-9]+}", writeAuth(handleUpdateMisinformationAlert)).Methods("PATCH")
	r.HandleFunc("/ai-monitoring/security-threats", readAuth(handleSecurityThreats)).Methods("GET")
	r.HandleFunc("/ai-monitoring/security-threats", writeAuth(handleCreateSecurityThreat)).Methods("POST")
	r.HandleFunc("/ai-monitoring/security-threats/{id:[0-9]+}", writeAuth(handleUpdateSecurityThreat)).Methods("PATCH")
	r.HandleFunc("/ai-monitoring/cv-monitoring", readAuth(handleCVMonitoring)).Methods("GET")
	r.HandleFunc("/ai-monitoring/cv-monitoring", writeAuth(handleCreateCVEvent)).Methods("POST")

	// Pgpool-II Infrastructure — admin only
	r.HandleFunc("/pgpool/status", adminOnly(handlePgpoolStatus)).Methods("GET")
	r.HandleFunc("/pgpool/nodes", adminOnly(handlePgpoolNodes)).Methods("GET")
	r.HandleFunc("/pgpool/health", adminOnly(handlePgpoolHealth)).Methods("GET")
	r.HandleFunc("/pgpool/config", adminOnly(handlePgpoolConfig)).Methods("GET")
	r.HandleFunc("/pgpool/metrics", adminOnly(handlePgpoolMetricsEndpoint)).Methods("GET")
	r.HandleFunc("/pgpool/replication", adminOnly(handlePgpoolReplicationStatus)).Methods("GET")
	r.HandleFunc("/pgpool/cache", adminOnly(handlePgpoolQueryCache)).Methods("GET")
	r.HandleFunc("/pgpool/dashboard", adminOnly(handlePgpoolDashboard)).Methods("GET")

	// Production Upgrades — admin only for write, auth for read
	r.HandleFunc("/production/status", readAuth(handleProductionUpgradeStatus)).Methods("GET")
	r.HandleFunc("/production/hsm/stats", adminOnly(handleProductionHSMStats)).Methods("GET")
	r.HandleFunc("/production/hsm/generate-key", adminOnly(handleProductionHSMGenerateKey)).Methods("POST")
	r.HandleFunc("/production/hsm/sign", adminOnly(handleProductionHSMSign)).Methods("POST")
	r.HandleFunc("/production/hsm/verify", adminOnly(handleProductionHSMVerify)).Methods("POST")
	r.HandleFunc("/production/hsm/rotate", adminOnly(handleProductionHSMRotate)).Methods("POST")
	r.HandleFunc("/production/sms/stats", readAuth(handleProductionSMSGatewayStats)).Methods("GET")
	r.HandleFunc("/production/sms/send", adminOnly(handleProductionSMSSend)).Methods("POST")
	r.HandleFunc("/production/sms/delivery-log", readAuth(handleProductionSMSDeliveryLog)).Methods("GET")
	r.HandleFunc("/production/pad/stats", readAuth(handleProductionPADStats)).Methods("GET")
	r.HandleFunc("/production/pad/check", writeAuth(handleProductionPADCheck)).Methods("POST")
	r.HandleFunc("/production/pad/attack-log", readAuth(handleProductionPADAttackLog)).Methods("GET")
	r.HandleFunc("/production/ipfs/stats", readAuth(handleProductionIPFSStats)).Methods("GET")
	r.HandleFunc("/production/ipfs/store", adminOnly(handleProductionIPFSStore)).Methods("POST")
	r.HandleFunc("/production/ipfs/verify", readAuth(handleProductionIPFSVerify)).Methods("GET")
	r.HandleFunc("/production/fabric/stats", readAuth(handleProductionFabricStats)).Methods("GET")
	r.HandleFunc("/production/fabric/submit", adminOnly(handleProductionFabricSubmit)).Methods("POST")
	r.HandleFunc("/production/fabric/verify-endorsements", readAuth(handleProductionFabricVerifyEndorsements)).Methods("GET")
	r.HandleFunc("/production/ledger/stats", readAuth(handleProductionTBStats)).Methods("GET")
	r.HandleFunc("/production/ledger/transfer", adminOnly(handleProductionTBCreateTransfer)).Methods("POST")
	r.HandleFunc("/production/ledger/journal", readAuth(handleProductionTBJournal)).Methods("GET")

	// Middleware status & management
	r.HandleFunc("/middleware/status", handleMiddlewareStatus).Methods("GET")
	r.HandleFunc("/middleware/health", handleMiddlewareHealth).Methods("GET")
	r.HandleFunc("/admin/data-retention", readAuth(handleDataRetentionStatus)).Methods("GET")
	r.HandleFunc("/middleware/kafka/topics", readAuth(handleKafkaTopics)).Methods("GET")
	r.HandleFunc("/middleware/temporal/workflows", readAuth(handleTemporalWorkflows)).Methods("GET")
	r.HandleFunc("/middleware/temporal/workflows/{id}", readAuth(handleTemporalWorkflowStatus)).Methods("GET")
	r.HandleFunc("/middleware/tigerbeetle/accounts", readAuth(handleTBAccounts)).Methods("GET")
	r.HandleFunc("/middleware/tigerbeetle/transfers", readAuth(handleTBTransfers)).Methods("GET")
	r.HandleFunc("/middleware/apisix/routes", readAuth(handleAPISIXRoutes)).Methods("GET")
	r.HandleFunc("/middleware/apisix/config", readAuth(handleAPISIXConfig)).Methods("GET")
	r.HandleFunc("/middleware/permify/check", writeAuth(handlePermifyCheck)).Methods("POST")
	r.HandleFunc("/middleware/fluvio/topics", readAuth(handleFluvioTopics)).Methods("GET")
	r.HandleFunc("/middleware/fluvio/consume/{topic}", readAuth(handleFluvioConsume)).Methods("GET")
	r.HandleFunc("/middleware/lakehouse/analytics/{election_id}/{type}", readAuth(handleLakehouseAnalytics)).Methods("GET")
	r.HandleFunc("/middleware/lakehouse/tables", readAuth(handleLakehouseTables)).Methods("GET")
	r.HandleFunc("/middleware/redis/stats", adminOnly(handleRedisStats)).Methods("GET")

	// Mojaloop — 4-Phase Transaction Pattern
	r.HandleFunc("/middleware/mojaloop/status", handleMojaStatus).Methods("GET")
	r.HandleFunc("/middleware/mojaloop/parties", handleMojaPartyLookup).Methods("GET")
	r.HandleFunc("/middleware/mojaloop/quotes", writeAuth(handleMojaCreateQuote)).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/transfers", writeAuth(handleMojaCreateTransfer)).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/settlements", adminOnly(handleMojaSettle)).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/transactions", readAuth(handleMojaTransactions)).Methods("GET")

	// OpenSearch — Full-text Search
	r.HandleFunc("/middleware/opensearch/status", readAuth(handleOpenSearchStatus)).Methods("GET")
	r.HandleFunc("/middleware/opensearch/search", readAuth(handleOpenSearchSearch)).Methods("GET")
	r.HandleFunc("/middleware/opensearch/index", writeAuth(handleOpenSearchIndex)).Methods("POST")
	r.HandleFunc("/middleware/opensearch/indices", readAuth(handleOpenSearchIndices)).Methods("GET")
	r.HandleFunc("/middleware/opensearch/stats", readAuth(handleOpenSearchStats)).Methods("GET")

	// OpenAppSec — WAF
	r.HandleFunc("/middleware/waf/status", adminOnly(handleWAFStatus)).Methods("GET")
	r.HandleFunc("/middleware/waf/inspect", adminOnly(handleWAFInspect)).Methods("POST")
	r.HandleFunc("/middleware/waf/threats", adminOnly(handleWAFThreatLog)).Methods("GET")
	r.HandleFunc("/middleware/waf/stats", adminOnly(handleWAFStats)).Methods("GET")
	r.HandleFunc("/middleware/waf/blocklist", adminOnly(handleWAFBlocklist)).Methods("GET", "POST")

	// INEC Domain Logic — Form Validation, Collation, Reconciliation — auth required
	r.HandleFunc("/inec/ec8a/submit", writeAuth(handleSubmitEC8A)).Methods("POST")
	r.HandleFunc("/inec/collation", readAuth(handleHierarchicalCollation)).Methods("GET")
	r.HandleFunc("/inec/reconciliation/ballot", readAuth(handleBallotReconciliation)).Methods("GET")
	r.HandleFunc("/inec/reconciliation/dual-ledger", readAuth(handleDualLedgerReconciliation)).Methods("GET")

	// Admin user management
	r.HandleFunc("/admin/users/promote", adminOnly(handlePromoteUser)).Methods("POST")
	r.HandleFunc("/users", adminOnly(handleListUsers)).Methods("GET")
	r.HandleFunc("/users/{id:[0-9]+}", adminOnly(handleGetUser)).Methods("GET")
	r.HandleFunc("/users", adminOnly(handleCreateUser)).Methods("POST")
	r.HandleFunc("/users/{id:[0-9]+}", adminOnly(handleUpdateUser)).Methods("PATCH")
	r.HandleFunc("/users/{id:[0-9]+}", adminOnly(handleDeleteUser)).Methods("DELETE")

	// Election delete
	r.HandleFunc("/elections/{id:[0-9]+}", adminOnly(handleDeleteElection)).Methods("DELETE")

	// Command Center (#1) + Load Shedding (#25)
	r.HandleFunc("/command-center/live", adminOnly(handleCommandCenterLive)).Methods("GET")
	r.HandleFunc("/command-center/stream", handleCommandCenterSSE).Methods("GET")
	r.HandleFunc("/command-center/alerts", readAuth(handleCommandCenterAlerts)).Methods("GET")
	r.HandleFunc("/command-center/dispatch", adminOnly(handleNotificationDispatch)).Methods("POST")
	r.HandleFunc("/escalation/config", adminOnly(handleEscalationConfig)).Methods("GET", "POST")
	r.HandleFunc("/load-shedding", adminOnly(handleLoadShedding)).Methods("GET", "POST")

	// MFA (#3) — TOTP, WebAuthn, SMS OTP, Backup Codes
	r.HandleFunc("/auth/mfa/totp/setup", writeAuth(handleMFASetupTOTP)).Methods("POST")
	r.HandleFunc("/auth/mfa/totp/verify", writeAuth(handleMFAVerifyTOTP)).Methods("POST")
	r.HandleFunc("/auth/mfa/challenge", handleMFAChallenge).Methods("POST")
	r.HandleFunc("/auth/mfa/sms/send", handleMFASendSMS).Methods("POST")
	r.HandleFunc("/auth/mfa/status", readAuth(handleMFAStatus)).Methods("GET")
	r.HandleFunc("/auth/mfa/webauthn/register", writeAuth(handleMFAWebAuthnRegister)).Methods("POST")
	// Enhanced MFA (RFC 6238 TOTP + WebAuthn FIDO2 + backup codes)
	r.HandleFunc("/auth/mfa/setup", writeAuth(handleMFASetup)).Methods("POST")
	r.HandleFunc("/auth/mfa/verify-setup", writeAuth(handleMFAVerifySetup)).Methods("POST")
	r.HandleFunc("/auth/mfa/disable", writeAuth(handleMFADisable)).Methods("POST")
	r.HandleFunc("/auth/mfa/webauthn/begin", writeAuth(handleMFAWebAuthnBegin)).Methods("POST")
	r.HandleFunc("/auth/mfa/webauthn/complete", writeAuth(handleMFAWebAuthnComplete)).Methods("POST")
	r.HandleFunc("/auth/mfa/webauthn/list", readAuth(handleMFAWebAuthnList)).Methods("GET")
	r.HandleFunc("/auth/mfa/backup-codes", writeAuth(handleMFABackupCodes)).Methods("POST")

	// Citizen Portal (#6) + Cryptographic Result Chain (#4)
	r.HandleFunc("/citizen/verify", handleCitizenVerify).Methods("GET")
	r.HandleFunc("/citizen/verify/signature", handleCitizenVerifySignature).Methods("GET")
	r.HandleFunc("/results/sign", writeAuth(handleSignResult)).Methods("POST")
	r.HandleFunc("/results/qr", readAuth(handleResultQRData)).Methods("GET")

	// Media API (#23) + PDF Reports (#8) + OpenAPI (#11)
	r.HandleFunc("/media/stream", handleMediaStream).Methods("GET")
	r.HandleFunc("/media/widget", handleMediaWidget).Methods("GET")
	r.HandleFunc("/export/report/pdf", readAuth(handleExportPDFReport)).Methods("GET")
	r.HandleFunc("/openapi.json", handleOpenAPIDocs).Methods("GET")

	// Geo-fenced Submissions (#12) + Override
	r.HandleFunc("/geo/submission/check", writeAuth(handleGeoFencedSubmit)).Methods("POST")
	r.HandleFunc("/geo/submission/override", adminOnly(handleGeoFenceOverride)).Methods("POST")

	// Anomaly Escalation (#7) + Biometric Quality (#9)
	r.HandleFunc("/anomaly/escalation", writeAuth(handleAnomalyEscalation)).Methods("GET", "POST")
	r.HandleFunc("/biometric/quality-check", writeAuth(handleBiometricQualityCheck)).Methods("POST")

	// Predictive Analytics (#16) + Multi-Election (#17) + Archive
	r.HandleFunc("/predictive/analytics", readAuth(handlePredictiveAnalytics)).Methods("GET")
	r.HandleFunc("/election/templates", readAuth(handleElectionTemplates)).Methods("GET", "POST")
	r.HandleFunc("/election/archive", readAuth(handleElectionArchive)).Methods("GET", "POST")

	// Data Sovereignty (#20) + Erasure
	r.HandleFunc("/data/classification", adminOnly(handleDataClassification)).Methods("GET", "POST")
	r.HandleFunc("/data/erasure", adminOnly(handleDataErasure)).Methods("POST")

	// Observer Photo Verification (#18)
	r.HandleFunc("/observer/photo-verify", writeAuth(handleObserverPhotoVerify)).Methods("POST")

	// Offline Conflict Resolution (#2)
	r.HandleFunc("/offline/conflict/resolve", writeAuth(handleOfflineConflictResolve)).Methods("POST")

	// Voice IVR (#14)
	r.HandleFunc("/ivr/verify", handleIVRVerify).Methods("POST")

	// ── Platform Improvements v2 ──
	// OpenAPI & Swagger
	r.HandleFunc("/api/openapi.json", handleOpenAPISpec).Methods("GET")
	r.HandleFunc("/api/docs", handleSwaggerUI).Methods("GET")
	// Geo-IP Fraud Detection
	r.HandleFunc("/security/geo-ip-check", writeAuth(handleGeoIPCheck)).Methods("POST")
	// DLP Export Controls
	r.HandleFunc("/security/dlp-export", writeAuth(handleDLPExport)).Methods("POST")
	// Real-Time Presence
	r.HandleFunc("/presence/heartbeat", writeAuth(handlePresenceHeartbeat)).Methods("POST")
	r.HandleFunc("/presence/list", readAuth(handlePresenceList)).Methods("GET")
	// Batch Operations
	r.HandleFunc("/admin/batch/users", adminOnly(handleBatchUserImport)).Methods("POST")
	r.HandleFunc("/admin/batch/status", adminOnly(handleBatchStatusUpdate)).Methods("POST")
	// AI Integrity Score
	r.HandleFunc("/ai/integrity-score", readAuth(handleIntegrityScore)).Methods("GET")
	r.HandleFunc("/ai/integrity-heatmap", readAuth(handleIntegrityHeatmap)).Methods("GET")
	// Blockchain Result Certificate
	r.HandleFunc("/public/result-certificate", handleResultCertificate).Methods("GET")
	// Public TV Dashboard
	r.HandleFunc("/public/tv-dashboard", handlePublicTVDashboard).Methods("GET")
	// Compliance Reporting
	r.HandleFunc("/reports/compliance", readAuth(handleComplianceReport)).Methods("GET")
	// Audit Timeline
	r.HandleFunc("/audit/timeline", readAuth(handleAuditTimeline)).Methods("GET")
	// Voice Transcription
	r.HandleFunc("/voice/transcribe", writeAuth(handleVoiceTranscription)).Methods("POST")

	// Static file serving for observer photo uploads
	r.PathPrefix("/uploads/").Handler(http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))

	// Prometheus metrics endpoint
	r.Handle("/metrics", metricsHandler()).Methods("GET")

	// Middleware chain: panic recovery → request ID → tracing → access log → input validation → metrics → CORS → auth → CSRF → security → WAF → rate limit → load shed → role rate → gzip → size limit
	handler := panicRecoveryMiddleware(
		requestIDMiddleware(
			otelTracingMiddleware(
				tracingMiddleware(
					accessLogMiddleware(
						inputValidationMiddleware(
							metricsMiddleware(
								corsProductionMiddleware(
									jwtAuthMiddleware(
										csrfMiddleware(
											enhancedSecurityHeaders(
												wafMiddleware(
													requestSizeLimit(
														rateLimitMiddleware(
															loadSheddingMiddleware(
																roleBasedRateLimit(
																	gzipMiddleware(r)))))))))))))))))

	addr := ":8088"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	tlsMinVersion := uint16(tls.VersionTLS12)
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion:               tlsMinVersion,
			PreferServerCipherSuites: true,
			CurvePreferences:         []tls.CurveID{tls.X25519, tls.CurveP256},
		},
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		tlsCert := os.Getenv("TLS_CERT_FILE")
		tlsKey := os.Getenv("TLS_KEY_FILE")
		if tlsCert != "" && tlsKey != "" {
			log.Info().Str("addr", addr).Msg("INEC Go Backend listening (TLS)")
			if err := srv.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
				log.Fatal().Err(err).Msg("TLS server failed")
			}
		} else {
			log.Info().Str("addr", addr).Msg("INEC Go Backend listening")
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatal().Err(err).Msg("Server failed")
			}
		}
	}()

	<-done
	log.Info().Msg("Shutting down gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Close middleware connections
	if mwHub != nil {
		mwHub.Shutdown()
	}

	// Shutdown architecture layer (event bus, tracing exporters)
	if serviceRegistry != nil {
		serviceRegistry.Shutdown()
	}

	if err := srv.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Forced shutdown")
	}

	if db != nil {
		db.Close()
	}
	log.Info().Msg("Server stopped")
}

type M map[string]interface{}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, detail string) {
	// In production, suppress internal error details from 500 responses
	if code >= 500 && os.Getenv("APP_ENV") == "production" {
		log.Error().Int("code", code).Str("detail", detail).Msg("internal error")
		writeJSON(w, code, M{"detail": "internal server error"})
		return
	}
	writeJSON(w, code, M{"detail": detail})
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
	// Try Redis-backed rate limiting for multi-replica consistency
	if mwHub != nil && mwHub.Redis != nil {
		return rl.allowRedis(key, limit, window)
	}
	return rl.allowLocal(key, limit, window)
}

func (rl *rateLimiterStore) allowRedis(key string, limit int, window time.Duration) bool {
	ctx := context.Background()
	redisKey := "ratelimit:" + key
	count, err := mwHub.Redis.Incr(ctx, redisKey)
	if err != nil {
		// Fall back to local on Redis error
		return rl.allowLocal(key, limit, window)
	}
	if count == 1 {
		// First request in window — set expiry
		mwHub.Redis.Expire(ctx, redisKey, window)
	}
	return int(count) <= limit
}

func (rl *rateLimiterStore) allowLocal(key string, limit int, window time.Duration) bool {
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

func requestSizeLimit(next http.Handler) http.Handler {
	const maxBody = 10 << 20 // 10 MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBody {
			writeError(w, 413, "request body too large")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func rateLimitMiddleware(next http.Handler) http.Handler {
	type rateRule struct {
		prefix string
		limit  int
		window time.Duration
	}
	limits := []rateRule{
		// Auth endpoints: aggressive limits (brute-force protection)
		{"/auth/login", 5, time.Minute},           // 5 login attempts per minute per IP
		{"/auth/register", 3, time.Minute},         // 3 registrations per minute per IP
		{"/auth/refresh", 10, time.Minute},         // 10 token refreshes per minute
		{"/auth/forgot-password", 2, time.Minute},  // 2 password reset requests per minute
		{"/sms/send-otp", 2, time.Minute},          // 2 OTP sends per minute
		{"/sms/verify-otp", 5, time.Minute},        // 5 OTP verifications per minute
		// Data endpoints
		{"/geo/tiles", 120, time.Minute},
		{"/dashboard/metrics", 30, time.Minute},
		{"/results", 60, time.Minute},
		{"/geo/reports", 10, time.Minute},
		{"/export/", 2, time.Minute},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r) // X-Forwarded-For aware — works behind load balancers
		for _, l := range limits {
			if strings.HasPrefix(r.URL.Path, l.prefix) {
				if !rateLimiter.allow(ip+":"+l.prefix, l.limit, l.window) {
					w.Header().Set("Retry-After", fmt.Sprintf("%d", int(l.window.Seconds())))
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
		// Skip gzip for SSE/streaming endpoints (they need raw Flusher access)
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Path == "/observer/stream" {
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
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Msg("broadcastWS: marshal failed")
		return
	}
	wsClients.RLock()
	defer wsClients.RUnlock()
	for conn := range wsClients.conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Debug().Err(err).Msg("broadcastWS: write failed, client will be cleaned up")
		}
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
	if i <= 0 {
		return def
	}
	if i > 10000 {
		return 10000
	}
	return i
}

// Deep health check — verifies DB, middleware, disk
func handleDeepHealthCheck(w http.ResponseWriter, r *http.Request) {
	checks := M{}
	allOK := true

	// Database check
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		checks["database"] = M{"status": "unhealthy", "error": err.Error()}
		allOK = false
	} else {
		var count int
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&count); err != nil {
			checks["database"] = M{"status": "degraded", "error": err.Error()}
			allOK = false
		} else {
			checks["database"] = M{"status": "healthy"}
		}
	}

	// Middleware subsystem checks
	if mwHub != nil {
		mwChecks := M{}
		for name, st := range mwHub.status {
			mwChecks[name] = M{"connected": st.Connected, "mode": st.Mode}
			if !st.Connected {
				allOK = false
			}
		}
		checks["middleware"] = mwChecks
	}

	// Memory / uptime info
	checks["uptime"] = time.Since(serverStartTime).String()

	status := 200
	result := "healthy"
	if !allOK {
		status = 503
		result = "degraded"
	}
	writeJSON(w, status, M{"status": result, "checks": checks})
}

func handleReadinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		writeJSON(w, 503, M{"ready": false, "error": "database unreachable"})
		return
	}
	writeJSON(w, 200, M{"ready": true})
}

var serverStartTime = time.Now()



