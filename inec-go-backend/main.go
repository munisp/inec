package main

import (
	"compress/gzip"
	"context"
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
	_ "modernc.org/sqlite"
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
	// Initialize structured logging
	initLogger()
	// Initialize input validation
	initValidator()
	// Initialize Prometheus metrics
	initMetrics()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dbPath := os.Getenv("DB_PATH")
		if dbPath == "" {
			dbPath = "file:inec.db?_journal_mode=WAL&_foreign_keys=ON&cache=shared&_busy_timeout=5000"
		}
		dsn = dbPath
	}

	db = openDatabase(dsn)
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatal().Err(err).Msg("Database connection failed")
	}
	log.Info().Msg("Database connected")

	initScaledDB(db)
	initPgpool()
	go periodicPoolStats()

	initDB(db)
	seedDatabase(db)
	seedBVASDevices(db)
	initEMSTables(db)
	seedEMSData(db)
	initPhase7Tables(db)
	seedPhase7Data(db)
	initBiometricEngine(db)
	initBiometricAdvanced(db)
	initAIProxy()
	initBlockchainProduction(db)
	initProductionUpgrades(db)
	initMiddlewareTables(db)

	mwHub = initMiddlewareHub()

	// Seed search indices after hub is ready
	go seedSearchIndices(db)
	// Start background cache cleanup
	go cleanupExpiredCache()

	r := mux.NewRouter()

	// Health — deep checks
	r.HandleFunc("/healthz", handleDeepHealthCheck).Methods("GET")
	r.HandleFunc("/readiness", handleReadinessCheck).Methods("GET")
	r.HandleFunc("/db/metrics", handleDBMetrics).Methods("GET")
	r.HandleFunc("/db/pool", handleDBPoolStats).Methods("GET")

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
	r.HandleFunc("/ai/proxy/anomalies", handleAIProxy).Methods("GET")
	r.HandleFunc("/ai/fallback/anomalies", handleAIFallbackAnomalies).Methods("GET")

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

	// EMS - Voter Registration
	r.HandleFunc("/ems/voters", handleListVoters).Methods("GET")
	r.HandleFunc("/ems/voters/stats", handleVoterStats).Methods("GET")
	r.HandleFunc("/ems/voters/register", handleRegisterVoter).Methods("POST")
	r.HandleFunc("/ems/voters/{vin}", handleGetVoter).Methods("GET")
	r.HandleFunc("/ems/voters/{vin}/verify", handleVoterVerify).Methods("POST")
	r.HandleFunc("/ems/voters/{vin}/transfer", handleVoterTransfer).Methods("POST")
	r.HandleFunc("/ems/registration-centers", handleRegistrationCenters).Methods("GET")

	// EMS - Workflow Engine
	r.HandleFunc("/ems/workflows", handleListWorkflows).Methods("GET")
	r.HandleFunc("/ems/workflows", handleCreateWorkflow).Methods("POST")
	r.HandleFunc("/ems/workflows/{id}", handleGetWorkflow).Methods("GET")
	r.HandleFunc("/ems/workflows/{id}/advance", handleAdvanceWorkflow).Methods("POST")

	// EMS - BVAS Sync Engine
	r.HandleFunc("/ems/sync/submit", handleBVASSyncSubmit).Methods("POST")
	r.HandleFunc("/ems/sync/heartbeat", handleBVASHeartbeat).Methods("POST")
	r.HandleFunc("/ems/sync/stats", handleBVASSyncStats).Methods("GET")
	r.HandleFunc("/ems/sync/queue", handleBVASSyncQueue).Methods("GET")
	r.HandleFunc("/ems/sync/conflicts/{id}/resolve", handleBVASConflictResolve).Methods("POST")

	// EMS - Portal Integration Hub
	r.HandleFunc("/ems/portals", handleListPortals).Methods("GET")
	r.HandleFunc("/ems/portals/status", handlePortalHubStatus).Methods("GET")
	r.HandleFunc("/ems/portals/{id}", handleGetPortal).Methods("GET")
	r.HandleFunc("/ems/portals/{id}/sync", handlePortalSync).Methods("POST")
	r.HandleFunc("/ems/portals/sync-log", handlePortalSyncLog).Methods("GET")
	r.HandleFunc("/ems/portals/webhooks", handlePortalWebhooks).Methods("GET")

	// EMS - Data Validation Pipeline
	r.HandleFunc("/ems/validation/rules", handleListValidationRules).Methods("GET")
	r.HandleFunc("/ems/validation/validate", handleValidateEntity).Methods("POST")
	r.HandleFunc("/ems/validation/stats", handleValidationStats).Methods("GET")
	r.HandleFunc("/ems/validation/history", handleValidationHistory).Methods("GET")

	// EMS - Admin Console / Election Lifecycle
	r.HandleFunc("/ems/elections/{election_id}/lifecycle", handleElectionLifecycle).Methods("GET")
	r.HandleFunc("/ems/elections/{election_id}/transition", handleTransitionElection).Methods("POST")
	r.HandleFunc("/ems/staff", handleListStaffAssignments).Methods("GET")
	r.HandleFunc("/ems/staff", handleAssignStaff).Methods("POST")
	r.HandleFunc("/ems/materials", handleListMaterials).Methods("GET")
	r.HandleFunc("/ems/materials/{id}/dispatch", handleDispatchMaterial).Methods("PATCH")
	r.HandleFunc("/ems/materials/stats", handleMaterialStats).Methods("GET")
	r.HandleFunc("/ems/dashboard", handleEMSDashboard).Methods("GET")

	// Phase 7 - Enhanced Biometric Verification
	r.HandleFunc("/biometric/stats", handleBiometricStats).Methods("GET")
	r.HandleFunc("/biometric/verify", handleBiometricVerify).Methods("POST")
	r.HandleFunc("/biometric/profiles", handleBiometricProfiles).Methods("GET")
	r.HandleFunc("/biometric/abis/duplicates", handleABISDuplicates).Methods("GET")
	r.HandleFunc("/biometric/abis/{id}/resolve", handleABISResolve).Methods("POST")

	// Biometric Engine - Production-Grade
	r.HandleFunc("/biometric/engine/stats", handleBiometricEngineStats).Methods("GET")
	r.HandleFunc("/biometric/engine/enroll", handleABISEnroll).Methods("POST")
	r.HandleFunc("/biometric/engine/verify", handleABISVerify).Methods("POST")
	r.HandleFunc("/biometric/engine/verify-multimodal", handleMultiModalVerify).Methods("POST")
	r.HandleFunc("/biometric/engine/identify", handleABISIdentify).Methods("GET")
	r.HandleFunc("/biometric/engine/pad-check", handlePADCheck).Methods("POST")
	r.HandleFunc("/biometric/engine/pad-history", handlePADHistory).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/jobs", handleDedupJobs).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/start", handleDedupStart).Methods("POST")
	r.HandleFunc("/biometric/engine/dedup/{job_id}/candidates", handleDedupCandidates).Methods("GET")
	r.HandleFunc("/biometric/engine/dedup/resolve/{id}", handleDedupResolve).Methods("POST")
	r.HandleFunc("/biometric/engine/vault/stats", handleVaultStats).Methods("GET")
	r.HandleFunc("/biometric/engine/vault/rotate-key", handleVaultRotateKey).Methods("POST")
	r.HandleFunc("/biometric/engine/vault/audit", handleVaultAudit).Methods("GET")
	r.HandleFunc("/biometric/engine/devices", handleBVASDeviceCapabilities).Methods("GET")
	r.HandleFunc("/biometric/engine/devices/register", handleBVASRegisterDevice).Methods("POST")
	r.HandleFunc("/biometric/engine/capture-sessions", handleBVASCaptureSessions).Methods("GET")
	r.HandleFunc("/biometric/engine/pipeline", handleABISPipelineStatus).Methods("GET")
	r.HandleFunc("/biometric/engine/config", handleABISConfig).Methods("GET", "POST")
	r.HandleFunc("/biometric/engine/template-integrity", handleTemplateIntegrity).Methods("GET")

	// Biometric Advanced - 15 Improvements
	r.HandleFunc("/biometric/advanced/stats", handleAdvancedBiometricStats).Methods("GET")
	r.HandleFunc("/biometric/advanced/hsm/stats", handleHSMStats).Methods("GET")
	r.HandleFunc("/biometric/advanced/hsm/generate-key", handleHSMGenerateKey).Methods("POST")
	r.HandleFunc("/biometric/advanced/sdk/providers", handleSDKProviders).Methods("GET")
	r.HandleFunc("/biometric/advanced/aging", handleTemplateAging).Methods("GET")
	r.HandleFunc("/biometric/advanced/cancelable", handleCancelableStatus).Methods("GET")
	r.HandleFunc("/biometric/advanced/cancelable/revoke", handleCancelableRevoke).Methods("POST")
	r.HandleFunc("/biometric/advanced/threshold-tuning", handleThresholdTuning).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/distributed-dedup", handleDistributedDedup).Methods("POST")
	r.HandleFunc("/biometric/advanced/pad-models", handlePADModels).Methods("GET")
	r.HandleFunc("/biometric/advanced/pad-models/update", handlePADModelUpdate).Methods("POST")
	r.HandleFunc("/biometric/advanced/quality-gateway", handleQualityGateway).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/offline-queue", handleOfflineQueue).Methods("GET")
	r.HandleFunc("/biometric/advanced/offline-queue/sync", handleBioOfflineSync).Methods("POST")
	r.HandleFunc("/biometric/advanced/score-normalize", handleScoreNormalize).Methods("POST")
	r.HandleFunc("/biometric/advanced/score-cohorts", handleScoreCohorts).Methods("GET")
	r.HandleFunc("/biometric/advanced/nist-benchmark", handleNISTBenchmark).Methods("GET", "POST")
	r.HandleFunc("/biometric/advanced/audit/timeline", handleBioAuditTimeline).Methods("GET")
	r.HandleFunc("/biometric/advanced/audit/summary", handleBioAuditSummary).Methods("GET")
	r.HandleFunc("/biometric/advanced/kiosk/start", handleKioskStart).Methods("POST")
	r.HandleFunc("/biometric/advanced/kiosk/{session_id}/advance", handleKioskAdvance).Methods("POST")
	r.HandleFunc("/biometric/advanced/kiosk/sessions", handleKioskSessions).Methods("GET")
	r.HandleFunc("/biometric/advanced/multi-finger/enroll", handleMultiFingerEnroll).Methods("POST")
	r.HandleFunc("/biometric/advanced/multi-finger", handleMultiFingerStatus).Methods("GET")
	r.HandleFunc("/biometric/advanced/privacy-match", handlePrivacyMatch).Methods("POST")
	r.HandleFunc("/biometric/advanced/privacy-stats", handlePrivacyStats).Methods("GET")

	// Phase 7 - Blockchain-Enhanced Result Transmission
	r.HandleFunc("/blockchain/stats", handleBlockchainStats).Methods("GET")
	r.HandleFunc("/blockchain/chain", handleBlockchainChain).Methods("GET")
	r.HandleFunc("/blockchain/contracts", handleSmartContracts).Methods("GET")
	r.HandleFunc("/blockchain/verify/{result_id}", handleBlockchainVerifyResult).Methods("GET")
	r.HandleFunc("/blockchain/audit", handleBlockchainAuditTrail).Methods("GET")

	// Production Blockchain & Ledger
	r.HandleFunc("/blockchain/production/stats", handleBlockchainProductionStats).Methods("GET")
	r.HandleFunc("/blockchain/fabric/network", handleFabricNetworkStats).Methods("GET")
	r.HandleFunc("/blockchain/fabric/blocks", handleFabricBlocks).Methods("GET")
	r.HandleFunc("/blockchain/fabric/transactions", handleFabricTransactions).Methods("GET")
	r.HandleFunc("/blockchain/fabric/verify-chain", handleFabricVerifyChain).Methods("GET")
	r.HandleFunc("/blockchain/fabric/submit", handleFabricSubmitTx).Methods("POST")
	r.HandleFunc("/blockchain/chaincode/validate-result", handleChaincodeValidateResult).Methods("POST")
	r.HandleFunc("/blockchain/chaincode/aggregate", handleChaincodeAggregate).Methods("POST")
	r.HandleFunc("/blockchain/ipfs/stats", handleIPFSStats).Methods("GET")
	r.HandleFunc("/blockchain/ipfs/store", handleIPFSStore).Methods("POST")
	r.HandleFunc("/blockchain/ipfs/verify", handleIPFSVerify).Methods("GET")
	r.HandleFunc("/blockchain/ipfs/objects", handleIPFSObjects).Methods("GET")
	r.HandleFunc("/blockchain/ledger/stats", handlePersistentTBStats).Methods("GET")
	r.HandleFunc("/blockchain/ledger/accounts", handlePersistentTBAccounts).Methods("GET")
	r.HandleFunc("/blockchain/ledger/transfers", handlePersistentTBTransfers).Methods("GET")
	r.HandleFunc("/blockchain/ledger/transfer", handlePersistentTBCreateTransfer).Methods("POST")
	r.HandleFunc("/blockchain/ledger/transfer/post", handlePersistentTBPostTransfer).Methods("POST")
	r.HandleFunc("/blockchain/merkle/build", handleMerkleTreeBuild).Methods("POST")
	r.HandleFunc("/blockchain/merkle/trees", handleMerkleTreeList).Methods("GET")

	// Phase 7 - Training & Capacity Building
	r.HandleFunc("/training/courses", handleTrainingCourses).Methods("GET")
	r.HandleFunc("/training/stats", handleTrainingStats).Methods("GET")
	r.HandleFunc("/training/enrollments", handleTrainingEnrollments).Methods("GET")
	r.HandleFunc("/training/certificates", handleTrainingCertificates).Methods("GET")
	r.HandleFunc("/training/vr-scenarios", handleVRScenarios).Methods("GET")

	// Phase 7 - Stakeholder Engagement
	r.HandleFunc("/stakeholders/stats", handleStakeholderStats).Methods("GET")
	r.HandleFunc("/stakeholders", handleListStakeholders).Methods("GET")
	r.HandleFunc("/stakeholders/incidents", handleStakeholderIncidents).Methods("GET")
	r.HandleFunc("/stakeholders/grievances", handleListGrievances).Methods("GET")
	r.HandleFunc("/stakeholders/notifications", handlePushNotifications).Methods("GET")
	r.HandleFunc("/stakeholders/notifications", handleSendNotification).Methods("POST")

	// Phase 7 - AI Election Monitoring & Analytics
	r.HandleFunc("/ai-monitoring/dashboard", handleAIMonitoringDashboard).Methods("GET")
	r.HandleFunc("/ai-monitoring/predictions", handleAIPredictions).Methods("GET")
	r.HandleFunc("/ai-monitoring/sentiment", handleSentimentAnalysis).Methods("GET")
	r.HandleFunc("/ai-monitoring/misinformation", handleMisinformationAlerts).Methods("GET")
	r.HandleFunc("/ai-monitoring/security-threats", handleSecurityThreats).Methods("GET")
	r.HandleFunc("/ai-monitoring/cv-monitoring", handleCVMonitoring).Methods("GET")

	// Pgpool-II Infrastructure
	r.HandleFunc("/pgpool/status", handlePgpoolStatus).Methods("GET")
	r.HandleFunc("/pgpool/nodes", handlePgpoolNodes).Methods("GET")
	r.HandleFunc("/pgpool/health", handlePgpoolHealth).Methods("GET")
	r.HandleFunc("/pgpool/config", handlePgpoolConfig).Methods("GET")
	r.HandleFunc("/pgpool/metrics", handlePgpoolMetricsEndpoint).Methods("GET")
	r.HandleFunc("/pgpool/replication", handlePgpoolReplicationStatus).Methods("GET")
	r.HandleFunc("/pgpool/cache", handlePgpoolQueryCache).Methods("GET")
	r.HandleFunc("/pgpool/dashboard", handlePgpoolDashboard).Methods("GET")

	// Production Upgrades
	r.HandleFunc("/production/status", handleProductionUpgradeStatus).Methods("GET")
	r.HandleFunc("/production/hsm/stats", handleProductionHSMStats).Methods("GET")
	r.HandleFunc("/production/hsm/generate-key", handleProductionHSMGenerateKey).Methods("POST")
	r.HandleFunc("/production/hsm/sign", handleProductionHSMSign).Methods("POST")
	r.HandleFunc("/production/hsm/verify", handleProductionHSMVerify).Methods("POST")
	r.HandleFunc("/production/hsm/rotate", handleProductionHSMRotate).Methods("POST")
	r.HandleFunc("/production/sms/stats", handleProductionSMSGatewayStats).Methods("GET")
	r.HandleFunc("/production/sms/send", handleProductionSMSSend).Methods("POST")
	r.HandleFunc("/production/sms/delivery-log", handleProductionSMSDeliveryLog).Methods("GET")
	r.HandleFunc("/production/pad/stats", handleProductionPADStats).Methods("GET")
	r.HandleFunc("/production/pad/check", handleProductionPADCheck).Methods("POST")
	r.HandleFunc("/production/pad/attack-log", handleProductionPADAttackLog).Methods("GET")
	r.HandleFunc("/production/ipfs/stats", handleProductionIPFSStats).Methods("GET")
	r.HandleFunc("/production/ipfs/store", handleProductionIPFSStore).Methods("POST")
	r.HandleFunc("/production/ipfs/verify", handleProductionIPFSVerify).Methods("GET")
	r.HandleFunc("/production/fabric/stats", handleProductionFabricStats).Methods("GET")
	r.HandleFunc("/production/fabric/submit", handleProductionFabricSubmit).Methods("POST")
	r.HandleFunc("/production/fabric/verify-endorsements", handleProductionFabricVerifyEndorsements).Methods("GET")
	r.HandleFunc("/production/ledger/stats", handleProductionTBStats).Methods("GET")
	r.HandleFunc("/production/ledger/transfer", handleProductionTBCreateTransfer).Methods("POST")
	r.HandleFunc("/production/ledger/journal", handleProductionTBJournal).Methods("GET")

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

	// Mojaloop — 4-Phase Transaction Pattern
	r.HandleFunc("/middleware/mojaloop/status", handleMojaStatus).Methods("GET")
	r.HandleFunc("/middleware/mojaloop/parties", handleMojaPartyLookup).Methods("GET")
	r.HandleFunc("/middleware/mojaloop/quotes", handleMojaCreateQuote).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/transfers", handleMojaCreateTransfer).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/settlements", handleMojaSettle).Methods("POST")
	r.HandleFunc("/middleware/mojaloop/transactions", handleMojaTransactions).Methods("GET")

	// OpenSearch — Full-text Search
	r.HandleFunc("/middleware/opensearch/status", handleOpenSearchStatus).Methods("GET")
	r.HandleFunc("/middleware/opensearch/search", handleOpenSearchSearch).Methods("GET")
	r.HandleFunc("/middleware/opensearch/index", handleOpenSearchIndex).Methods("POST")
	r.HandleFunc("/middleware/opensearch/indices", handleOpenSearchIndices).Methods("GET")
	r.HandleFunc("/middleware/opensearch/stats", handleOpenSearchStats).Methods("GET")

	// OpenAppSec — WAF
	r.HandleFunc("/middleware/waf/status", handleWAFStatus).Methods("GET")
	r.HandleFunc("/middleware/waf/inspect", handleWAFInspect).Methods("POST")
	r.HandleFunc("/middleware/waf/threats", handleWAFThreatLog).Methods("GET")
	r.HandleFunc("/middleware/waf/stats", handleWAFStats).Methods("GET")
	r.HandleFunc("/middleware/waf/blocklist", handleWAFBlocklist).Methods("GET", "POST")

	// INEC Domain Logic — Form Validation, Collation, Reconciliation
	r.HandleFunc("/inec/ec8a/submit", handleSubmitEC8A).Methods("POST")
	r.HandleFunc("/inec/collation", handleHierarchicalCollation).Methods("GET")
	r.HandleFunc("/inec/reconciliation/ballot", handleBallotReconciliation).Methods("GET")
	r.HandleFunc("/inec/reconciliation/dual-ledger", handleDualLedgerReconciliation).Methods("GET")

	// Prometheus metrics endpoint
	r.Handle("/metrics", metricsHandler()).Methods("GET")

	// Middleware chain: request ID → access log → metrics → CORS → auth → security → WAF → rate limit → gzip → size limit
	handler := requestIDMiddleware(
		accessLogMiddleware(
			metricsMiddleware(
				corsProductionMiddleware(
					jwtAuthMiddleware(
						securityHeaders(
							wafMiddleware(
								requestSizeLimit(
									rateLimitMiddleware(
						gzipMiddleware(r))))))))))

	addr := ":8088"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Info().Str("addr", addr).Msg("INEC Go Backend listening")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
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
	limits := []struct {
		prefix string
		limit  int
	}{
		{"/auth/login", 5},
		{"/auth/register", 3},
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
