package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"strings"

	"github.com/lib/pq"
	_ "modernc.org/sqlite"
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
	return c.inner.Prepare(convertPlaceholders(query))
}

func (c *pgCompatConn) Close() error {
	return c.inner.Close()
}

func (c *pgCompatConn) Begin() (driver.Tx, error) {
	return c.inner.Begin()
}

func openPgCompat(dsn string) *sql.DB {
	return sql.OpenDB(&pgCompatConnector{dsn: dsn})
}

func openDatabase(dsn string) *sql.DB {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		usePostgres = true
		log.Println("Using PostgreSQL database")
		return openPgCompat(dsn)
	}
	usePostgres = false
	log.Println("Using SQLite database (fallback)")
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatal("SQLite connection failed: ", err)
	}
	d.Exec("PRAGMA journal_mode=WAL")
	d.Exec("PRAGMA foreign_keys=ON")
	d.Exec("PRAGMA busy_timeout=5000")
	return d
}

func convertDDLForSQLite(schema string) string {
	s := strings.ReplaceAll(schema, "SERIAL PRIMARY KEY", "INTEGER PRIMARY KEY AUTOINCREMENT")
	s = strings.ReplaceAll(s, "BYTEA", "BLOB")
	return s
}

func execMulti(database *sql.DB, multiSQL string) {
	if !usePostgres {
		multiSQL = convertDDLForSQLite(multiSQL)
		if _, err := database.Exec(multiSQL); err != nil {
			log.Printf("execMulti(sqlite) warning: %v", err)
		}
		return
	}
	stmts := strings.Split(multiSQL, ";")
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := database.Exec(s); err != nil {
			log.Printf("execMulti warning: %v (stmt: %.80s...)", err, s)
		}
	}
}

type dbQuerier interface {
	QueryRow(string, ...interface{}) *sql.Row
	Exec(string, ...interface{}) (sql.Result, error)
}

func insertReturningID(querier dbQuerier, query string, args ...interface{}) int64 {
	if !usePostgres {
		res, err := querier.Exec(query, args...)
		if err != nil {
			return 0
		}
		id, _ := res.LastInsertId()
		return id
	}
	q := strings.TrimRight(query, " \t\n;")
	if !strings.Contains(strings.ToUpper(q), "RETURNING") {
		q += " RETURNING id"
	}
	var id int64
	querier.QueryRow(q, args...).Scan(&id)
	return id
}

func sqlNow() string {
	if usePostgres {
		return "NOW()"
	}
	return "datetime('now')"
}

func sqlInterval(expr string) string {
	if usePostgres {
		return "NOW() - INTERVAL '" + expr + "'"
	}
	return "datetime('now', '-" + expr + "')"
}

func sqlEpoch(col string) string {
	if usePostgres {
		return "COALESCE(EXTRACT(EPOCH FROM " + col + ")::INTEGER, 0)"
	}
	return "COALESCE(CAST(strftime('%s', " + col + ") AS INTEGER), 0)"
}
