// R4-08 regression: MaxLoginAttempts / LockoutDuration enforcement.
// Requires R4_TEST_DB (lib/pq DSN for a scratch PostgreSQL database).
package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

func r4LockoutDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("R4_TEST_DB")
	if dsn == "" {
		t.Skip("R4_TEST_DB not set — skipping PG-backed lockout regression test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestR408_LoginLockoutEnforced(t *testing.T) {
	db := r4LockoutDB(t)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY, username TEXT UNIQUE NOT NULL, password_hash TEXT,
		full_name TEXT, role TEXT, staff_id TEXT, state_code TEXT,
		is_active INTEGER DEFAULT 1, login_count INTEGER DEFAULT 0)`); err != nil {
		t.Fatalf("ddl: %v", err)
	}

	cfg := DefaultConfig([]byte(strings.Repeat("s", 40)))
	cfg.MaxLoginAttempts = 3
	cfg.LockoutDuration = 15 * time.Minute
	svc := NewService(db, cfg)
	if err := svc.InitTables(context.Background()); err != nil {
		t.Fatalf("InitTables: %v", err)
	}

	username := fmt.Sprintf("r408-%d", time.Now().UnixNano())
	hash, _ := svc.HashPassword("correct-password")
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, full_name, role, is_active)
		VALUES ($1, $2, 'R4 Lockout', 'admin', 1)`, username, hash); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// 3 wrong passwords → account locks at the threshold.
	for i := 1; i <= 3; i++ {
		_, err := svc.Login(context.Background(), username, "wrong-password")
		if err == nil {
			t.Fatalf("attempt %d: expected error", i)
		}
		if i < 3 && !strings.Contains(err.Error(), "invalid credentials") {
			t.Fatalf("attempt %d: unexpected error %v", i, err)
		}
		if i == 3 && !strings.Contains(err.Error(), "locked") {
			t.Fatalf("attempt %d: expected lockout error, got %v", i, err)
		}
	}

	// Correct password while locked → still rejected.
	if _, err := svc.Login(context.Background(), username, "correct-password"); err == nil ||
		!strings.Contains(err.Error(), "locked") {
		t.Fatalf("locked account must reject even correct password, got %v", err)
	}

	// Lock expires → correct password succeeds AND clears the counter.
	if _, err := db.Exec(`UPDATE users SET locked_until = NOW() - INTERVAL '1 minute' WHERE username=$1`, username); err != nil {
		t.Fatalf("expire lock: %v", err)
	}
	if _, err := svc.Login(context.Background(), username, "correct-password"); err != nil {
		t.Fatalf("login after lock expiry must succeed: %v", err)
	}
	var attempts int
	var lockedUntil sql.NullTime
	if err := db.QueryRow(`SELECT failed_login_attempts, locked_until FROM users WHERE username=$1`, username).
		Scan(&attempts, &lockedUntil); err != nil {
		t.Fatalf("read state: %v", err)
	}
	if attempts != 0 || lockedUntil.Valid {
		t.Fatalf("R4-08: lockout state not cleared on success (attempts=%d, locked_until=%v)", attempts, lockedUntil)
	}
}
