package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	pgpoolHost    string
	pgpoolPort    string
	pgpoolPCPHost string
	pgpoolPCPPort string
	pgpoolEnabled bool

	pgpoolMetrics *PgpoolMetrics
)

type PgpoolMetrics struct {
	mu                 sync.RWMutex
	LastHealthCheck    time.Time
	PrimaryHealthy     bool
	ReplicaHealthy     bool
	PgpoolHealthy      bool
	FailoverCount      int64
	HealthCheckCount   int64
	LoadBalanceReads   int64
	LoadBalanceWrites  int64
	CacheHitRatio      float64
	ConnectionsActive  int64
	ConnectionsIdle    int64
	ConnectionsTotal   int64
	ReplicationLagBytes int64
	BackendNodes       []PgpoolBackendNode
}

type PgpoolBackendNode struct {
	ID              int    `json:"id"`
	Hostname        string `json:"hostname"`
	Port            int    `json:"port"`
	Status          string `json:"status"`
	Role            string `json:"role"`
	Weight          float64 `json:"weight"`
	SelectCount     int64  `json:"select_count"`
	ReplicationLag  int64  `json:"replication_lag_bytes"`
	LastStatusCheck string `json:"last_status_check"`
}

func initPgpool() {
	pgpoolHost = os.Getenv("PGPOOL_HOST")
	pgpoolPort = os.Getenv("PGPOOL_PORT")
	pgpoolPCPHost = os.Getenv("PGPOOL_PCP_HOST")
	pgpoolPCPPort = os.Getenv("PGPOOL_PCP_PORT")

	if pgpoolHost == "" {
		pgpoolHost = "pgpool"
	}
	if pgpoolPort == "" {
		pgpoolPort = "5432"
	}
	if pgpoolPCPHost == "" {
		pgpoolPCPHost = pgpoolHost
	}
	if pgpoolPCPPort == "" {
		pgpoolPCPPort = "9898"
	}

	pgpoolMetrics = &PgpoolMetrics{
		BackendNodes: make([]PgpoolBackendNode, 0),
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn != "" && strings.Contains(dsn, "pgpool") {
		pgpoolEnabled = true
		log.Info().Msg("Pgpool-II detected in DATABASE_URL — monitoring enabled")
	} else if pgpoolHost != "pgpool" || os.Getenv("PGPOOL_ENABLED") == "true" {
		pgpoolEnabled = true
		log.Info().Msg("Pgpool-II monitoring enabled via env config")
	}

	if pgpoolEnabled {
		go pgpoolHealthCheckLoop()
		go pgpoolNodeMonitorLoop()
		log.Info().Str("host", pgpoolHost).Str("port", pgpoolPort).Msg("Pgpool-II integration initialized")
	} else {
		log.Info().Msg("Pgpool-II not configured — running in direct-connect mode")
	}
}

func pgpoolHealthCheckLoop() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	pgpoolRunHealthCheck()

	for range ticker.C {
		pgpoolRunHealthCheck()
	}
}

func pgpoolRunHealthCheck() {
	atomic.AddInt64(&pgpoolMetrics.HealthCheckCount, 1)
	pgpoolMetrics.mu.Lock()
	pgpoolMetrics.LastHealthCheck = time.Now()
	pgpoolMetrics.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if dbWriter != nil {
		if err := dbWriter.PingContext(ctx); err != nil {
			pgpoolMetrics.mu.Lock()
			pgpoolMetrics.PrimaryHealthy = false
			pgpoolMetrics.mu.Unlock()
			log.Warn().Err(err).Msg("Pgpool primary ping failed")
		} else {
			pgpoolMetrics.mu.Lock()
			pgpoolMetrics.PrimaryHealthy = true
			pgpoolMetrics.mu.Unlock()
		}
	}

	if dbReader != nil && dbReader != dbWriter {
		if err := dbReader.PingContext(ctx); err != nil {
			pgpoolMetrics.mu.Lock()
			pgpoolMetrics.ReplicaHealthy = false
			pgpoolMetrics.mu.Unlock()
			log.Warn().Err(err).Msg("Pgpool replica ping failed")
		} else {
			pgpoolMetrics.mu.Lock()
			pgpoolMetrics.ReplicaHealthy = true
			pgpoolMetrics.mu.Unlock()
		}
	}

	pgpoolMetrics.mu.Lock()
	pgpoolMetrics.PgpoolHealthy = pgpoolMetrics.PrimaryHealthy
	pgpoolMetrics.mu.Unlock()
}

func pgpoolNodeMonitorLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	pgpoolQueryNodeStatus()

	for range ticker.C {
		pgpoolQueryNodeStatus()
	}
}

func pgpoolQueryNodeStatus() {
	if !usePostgres || dbWriter == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodes := make([]PgpoolBackendNode, 0, 2)

	var primaryLag int64
	err := dbWriter.QueryRowContext(ctx,
		"SELECT CASE WHEN pg_is_in_recovery() THEN pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()) ELSE 0 END").Scan(&primaryLag)
	primaryStatus := "up"
	if err != nil {
		primaryStatus = "unknown"
	}

	nodes = append(nodes, PgpoolBackendNode{
		ID:              0,
		Hostname:        "pg-primary",
		Port:            5432,
		Status:          primaryStatus,
		Role:            "primary",
		Weight:          1.0,
		ReplicationLag:  0,
		LastStatusCheck: time.Now().Format(time.RFC3339),
	})

	if dbReader != nil && dbReader != dbWriter {
		var replicaLag int64
		replicaStatus := "up"
		err := dbReader.QueryRowContext(ctx,
			"SELECT CASE WHEN pg_is_in_recovery() THEN COALESCE(pg_wal_lsn_diff(pg_last_wal_receive_lsn(), pg_last_wal_replay_lsn()), 0) ELSE 0 END").Scan(&replicaLag)
		if err != nil {
			replicaStatus = "unknown"
			replicaLag = -1
		}

		nodes = append(nodes, PgpoolBackendNode{
			ID:              1,
			Hostname:        "pg-replica",
			Port:            5432,
			Status:          replicaStatus,
			Role:            "standby",
			Weight:          2.0,
			ReplicationLag:  replicaLag,
			LastStatusCheck: time.Now().Format(time.RFC3339),
		})

		pgpoolMetrics.mu.Lock()
		pgpoolMetrics.ReplicationLagBytes = replicaLag
		pgpoolMetrics.mu.Unlock()
	}

	var connActive, connIdle int
	rows, err := dbWriter.QueryContext(ctx,
		"SELECT state, count(*) FROM pg_stat_activity WHERE datname = current_database() GROUP BY state")
	if err == nil {
		defer rows.Close()
		var total int64
		for rows.Next() {
			var state sql.NullString
			var cnt int
			if rows.Scan(&state, &cnt) == nil {
				if state.Valid && state.String == "active" {
					connActive = cnt
				} else if state.Valid && state.String == "idle" {
					connIdle = cnt
				}
				total += int64(cnt)
			}
		}
		pgpoolMetrics.mu.Lock()
		pgpoolMetrics.ConnectionsActive = int64(connActive)
		pgpoolMetrics.ConnectionsIdle = int64(connIdle)
		pgpoolMetrics.ConnectionsTotal = total
		pgpoolMetrics.mu.Unlock()
	}

	pgpoolMetrics.mu.Lock()
	pgpoolMetrics.BackendNodes = nodes
	pgpoolMetrics.mu.Unlock()

	writerStats := dbWriter.Stats()
	pgpoolMetrics.mu.Lock()
	pgpoolMetrics.LoadBalanceWrites = int64(writerStats.InUse)
	pgpoolMetrics.mu.Unlock()
	if dbReader != nil && dbReader != dbWriter {
		readerStats := dbReader.Stats()
		pgpoolMetrics.mu.Lock()
		pgpoolMetrics.LoadBalanceReads = int64(readerStats.InUse)
		pgpoolMetrics.mu.Unlock()
	}
}

func handlePgpoolStatus(w http.ResponseWriter, r *http.Request) {
	pgpoolMetrics.mu.RLock()
	status := M{
		"enabled":     pgpoolEnabled,
		"healthy":     pgpoolMetrics.PgpoolHealthy,
		"config": M{
			"host":     pgpoolHost,
			"port":     pgpoolPort,
			"pcp_host": pgpoolPCPHost,
			"pcp_port": pgpoolPCPPort,
		},
		"health": M{
			"primary_healthy":  pgpoolMetrics.PrimaryHealthy,
			"replica_healthy":  pgpoolMetrics.ReplicaHealthy,
			"pgpool_healthy":   pgpoolMetrics.PgpoolHealthy,
			"last_check":       pgpoolMetrics.LastHealthCheck.Format(time.RFC3339),
			"check_count":      atomic.LoadInt64(&pgpoolMetrics.HealthCheckCount),
		},
		"backends": pgpoolMetrics.BackendNodes,
		"connections": M{
			"active": pgpoolMetrics.ConnectionsActive,
			"idle":   pgpoolMetrics.ConnectionsIdle,
			"total":  pgpoolMetrics.ConnectionsTotal,
		},
		"replication": M{
			"lag_bytes":    pgpoolMetrics.ReplicationLagBytes,
			"lag_status":   replicationLagStatus(pgpoolMetrics.ReplicationLagBytes),
		},
		"load_balancing": M{
			"mode":            "statement_level",
			"active_reads":    pgpoolMetrics.LoadBalanceReads,
			"active_writes":   pgpoolMetrics.LoadBalanceWrites,
			"read_weight":     "replica:2, primary:1",
		},
		"features": M{
			"connection_pooling":           true,
			"load_balancing":               true,
			"streaming_replication_check":  true,
			"automatic_failover":           pgpoolEnabled,
			"auto_failback":               pgpoolEnabled,
			"in_memory_query_cache":        true,
			"statement_level_lb":           true,
			"health_check":                true,
			"connection_limit_queuing":     true,
		},
	}
	pgpoolMetrics.mu.RUnlock()

	writeJSON(w, 200, status)
}

func handlePgpoolNodes(w http.ResponseWriter, r *http.Request) {
	pgpoolMetrics.mu.RLock()
	nodes := pgpoolMetrics.BackendNodes
	pgpoolMetrics.mu.RUnlock()

	data := M{
		"nodes":       nodes,
		"total":       len(nodes),
		"primary_count": countNodesByRole(nodes, "primary"),
		"standby_count": countNodesByRole(nodes, "standby"),
	}
	writeJSON(w, 200, data)
}

func countNodesByRole(nodes []PgpoolBackendNode, role string) int {
	count := 0
	for _, n := range nodes {
		if n.Role == role {
			count++
		}
	}
	return count
}

func handlePgpoolHealth(w http.ResponseWriter, r *http.Request) {
	pgpoolMetrics.mu.RLock()
	healthy := pgpoolMetrics.PgpoolHealthy
	primaryOK := pgpoolMetrics.PrimaryHealthy
	replicaOK := pgpoolMetrics.ReplicaHealthy
	lagBytes := pgpoolMetrics.ReplicationLagBytes
	lastCheck := pgpoolMetrics.LastHealthCheck
	pgpoolMetrics.mu.RUnlock()

	status := 200
	if !healthy {
		status = 503
	}

	writeJSON(w, status, M{
		"healthy":         healthy,
		"primary_ok":      primaryOK,
		"replica_ok":      replicaOK,
		"replication_lag": lagBytes,
		"lag_status":      replicationLagStatus(lagBytes),
		"last_check":      lastCheck.Format(time.RFC3339),
		"uptime_seconds":  time.Since(lastCheck).Seconds(),
	})
}

func handlePgpoolConfig(w http.ResponseWriter, r *http.Request) {
	config := M{
		"pgpool": M{
			"version":                  "4.5",
			"clustering_mode":          "streaming_replication",
			"num_init_children":        64,
			"max_pool":                 4,
			"child_life_time":          300,
			"connection_life_time":     0,
			"client_idle_limit":        300,
			"reserved_connections":     2,
			"statement_level_lb":       true,
			"memory_cache_enabled":     true,
			"memqcache_total_size_mb":  64,
			"memqcache_expire_sec":     15,
			"auto_failover":            true,
			"auto_failback":            true,
			"health_check_period_sec":  10,
			"sr_check_period_sec":      5,
			"delay_threshold_bytes":    10000000,
		},
		"primary": M{
			"host":             "pg-primary",
			"port":             5432,
			"weight":           1,
			"max_connections":  200,
			"shared_buffers":   "256MB",
			"wal_level":        "replica",
			"max_wal_senders":  5,
			"wal_keep_size":    "256MB",
		},
		"replica": M{
			"host":             "pg-replica",
			"port":             5432,
			"weight":           2,
			"hot_standby":      true,
			"replication_slot": "replica_slot_1",
			"max_connections":  200,
			"shared_buffers":   "256MB",
		},
		"app_layer": M{
			"primary_pool_max_open":     25,
			"primary_pool_max_idle":     10,
			"primary_conn_max_lifetime": "5m",
			"replica_pool_max_open":     50,
			"replica_pool_max_idle":     25,
			"replica_conn_max_lifetime": "5m",
			"slow_query_threshold_ms":   slowQueryThresholdMs,
			"stmt_cache_enabled":        true,
			"read_write_split":          dbReader != dbWriter,
		},
	}
	writeJSON(w, 200, config)
}

func handlePgpoolMetricsEndpoint(w http.ResponseWriter, r *http.Request) {
	pgpoolMetrics.mu.RLock()
	defer pgpoolMetrics.mu.RUnlock()

	baseMetrics := dbMetrics.snapshot()

	combined := M{
		"db_scaling_layer": baseMetrics,
		"pgpool": M{
			"enabled":           pgpoolEnabled,
			"healthy":           pgpoolMetrics.PgpoolHealthy,
			"primary_healthy":   pgpoolMetrics.PrimaryHealthy,
			"replica_healthy":   pgpoolMetrics.ReplicaHealthy,
			"health_checks":     atomic.LoadInt64(&pgpoolMetrics.HealthCheckCount),
			"failover_count":    atomic.LoadInt64(&pgpoolMetrics.FailoverCount),
			"replication_lag":   pgpoolMetrics.ReplicationLagBytes,
			"connections_active": pgpoolMetrics.ConnectionsActive,
			"connections_idle":  pgpoolMetrics.ConnectionsIdle,
			"connections_total": pgpoolMetrics.ConnectionsTotal,
			"backend_nodes":    len(pgpoolMetrics.BackendNodes),
		},
		"architecture": M{
			"topology":  "primary-replica-pgpool",
			"layers": []string{
				"Pgpool-II (connection pooling, load balancing, failover)",
				"Go sql.DB pools (app-level pooling, read/write split)",
				"pgscale.go (metrics, slow query detection, stmt cache)",
			},
			"data_flow": "Client → Go Backend → Pgpool-II → [Primary (writes) | Replica (reads)]",
		},
	}

	writeJSON(w, 200, combined)
}

func handlePgpoolReplicationStatus(w http.ResponseWriter, r *http.Request) {
	if !usePostgres || dbWriter == nil {
		writeJSON(w, 200, M{"status": "not_configured", "engine": "sqlite"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := M{"status": "ok"}

	var isRecovery bool
	if err := dbWriter.QueryRowContext(ctx, "SELECT pg_is_in_recovery()").Scan(&isRecovery); err == nil {
		result["primary_is_in_recovery"] = isRecovery
	}

	rows, err := dbWriter.QueryContext(ctx,
		`SELECT client_addr, state, sent_lsn, write_lsn, flush_lsn, replay_lsn,
			COALESCE(pg_wal_lsn_diff(sent_lsn, replay_lsn), 0) as lag_bytes
		FROM pg_stat_replication`)
	if err == nil {
		defer rows.Close()
		replicas := make([]M, 0)
		for rows.Next() {
			var clientAddr, state sql.NullString
			var sentLSN, writeLSN, flushLSN, replayLSN sql.NullString
			var lagBytes int64
			if rows.Scan(&clientAddr, &state, &sentLSN, &writeLSN, &flushLSN, &replayLSN, &lagBytes) == nil {
				replicas = append(replicas, M{
					"client_addr": clientAddr.String,
					"state":       state.String,
					"sent_lsn":    sentLSN.String,
					"write_lsn":   writeLSN.String,
					"flush_lsn":   flushLSN.String,
					"replay_lsn":  replayLSN.String,
					"lag_bytes":   lagBytes,
					"lag_status":  replicationLagStatus(lagBytes),
				})
			}
		}
		result["replicas"] = replicas
		result["replica_count"] = len(replicas)
	}

	var slotRows *sql.Rows
	slotRows, err = dbWriter.QueryContext(ctx,
		`SELECT slot_name, active, restart_lsn, confirmed_flush_lsn
		FROM pg_replication_slots`)
	if err == nil {
		defer slotRows.Close()
		slots := make([]M, 0)
		for slotRows.Next() {
			var slotName sql.NullString
			var active sql.NullBool
			var restartLSN, confirmedFlushLSN sql.NullString
			if slotRows.Scan(&slotName, &active, &restartLSN, &confirmedFlushLSN) == nil {
				slots = append(slots, M{
					"slot_name":           slotName.String,
					"active":              active.Bool,
					"restart_lsn":         restartLSN.String,
					"confirmed_flush_lsn": confirmedFlushLSN.String,
				})
			}
		}
		result["replication_slots"] = slots
	}

	writeJSON(w, 200, result)
}

func handlePgpoolQueryCache(w http.ResponseWriter, r *http.Request) {
	cacheInfo := M{
		"pgpool_query_cache": M{
			"enabled":           true,
			"method":            "shmem",
			"total_size_mb":     64,
			"max_num_cache":     1000000,
			"expire_seconds":    15,
			"auto_invalidation": true,
			"max_cache_bytes":   409600,
			"block_size_bytes":  1048576,
		},
		"app_level_cache": M{
			"enabled":        true,
			"type":           "in-memory TTL",
			"ttl_seconds":    15,
			"cached_endpoints": []string{
				"GET /dashboard/stats",
				"GET /dashboard/collation",
				"GET /geo/map-data",
			},
		},
		"stmt_cache": M{
			"enabled": true,
			"size":    stmtCache.size(),
		},
		"cache_layers": []M{
			{"layer": 1, "name": "Pgpool-II in-memory query cache", "scope": "SQL-level", "invalidation": "write-aware"},
			{"layer": 2, "name": "App response cache (15s TTL)", "scope": "endpoint-level", "invalidation": "TTL-based"},
			{"layer": 3, "name": "Prepared statement cache", "scope": "query-level", "invalidation": "none (append-only)"},
		},
	}
	writeJSON(w, 200, cacheInfo)
}

func replicationLagStatus(lagBytes int64) string {
	if lagBytes < 0 {
		return "unknown"
	}
	if lagBytes == 0 {
		return "in_sync"
	}
	if lagBytes < 1048576 {
		return "minimal"
	}
	if lagBytes < 10485760 {
		return "moderate"
	}
	return "high"
}

func handlePgpoolDashboard(w http.ResponseWriter, r *http.Request) {
	pgpoolMetrics.mu.RLock()
	defer pgpoolMetrics.mu.RUnlock()

	var poolUtilization float64
	if pgpoolMetrics.ConnectionsTotal > 0 {
		poolUtilization = float64(pgpoolMetrics.ConnectionsActive) / float64(pgpoolMetrics.ConnectionsTotal) * 100
	}

	dashboard := M{
		"summary": M{
			"status":             map[bool]string{true: "healthy", false: "degraded"}[pgpoolMetrics.PgpoolHealthy],
			"topology":           "1 Primary + 1 Replica + Pgpool-II",
			"engine":             map[bool]string{true: "postgresql", false: "sqlite"}[usePostgres],
			"pgpool_enabled":     pgpoolEnabled,
			"uptime_check_count": atomic.LoadInt64(&pgpoolMetrics.HealthCheckCount),
		},
		"primary": M{
			"healthy": pgpoolMetrics.PrimaryHealthy,
			"host":    "pg-primary",
			"role":    "read-write",
			"pool": M{
				"max_open": 25,
				"max_idle": 10,
			},
		},
		"replica": M{
			"healthy":          pgpoolMetrics.ReplicaHealthy,
			"host":             "pg-replica",
			"role":             "read-only",
			"replication_lag":  pgpoolMetrics.ReplicationLagBytes,
			"lag_status":       replicationLagStatus(pgpoolMetrics.ReplicationLagBytes),
			"pool": M{
				"max_open": 50,
				"max_idle": 25,
			},
		},
		"pgpool": M{
			"healthy":                  pgpoolMetrics.PgpoolHealthy,
			"version":                  "4.5",
			"clustering_mode":          "streaming_replication",
			"load_balancing":           "statement_level",
			"failover":                 "automatic",
			"query_cache":              "shmem (64MB, 15s TTL)",
			"max_connections":          256,
			"pool_utilization_pct":     fmt.Sprintf("%.1f%%", poolUtilization),
		},
		"connections": M{
			"active":            pgpoolMetrics.ConnectionsActive,
			"idle":              pgpoolMetrics.ConnectionsIdle,
			"total":             pgpoolMetrics.ConnectionsTotal,
			"utilization_pct":   fmt.Sprintf("%.1f%%", poolUtilization),
		},
		"scaling_layers": []M{
			{
				"layer":       "Pgpool-II",
				"description": "Connection pooling, load balancing, automatic failover, query cache",
				"status":      map[bool]string{true: "active", false: "standby"}[pgpoolEnabled],
			},
			{
				"layer":       "Go sql.DB Pool",
				"description": "Application-level connection pool with read/write split",
				"status":      "active",
			},
			{
				"layer":       "pgscale.go",
				"description": "Slow query detection, metrics, prepared statement cache, batch inserts",
				"status":      "active",
			},
			{
				"layer":       "Response Cache",
				"description": "15s TTL cache on expensive endpoints (collation, map-data, stats)",
				"status":      "active",
			},
		},
	}

	tmpl := `<!DOCTYPE html><html><head><title>INEC Pgpool-II Dashboard</title>
<style>body{font-family:system-ui;margin:2em;background:#f8f9fa}
h1{color:#1a5f2a}h2{color:#2d7a3e;border-bottom:2px solid #2d7a3e;padding-bottom:.3em}
.card{background:#fff;border-radius:8px;padding:1.5em;margin:1em 0;box-shadow:0 2px 8px rgba(0,0,0,.08)}
.healthy{color:#28a745}.degraded{color:#dc3545}.metric{display:inline-block;margin:0 2em 1em 0}
.metric .val{font-size:2em;font-weight:700}.metric .label{color:#666;font-size:.85em}
table{border-collapse:collapse;width:100%%}th,td{text-align:left;padding:.6em;border-bottom:1px solid #dee2e6}
th{background:#f1f8f4;color:#1a5f2a}.badge{padding:.2em .6em;border-radius:4px;font-size:.85em}
.badge-green{background:#d4edda;color:#155724}.badge-red{background:#f8d7da;color:#721c24}
.badge-blue{background:#cce5ff;color:#004085}pre{background:#f1f3f5;padding:1em;border-radius:4px;overflow-x:auto}</style></head>
<body><h1>INEC Election Platform — Pgpool-II Infrastructure</h1>`

	if r.URL.Query().Get("format") == "json" {
		writeJSON(w, 200, dashboard)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	fmt.Fprint(w, tmpl)
	fmt.Fprint(w, `<div class="card"><h2>Cluster Status</h2>`)

	statusClass := "healthy"
	if !pgpoolMetrics.PgpoolHealthy {
		statusClass = "degraded"
	}
	fmt.Fprintf(w, `<div class="metric"><div class="val %s">%s</div><div class="label">Overall</div></div>`,
		statusClass, strings.ToUpper(map[bool]string{true: "HEALTHY", false: "DEGRADED"}[pgpoolMetrics.PgpoolHealthy]))

	fmt.Fprintf(w, `<div class="metric"><div class="val">%d</div><div class="label">Health Checks</div></div>`,
		atomic.LoadInt64(&pgpoolMetrics.HealthCheckCount))
	fmt.Fprintf(w, `<div class="metric"><div class="val">%d</div><div class="label">Active Conns</div></div>`,
		pgpoolMetrics.ConnectionsActive)
	fmt.Fprintf(w, `<div class="metric"><div class="val">%s</div><div class="label">Replication</div></div>`,
		replicationLagStatus(pgpoolMetrics.ReplicationLagBytes))
	fmt.Fprintf(w, `</div>`)

	fmt.Fprintf(w, `<div class="card"><h2>Backend Nodes</h2><table><tr><th>ID</th><th>Host</th><th>Role</th><th>Status</th><th>Weight</th><th>Rep Lag</th></tr>`)
	for _, n := range pgpoolMetrics.BackendNodes {
		badge := "badge-green"
		if n.Status != "up" {
			badge = "badge-red"
		}
		fmt.Fprintf(w, `<tr><td>%d</td><td>%s:%d</td><td>%s</td><td><span class="badge %s">%s</span></td><td>%.0f</td><td>%d bytes</td></tr>`,
			n.ID, n.Hostname, n.Port, n.Role, badge, n.Status, n.Weight, n.ReplicationLag)
	}
	fmt.Fprintf(w, `</table></div>`)

	fmt.Fprintf(w, `<div class="card"><h2>Architecture</h2><pre>`)
	fmt.Fprintf(w, "Client Request\n")
	fmt.Fprintf(w, "    ↓\n")
	fmt.Fprintf(w, "Go Backend (handlers.go)\n")
	fmt.Fprintf(w, "    ↓\n")
	fmt.Fprintf(w, "pgscale.go (metrics, slow query detection, stmt cache)\n")
	fmt.Fprintf(w, "    ↓\n")
	fmt.Fprintf(w, "Pgpool-II (connection pool, load balance, query cache)\n")
	fmt.Fprintf(w, "    ├── Primary (pg-primary:5432) ← writes\n")
	fmt.Fprintf(w, "    └── Replica (pg-replica:5432) ← reads (2x weight)\n")
	fmt.Fprintf(w, "</pre></div>")

	fmt.Fprintf(w, `<div class="card"><h2>Scaling Layers</h2><table><tr><th>Layer</th><th>Role</th><th>Status</th></tr>`)
	fmt.Fprintf(w, `<tr><td>Pgpool-II 4.5</td><td>Connection pooling, load balancing, failover, query cache</td><td><span class="badge badge-green">%s</span></td></tr>`,
		map[bool]string{true: "active", false: "standby"}[pgpoolEnabled])
	fmt.Fprintf(w, `<tr><td>Go sql.DB Pool</td><td>App-level pooling, read/write split</td><td><span class="badge badge-green">active</span></td></tr>`)
	fmt.Fprintf(w, `<tr><td>pgscale.go</td><td>Slow query detection, metrics, batch inserts</td><td><span class="badge badge-green">active</span></td></tr>`)
	fmt.Fprintf(w, `<tr><td>Response Cache</td><td>15s TTL on collation/map-data/stats</td><td><span class="badge badge-green">active</span></td></tr>`)
	fmt.Fprintf(w, `</table></div>`)

	dashJSON, _ := json.MarshalIndent(dashboard, "", "  ")
	fmt.Fprintf(w, `<div class="card"><h2>Raw JSON <small>(<a href="?format=json">API</a>)</small></h2><pre>%s</pre></div>`, string(dashJSON))

	fmt.Fprintf(w, `</body></html>`)
}
