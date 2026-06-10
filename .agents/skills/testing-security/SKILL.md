---
name: testing-inec-security
description: Test security audit fixes on the INEC election platform Go backend. Use when verifying CSRF, SQL injection, path traversal, cookie security, or other security vulnerability fixes.
---

# Testing INEC Security Fixes

## Prerequisites

- PostgreSQL running on localhost:5432 (default DSN: `postgresql://ngapp:ngapp@localhost:5432/ngapp?sslmode=disable`)
- Go 1.22+ installed at `/usr/local/go/bin/go`
- Docker services optional (for real middleware testing)

## Devin Secrets Needed

None required for dev-mode testing. The backend auto-generates an ephemeral JWT_SECRET if not set.

## Starting the Go Backend

```bash
cd /home/ubuntu/repos/inec/inec-go-backend
export PATH="/usr/local/go/bin:$PATH"
APP_ENV=development go run . > /tmp/server.log 2>&1 &
# Wait ~5s for startup, then check:
tail -5 /tmp/server.log  # Should show "INEC Go Backend listening" on :8088
```

## Running the Full Test Suite

```bash
cd /home/ubuntu/repos/inec/inec-go-backend
export PATH="/usr/local/go/bin:$PATH"
go test -count=1 -v ./...
# Expect 330+ tests, 0 failures
```

## Adversarial Security Tests (curl)

### 1. CSRF Token Validation

The CSRF fix requires server-side token store validation. To test:

```bash
# Register + login
curl -s -c /tmp/cookies.txt -X POST http://localhost:8088/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"test_'$(date +%s)'","password":"LongPassword123","full_name":"Test","role":"public"}'

# Get valid CSRF token
CSRF=$(curl -s -b /tmp/cookies.txt http://localhost:8088/auth/csrf-token | python3 -c "import sys,json; print(json.load(sys.stdin)['csrf_token'])")

# Test: missing token → 403 "CSRF token missing"
curl -s -b /tmp/cookies.txt -X POST http://localhost:8088/elections -H "Content-Type: application/json" -d '{"name":"test"}'

# Test: forged token → 403 "CSRF token invalid or expired"
curl -s -b /tmp/cookies.txt -X POST http://localhost:8088/elections -H "X-CSRF-Token: forged-abc" -H "Content-Type: application/json" -d '{"name":"test"}'

# Test: valid token → passes CSRF (gets role-level 403, not CSRF 403)
curl -s -b /tmp/cookies.txt -X POST http://localhost:8088/elections -H "X-CSRF-Token: $CSRF" -H "Content-Type: application/json" -d '{"name":"test"}'

# Test: Bearer auth → CSRF bypassed entirely
curl -s -X POST http://localhost:8088/elections -H "Authorization: Bearer <jwt>" -H "Content-Type: application/json" -d '{"name":"test"}'
```

Key assertion: forged token MUST return 403 "CSRF token invalid or expired", not 200. If this passes, the old bug (existence-only check) is still present.

### 2. Cookie SameSite Verification

```bash
curl -s -D - -X POST http://localhost:8088/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"...","password":"..."}' | grep Set-Cookie
# Both cookies should have: HttpOnly; SameSite=Lax
```

### 3. SQL Injection Check (Lakehouse)

Verify no `fmt.Sprintf("'%v'", v)` patterns exist:
```bash
grep -c "Sprintf.*'%v'" inec-go-backend/mw_lakehouse.go  # Should be 0
```

### 4. Path Traversal Check (Uploads)

Verify all upload handlers sanitize filenames:
```bash
grep -n "filepath.Base.*ReplaceAll.*\.\." inec-go-backend/observer_monitoring.go
grep -n "filepath.Base.*ReplaceAll.*\.\." inec-go-backend/geo_advanced.go
grep -n "filepath.Clean" inec-go-backend/document_ai.go
```

## Role Requirements

- Self-registration only allows `public` and `observer` roles
- `/auth/logout`, `/observer/reports` POST, `/elections` POST require `admin`/`presiding_officer`/`collation_officer`
- To test admin-only endpoints at runtime, you'd need to promote a user via direct DB update or use the `/admin/promote` endpoint (requires existing admin)

## Known Limitations

- CI may fail due to GitHub billing issues ("recent account payments have failed") — this is not a code issue
- Go binary location: `/usr/local/go/bin/go` (not in default PATH for nohup/background shells)
- Some endpoints need admin role which can't be self-registered; use code verification for those
- Kafka backoff testing requires simulating Kafka failures (not possible with embedded in-memory bus)
