package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"github.com/rs/zerolog/log"
	"strings"

	"github.com/lib/pq"
)

var usePostgres bool

func convertPlaceholders(query string) string {
	n := 0
	inSingleQuote := false
	var b strings.Builder
	b.Grow(len(query) + 32)
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			inSingleQuote = !inSingleQuote
		}
		if ch == '?' && !inSingleQuote {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}

type pgCompatConnector struct {
	dsn string
}

func (c *pgCompatConnector) Connect(_ context.Context) (driver.Conn, error) {
	d := &pq.Driver{}
	conn, err := d.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &pgCompatConn{inner: conn}, nil
}

func (c *pgCompatConnector) Driver() driver.Driver {
	return &pq.Driver{}
}

type pgCompatConn struct {
	inner driver.Conn
}

func (c *pgCompatConn) Prepare(query string) (driver.Stmt, error) {
	query = convertPlaceholders(query)
	query = convertRuntimeSQL(query)
	return c.inner.Prepare(query)
}

func convertRuntimeSQL(query string) string {
	if strings.Contains(query, "INSERT OR IGNORE INTO") {
		query = strings.ReplaceAll(query, "INSERT OR IGNORE INTO", "INSERT INTO")
		// Append ON CONFLICT DO NOTHING before any RETURNING clause or at end
		if idx := strings.Index(strings.ToUpper(query), "RETURNING"); idx > 0 {
			query = query[:idx] + "ON CONFLICT DO NOTHING " + query[idx:]
		} else {
			query += " ON CONFLICT DO NOTHING"
		}
	}
	if strings.Contains(query, "INSERT OR REPLACE INTO") {
		// PostgreSQL equivalent: use ON CONFLICT ... DO UPDATE SET
		// For simplicity, convert to plain INSERT with ON CONFLICT DO NOTHING
		query = strings.ReplaceAll(query, "INSERT OR REPLACE INTO", "INSERT INTO")
		if idx := strings.Index(strings.ToUpper(query), "RETURNING"); idx > 0 {
			query = query[:idx] + "ON CONFLICT DO NOTHING " + query[idx:]
		} else {
			query += " ON CONFLICT DO NOTHING"
		}
	}
	if strings.Contains(query, "REPLACE INTO") && !strings.Contains(query, "INSERT OR REPLACE INTO") {
		query = strings.ReplaceAll(query, "REPLACE INTO", "INSERT INTO")
		query += " ON CONFLICT DO NOTHING"
	}
	// Convert datetime('now') → NOW()
	query = strings.ReplaceAll(query, "datetime('now')", "NOW()")
	query = strings.ReplaceAll(query, "AUTOINCREMENT", "")
	query = strings.ReplaceAll(query, "INTEGER PRIMARY KEY ", "SERIAL PRIMARY KEY ")
	return query
}

func (c *pgCompatConn) Close() error {
	return c.inner.Close()
}

func (c *pgCompatConn) Begin() (driver.Tx, error) {
	return c.inner.Begin()
}

// Implement ExecerContext so db.Exec also goes through our SQL conversion
func (c *pgCompatConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	query = convertPlaceholders(query)
	query = convertRuntimeSQL(query)
	if execer, ok := c.inner.(interface {
		ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error)
	}); ok {
		return execer.ExecContext(ctx, query, args)
	}
	// Fallback: prepare + exec
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	return stmt.Exec(vals)
}

// Implement QueryerContext so db.Query also goes through our SQL conversion
func (c *pgCompatConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	query = convertPlaceholders(query)
	query = convertRuntimeSQL(query)
	if queryer, ok := c.inner.(interface {
		QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error)
	}); ok {
		return queryer.QueryContext(ctx, query, args)
	}
	// Fallback: prepare + query
	stmt, err := c.inner.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	vals := make([]driver.Value, len(args))
	for i, a := range args {
		vals[i] = a.Value
	}
	return stmt.Query(vals)
}

func openPgCompat(dsn string) *sql.DB {
	return sql.OpenDB(&pgCompatConnector{dsn: dsn})
}

func openDatabase(dsn string) *sql.DB {
	usePostgres = true
	log.Info().Msg("Using PostgreSQL database")
	return openPgCompat(dsn)
}

func convertDDLForPostgres(schema string) string {
	s := strings.ReplaceAll(schema, "INTEGER PRIMARY KEY AUTOINCREMENT", "SERIAL PRIMARY KEY")
	s = strings.ReplaceAll(s, "BLOB", "BYTEA")
	s = strings.ReplaceAll(s, "BOOLEAN DEFAULT 1", "BOOLEAN DEFAULT TRUE")
	s = strings.ReplaceAll(s, "BOOLEAN DEFAULT 0", "BOOLEAN DEFAULT FALSE")
	// Convert INSERT OR IGNORE → INSERT ... ON CONFLICT DO NOTHING
	// Find each INSERT OR IGNORE statement and append ON CONFLICT DO NOTHING before the semicolon
	for strings.Contains(s, "INSERT OR IGNORE INTO") {
		idx := strings.Index(s, "INSERT OR IGNORE INTO")
		// Find the terminating semicolon for this statement
		semiIdx := strings.Index(s[idx:], ";")
		if semiIdx < 0 {
			s = strings.Replace(s, "INSERT OR IGNORE INTO", "INSERT INTO", 1)
			break
		}
		semiIdx += idx
		stmt := s[idx:semiIdx]
		stmt = strings.Replace(stmt, "INSERT OR IGNORE INTO", "INSERT INTO", 1)
		stmt += " ON CONFLICT DO NOTHING"
		s = s[:idx] + stmt + s[semiIdx:]
	}
	return s
}

func execMulti(database *sql.DB, multiSQL string) {
	multiSQL = convertDDLForPostgres(multiSQL)
	stmts := strings.Split(multiSQL, ";")
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := database.Exec(s); err != nil {
			log.Warn().Err(err).Msg("execMulti warning")
		}
	}
}

type dbQuerier interface {
	QueryRow(string, ...interface{}) *sql.Row
	Exec(string, ...interface{}) (sql.Result, error)
}

func insertReturningID(querier dbQuerier, query string, args ...interface{}) int64 {
	q := strings.TrimRight(query, " \t\n;")
	if !strings.Contains(strings.ToUpper(q), "RETURNING") {
		q += " RETURNING id"
	}
	var id int64
	querier.QueryRow(q, args...).Scan(&id)
	return id
}
