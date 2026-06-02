package main

import (
	"context"
	"database/sql"
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
	dbWriter             *sql.DB
	dbReader             *sql.DB
	dbMetrics            *DBMetrics
	stmtCache            *PreparedStmtCache
	slowQueryThresholdMs int64 = 100
)

type DBMetrics struct {
	mu            sync.RWMutex
	TotalReads    int64
	TotalWrites   int64
	SlowQueries   int64
	CacheHits     int64
	CacheMisses   int64
	ReplicaReads  int64
	PrimaryReads  int64
	AvgReadLatUs  int64
	AvgWriteLatUs int64
	readLatTotal  int64
	writeLatTotal int64
	PoolStats     map[string]interface{}
}

func newDBMetrics() *DBMetrics {
	return &DBMetrics{PoolStats: make(map[string]interface{})}
}

func (m *DBMetrics) recordRead(d time.Duration, replica bool) {
	atomic.AddInt64(&m.TotalReads, 1)
	us := d.Microseconds()
	atomic.AddInt64(&m.readLatTotal, us)
	total := atomic.LoadInt64(&m.TotalReads)
	if total > 0 {
		atomic.StoreInt64(&m.AvgReadLatUs, atomic.LoadInt64(&m.readLatTotal)/total)
	}
	if replica {
		atomic.AddInt64(&m.ReplicaReads, 1)
	} else {
		atomic.AddInt64(&m.PrimaryReads, 1)
	}
	if d.Milliseconds() > slowQueryThresholdMs {
		atomic.AddInt64(&m.SlowQueries, 1)
	}
}

func (m *DBMetrics) recordWrite(d time.Duration) {
	atomic.AddInt64(&m.TotalWrites, 1)
	us := d.Microseconds()
	atomic.AddInt64(&m.writeLatTotal, us)
	total := atomic.LoadInt64(&m.TotalWrites)
	if total > 0 {
		atomic.StoreInt64(&m.AvgWriteLatUs, atomic.LoadInt64(&m.writeLatTotal)/total)
	}
	if d.Milliseconds() > slowQueryThresholdMs {
		atomic.AddInt64(&m.SlowQueries, 1)
	}
}

func (m *DBMetrics) snapshot() map[string]interface{} {
	primary := dbWriter.Stats()
	result := map[string]interface{}{
		"total_reads":          atomic.LoadInt64(&m.TotalReads),
		"total_writes":         atomic.LoadInt64(&m.TotalWrites),
		"slow_queries":         atomic.LoadInt64(&m.SlowQueries),
		"cache_hits":           atomic.LoadInt64(&m.CacheHits),
		"cache_misses":         atomic.LoadInt64(&m.CacheMisses),
		"replica_reads":        atomic.LoadInt64(&m.ReplicaReads),
		"primary_reads":        atomic.LoadInt64(&m.PrimaryReads),
		"avg_read_latency_us":  atomic.LoadInt64(&m.AvgReadLatUs),
		"avg_write_latency_us": atomic.LoadInt64(&m.AvgWriteLatUs),
		"primary_pool": map[string]interface{}{
			"max_open":            primary.MaxOpenConnections,
			"open":                primary.OpenConnections,
			"in_use":              primary.InUse,
			"idle":                primary.Idle,
			"wait_count":          primary.WaitCount,
			"wait_duration_ms":    primary.WaitDuration.Milliseconds(),
			"max_idle_closed":     primary.MaxIdleClosed,
			"max_lifetime_closed": primary.MaxLifetimeClosed,
		},
	}
	if dbReader != nil && dbReader != dbWriter {
		replica := dbReader.Stats()
		result["replica_pool"] = map[string]interface{}{
			"max_open":         replica.MaxOpenConnections,
			"open":             replica.OpenConnections,
			"in_use":           replica.InUse,
			"idle":             replica.Idle,
			"wait_count":       replica.WaitCount,
			"wait_duration_ms": replica.WaitDuration.Milliseconds(),
		}
	}
	return result
}

type PreparedStmtCache struct {
	mu    sync.RWMutex
	stmts map[string]*sql.Stmt
	db    *sql.DB
}

func newPreparedStmtCache(database *sql.DB) *PreparedStmtCache {
	return &PreparedStmtCache{stmts: make(map[string]*sql.Stmt), db: database}
}

func (c *PreparedStmtCache) get(query string) *sql.Stmt {
	c.mu.RLock()
	s, ok := c.stmts[query]
	c.mu.RUnlock()
	if ok {
		atomic.AddInt64(&dbMetrics.CacheHits, 1)
		return s
	}
	atomic.AddInt64(&dbMetrics.CacheMisses, 1)
	stmt, err := c.db.Prepare(query)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	c.stmts[query] = stmt
	c.mu.Unlock()
	return stmt
}

func (c *PreparedStmtCache) size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.stmts)
}

func initScaledDB(primary *sql.DB) {
	dbMetrics = newDBMetrics()
	dbWriter = primary
	dbReader = primary

	if usePostgres {
		primary.SetMaxOpenConns(25)
		primary.SetMaxIdleConns(10)
		primary.SetConnMaxLifetime(5 * time.Minute)
		primary.SetConnMaxIdleTime(30 * time.Second)
	}

	replicaDSN := os.Getenv("DATABASE_REPLICA_URL")
	if replicaDSN != "" && usePostgres {
		replica := openPgCompat(replicaDSN)
		replica.SetMaxOpenConns(50)
		replica.SetMaxIdleConns(25)
		replica.SetConnMaxLifetime(5 * time.Minute)
		replica.SetConnMaxIdleTime(30 * time.Second)
		if err := replica.Ping(); err != nil {
			log.Warn().Err(err).Msg("Read replica connection failed, using primary for reads")
		} else {
			dbReader = replica
			log.Info().Msg("Read replica connected — reads will be routed to replica")
		}
	}

	stmtCache = newPreparedStmtCache(primary)

	if t := os.Getenv("SLOW_QUERY_THRESHOLD_MS"); t != "" {
		fmt.Sscanf(t, "%d", &slowQueryThresholdMs)
	}
	log.Info().Int64("slow_query_threshold_ms", slowQueryThresholdMs).Int("max_open", primary.Stats().MaxOpenConnections).Msg("DB scaling initialized")
}

func dbQueryCtx(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	target := dbReader
	isReplica := dbReader != dbWriter
	rows, err := target.QueryContext(ctx, query, args...)
	dbMetrics.recordRead(time.Since(start), isReplica)
	d := time.Since(start)
	if d.Milliseconds() > slowQueryThresholdMs {
		log.Warn().Int64("ms", d.Milliseconds()).Msg("SLOW_QUERY read")
	}
	return rows, err
}

func dbQueryRowCtx(ctx context.Context, query string, args ...interface{}) *sql.Row {
	start := time.Now()
	target := dbReader
	isReplica := dbReader != dbWriter
	row := target.QueryRowContext(ctx, query, args...)
	dbMetrics.recordRead(time.Since(start), isReplica)
	d := time.Since(start)
	if d.Milliseconds() > slowQueryThresholdMs {
		log.Warn().Int64("ms", d.Milliseconds()).Msg("SLOW_QUERY read_row")
	}
	return row
}

func dbExecCtx(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	start := time.Now()
	res, err := dbWriter.ExecContext(ctx, query, args...)
	dbMetrics.recordWrite(time.Since(start))
	d := time.Since(start)
	if d.Milliseconds() > slowQueryThresholdMs {
		log.Warn().Int64("ms", d.Milliseconds()).Msg("SLOW_QUERY write")
	}
	return res, err
}

func dbQueryTimeout(timeoutMs int, query string, args ...interface{}) (*sql.Rows, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	return dbQueryCtx(ctx, query, args...)
}

func dbExecTimeout(timeoutMs int, query string, args ...interface{}) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	return dbExecCtx(ctx, query, args...)
}

// dbExecLog executes a write query and logs errors (fire-and-forget with observability).
// Use for non-critical writes (audit logs, metrics, caches) where failure is acceptable but should be visible.
func dbExecLog(label string, query string, args ...interface{}) {
	_, err := db.Exec(query, args...)
	if err != nil {
		log.Error().Err(err).Str("op", label).Msg("db.Exec failed")
	}
}

func dbCachedQuery(query string, args ...interface{}) (*sql.Rows, error) {
	start := time.Now()
	stmt := stmtCache.get(query)
	if stmt != nil {
		rows, err := stmt.Query(args...)
		isReplica := dbReader != dbWriter
		dbMetrics.recordRead(time.Since(start), isReplica)
		return rows, err
	}
	return dbQueryCtx(context.Background(), query, args...)
}

func dbBatchInsert(table string, columns []string, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	start := time.Now()
	tx, err := dbWriter.Begin()
	if err != nil {
		return err
	}
	placeholders := make([]string, len(columns))
	for i := range columns {
		if usePostgres {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
		} else {
			placeholders[i] = "?"
		}
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(columns, ","), strings.Join(placeholders, ","))

	stmt, err := tx.Prepare(query)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.Exec(row...); err != nil {
			log.Warn().Err(err).Msg("batch insert warning")
		}
	}
	dbMetrics.recordWrite(time.Since(start))
	return tx.Commit()
}

func handleDBMetrics(w http.ResponseWriter, r *http.Request) {
	hasReplica := dbReader != nil && dbReader != dbWriter
	data := M{
		"engine":                  map[bool]string{true: "postgresql", false: "sqlite"}[usePostgres],
		"read_write_split":        hasReplica,
		"stmt_cache_size":         stmtCache.size(),
		"slow_query_threshold_ms": slowQueryThresholdMs,
		"metrics":                 dbMetrics.snapshot(),
		"scaling_patterns": M{
			"read_replica_routing":    hasReplica,
			"connection_pooling":      true,
			"prepared_stmt_cache":     true,
			"slow_query_logging":      true,
			"query_timeout_support":   true,
			"batch_insert_support":    true,
			"periodic_pool_reporting": true,
			"workload_isolation":      hasReplica,
		},
	}
	writeJSON(w, 200, data)
}

func handleDBPoolStats(w http.ResponseWriter, r *http.Request) {
	primary := dbWriter.Stats()
	data := M{
		"primary": M{
			"max_open_connections": primary.MaxOpenConnections,
			"open_connections":     primary.OpenConnections,
			"in_use":               primary.InUse,
			"idle":                 primary.Idle,
			"wait_count":           primary.WaitCount,
			"wait_duration_ms":     primary.WaitDuration.Milliseconds(),
			"max_idle_closed":      primary.MaxIdleClosed,
			"max_lifetime_closed":  primary.MaxLifetimeClosed,
			"max_idle_time_closed": primary.MaxIdleTimeClosed,
		},
	}
	if dbReader != nil && dbReader != dbWriter {
		replica := dbReader.Stats()
		data["replica"] = M{
			"max_open_connections": replica.MaxOpenConnections,
			"open_connections":     replica.OpenConnections,
			"in_use":               replica.InUse,
			"idle":                 replica.Idle,
			"wait_count":           replica.WaitCount,
			"wait_duration_ms":     replica.WaitDuration.Milliseconds(),
		}
	}
	writeJSON(w, 200, data)
}

func periodicPoolStats() {
	for {
		time.Sleep(60 * time.Second)
		stats := dbWriter.Stats()
		log.Info().Int("open", stats.OpenConnections).Int("in_use", stats.InUse).Int("idle", stats.Idle).Msg("DB_POOL primary")
		if dbReader != nil && dbReader != dbWriter {
			rs := dbReader.Stats()
			log.Info().Int("open", rs.OpenConnections).Int("in_use", rs.InUse).Int("idle", rs.Idle).Msg("DB_POOL replica")
		}
	}
}
