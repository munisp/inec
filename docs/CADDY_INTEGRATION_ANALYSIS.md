# Caddy Server: Value Proposition and Integration Architecture for INEC

## Executive Summary

[Caddy](https://github.com/caddyserver/caddy) is a production-grade, open-source web server and reverse proxy written in Go. It is the **only major web server that enables HTTPS automatically and by default**, and it is architecturally complementary — not competitive — with the INEC platform's existing APISIX, OpenAppSec, and service mesh stack. Caddy occupies a distinct layer in the traffic hierarchy that none of the other components currently fill: the **public-facing edge TLS terminator and ingress controller**.

---

## 1. What Value Does Caddy Add to the INEC Platform?

The INEC platform currently has:
- **APISIX** on ports 9080/9443 — API gateway for routing, JWT auth, rate limiting, and plugin orchestration
- **OpenAppSec** — ML-based WAF (currently config-only, no running container in `docker-compose.yml`)
- **Go backend** on port 8088 — application server
- **Keycloak** — identity provider
- **No edge TLS terminator** — APISIX is exposed directly to the internet

Caddy fills the critical gap as the **outermost layer** of the traffic stack. Here is the value it adds across six dimensions:

### 1.1 Automatic TLS Certificate Management (Critical for Election Infrastructure)

Caddy's flagship feature, powered by its embedded [CertMagic](https://github.com/caddyserver/certmagic) library, is fully automated HTTPS with zero configuration. For an election management system like INEC, this is not a convenience — it is a **compliance and trust requirement**.

| Capability | Caddy | Current INEC Stack |
|---|---|---|
| Automatic Let's Encrypt / ZeroSSL certificates | ✅ Built-in | ❌ Manual / missing |
| Auto-renewal before expiry | ✅ Computed per-cert lifetime | ❌ Requires cron jobs |
| OCSP stapling | ✅ Automatic | ❌ Not configured |
| TLS 1.3 + Ed25519 / ECDSA P-256 | ✅ Default | ⚠️ Requires APISIX config |
| Mutual TLS (mTLS) for service-to-service | ✅ Native | ❌ Not implemented |
| Session ticket key rotation (STEK) | ✅ Academically cited | ❌ Not implemented |
| HTTP/3 (QUIC) | ✅ Default | ❌ Not available in APISIX 3.7 |
| PCI DSS / NIST / HIPAA compliance | ✅ Certified defaults | ⚠️ Manual configuration required |

For a national election platform, a lapsed TLS certificate is a catastrophic event. Caddy eliminates this risk entirely.

### 1.2 Edge Ingress Layer (Fills the Missing Tier)

The current architecture exposes APISIX directly to the internet. This is architecturally incorrect for a high-security platform. The correct pattern is a three-tier ingress:

```
Internet → [Caddy: TLS + WAF + HTTP/3] → [APISIX: API Gateway + Auth] → [Go Backend]
```

Caddy acts as the **edge proxy** that:
- Terminates TLS before traffic reaches APISIX
- Enforces HTTPS redirects for all HTTP traffic
- Handles HTTP/3 (QUIC) for low-latency mobile access (critical for field officers on 4G/5G networks in Nigeria)
- Provides IP-level geoblocking and rate limiting before traffic hits the API gateway
- Strips sensitive headers before forwarding to APISIX

### 1.3 Coraza WAF Integration (Replaces/Augments OpenAppSec)

OpenAppSec currently has **no running container** in `docker-compose.yml` — only a `policy.yaml` config file exists. It is not actively protecting the platform. Caddy solves this immediately through the [coraza-caddy](https://github.com/corazawaf/coraza-caddy) plugin, which embeds the **OWASP Coraza WAF** with full **OWASP Core Rule Set (CRS)** support directly into Caddy.

| WAF Capability | OpenAppSec (current state) | Caddy + Coraza |
|---|---|---|
| Active in docker-compose | ❌ Not running | ✅ Embedded in Caddy |
| OWASP CRS rules | ✅ (when running) | ✅ Full CRS v4 support |
| SQL injection blocking | ✅ (when running) | ✅ |
| XSS blocking | ✅ (when running) | ✅ |
| ML-based threat detection | ✅ (OpenAppSec's strength) | ❌ Rule-based only |
| Zero additional container | ❌ Needs separate agent | ✅ Embedded in Caddy process |
| ModSecurity syntax compatibility | ❌ | ✅ 100% compatible |

**Recommendation**: Run both. Caddy + Coraza provides the OWASP CRS rule-based layer. OpenAppSec (once its container is added) provides the ML-based anomaly detection layer. They are complementary, not redundant.

### 1.4 Built-in Observability and Structured Logging

Caddy emits **structured JSON access logs** with zero-allocation performance. Every request includes: remote IP, latency, status code, request headers, response headers, and TLS metadata. These logs feed directly into the platform's existing Prometheus/Grafana observability stack.

```json
{
  "ts": 1721160000.123,
  "request": { "remote_ip": "41.58.x.x", "method": "POST", "uri": "/auth/login" },
  "status": 200,
  "latency": 0.045,
  "tls": { "version": "tls1.3", "cipher_suite": "TLS_AES_256_GCM_SHA384" }
}
```

Caddy also supports **OpenTelemetry tracing** natively, which integrates with the platform's existing OTel setup in the Go backend.

### 1.5 Zero-Downtime Configuration Reloads

During an active election, the platform **cannot go down**. Caddy supports live configuration reloads via its JSON Admin API (`POST /load`) with **zero dropped connections**. This means:
- New routes can be added without restarting
- TLS certificates can be rotated without downtime
- Rate limiting rules can be updated in real-time during an election

This is architecturally superior to NGINX, which requires a `SIGHUP` that can drop in-flight connections.

### 1.6 Written in Go — Native to the Platform

The INEC platform's backend is written in Go. Caddy is also written in Go. This means:
- The same team can read, debug, and extend Caddy's source code
- Caddy can be embedded directly into the Go backend as a library if needed
- Custom Caddy modules can be written in Go to implement INEC-specific logic (e.g., election day traffic shaping, biometric endpoint routing)
- No Python/Lua/C dependencies — the entire edge layer is memory-safe Go

---

## 2. How Caddy Integrates with Each Platform Component

### 2.1 Caddy ↔ APISIX

**Relationship**: Caddy is the **upstream edge proxy**; APISIX is the **downstream API gateway**.

```
[Client] ──HTTPS/HTTP3──► [Caddy :443]
                               │
                    TLS terminated, WAF inspected
                               │
                          ──HTTP──► [APISIX :9080]
                                         │
                              JWT auth, rate limit, routing
                                         │
                                    ──HTTP──► [Go Backend :8088]
```

Caddy's `reverse_proxy` directive forwards all traffic to APISIX. APISIX never needs to be exposed to the internet — it listens only on the internal Docker network. This eliminates the current security gap where APISIX ports 9080/9443 are directly bound to the host.

**Caddyfile configuration:**
```caddyfile
inec.gov.ng {
    # Automatic TLS from Let's Encrypt
    tls admin@inec.gov.ng

    # OWASP Coraza WAF
    coraza_waf {
        load_owasp_crs
        directives `
            Include /etc/caddy/coraza.conf
            Include @owasp_crs/*.conf
            SecRuleEngine On
        `
    }

    # Forward to APISIX (internal network only)
    reverse_proxy apisix:9080 {
        header_up X-Forwarded-For {remote_host}
        header_up X-Real-IP {remote_host}
        header_up X-Forwarded-Proto {scheme}
        # Strip internal headers
        header_up -X-Internal-Token
    }

    # Security headers
    header {
        Strict-Transport-Security "max-age=31536000; includeSubDomains; preload"
        X-Frame-Options "DENY"
        X-Content-Type-Options "nosniff"
        Content-Security-Policy "default-src 'self'"
        Referrer-Policy "strict-origin-when-cross-origin"
        -Server
    }

    # Structured access logs → Prometheus/Loki
    log {
        output file /var/log/caddy/access.log
        format json
    }
}

# Redirect HTTP to HTTPS
http://inec.gov.ng {
    redir https://{host}{uri} permanent
}
```

### 2.2 Caddy ↔ OpenAppSec

**Relationship**: Caddy + Coraza provides the **rule-based WAF layer**; OpenAppSec provides the **ML-based anomaly detection layer**. They operate at different levels.

**Option A — Caddy before OpenAppSec (Recommended):**
```
[Client] → [Caddy + Coraza: OWASP CRS rules] → [OpenAppSec agent: ML detection] → [APISIX] → [Backend]
```

OpenAppSec currently supports NGINX as its attachment point. The recommended integration is to run OpenAppSec as a sidecar to APISIX (which runs on OpenResty/NGINX), while Caddy handles the outer TLS and OWASP CRS layer.

**Option B — Caddy replaces NGINX in OpenAppSec stack:**
OpenAppSec does not yet have a native Caddy attachment. However, Caddy + Coraza provides equivalent OWASP CRS coverage. The ML-based detection of OpenAppSec can be preserved by running it as a standalone inspection service that Caddy forwards request metadata to via a Dapr sidecar binding.

### 2.3 Caddy ↔ Keycloak

Caddy can proxy the Keycloak admin console and OIDC endpoints with automatic TLS, replacing the need for a separate NGINX reverse proxy in front of Keycloak.

```caddyfile
auth.inec.gov.ng {
    tls admin@inec.gov.ng
    reverse_proxy keycloak:8080 {
        header_up Host {upstream_hostport}
    }
}
```

Caddy also supports **OpenID Connect authentication** natively via the `caddy-auth-portal` plugin, which can act as a secondary authentication layer in front of the admin dashboard.

### 2.4 Caddy ↔ Dapr

Caddy can route traffic to Dapr-enabled services using Dapr's HTTP API. The Dapr sidecar runs alongside each service, and Caddy routes to the Dapr port:

```caddyfile
/dapr/* {
    reverse_proxy localhost:3500  # Dapr HTTP API port
}
```

This allows Caddy to trigger Dapr pub/sub events, invoke service methods, and manage state — all through standard HTTP routing rules.

### 2.5 Caddy ↔ Temporal (Workflow UI)

Caddy can proxy the Temporal Web UI with TLS and authentication:

```caddyfile
workflows.inec.gov.ng {
    tls admin@inec.gov.ng
    # Restrict to internal INEC IP ranges only
    @internal remote_ip 10.0.0.0/8 172.16.0.0/12
    handle @internal {
        reverse_proxy temporal-ui:8080
    }
    respond "Access Denied" 403
}
```

### 2.6 Caddy ↔ Redis (Certificate Storage)

Caddy supports Redis as a **certificate storage backend** for clustered deployments. This is critical for the INEC platform which runs multiple backend replicas — all Caddy instances share the same certificate pool via Redis, eliminating certificate duplication and rate-limit exhaustion from ACME providers.

```json
{
    "storage": {
        "module": "redis",
        "address": "redis:6379",
        "password": "${REDIS_PASSWORD}",
        "key_prefix": "caddy_certs"
    }
}
```

### 2.7 Caddy ↔ Fluvio / Kafka (Access Log Streaming)

Caddy's structured JSON access logs can be streamed directly to Fluvio topics via a log processor sidecar, feeding the platform's real-time analytics lakehouse:

```
Caddy access.log → Fluvio topic: caddy.access → Lakehouse (Delta Lake) → Grafana
```

This gives the platform real-time visibility into traffic patterns, attack attempts, and geographic distribution of requests during election day.

---

## 3. Recommended Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│                         PUBLIC INTERNET                             │
└──────────────────────────┬──────────────────────────────────────────┘
                           │ HTTPS (443) / HTTP/3 (QUIC)
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│  CADDY (Edge Layer)                                                 │
│  • Automatic TLS (Let's Encrypt / ZeroSSL)                         │
│  • HTTP/3 (QUIC) for mobile field officers                         │
│  • Coraza WAF (OWASP CRS v4 — SQLi, XSS, RCE, SSRF)              │
│  • Security headers (HSTS, CSP, X-Frame-Options)                   │
│  • IP geoblocking (Nigeria-only for admin endpoints)               │
│  • Structured JSON access logs → Fluvio → Lakehouse                │
│  • Redis-backed cert storage (clustered deployment)                 │
└──────────────────────────┬──────────────────────────────────────────┘
                           │ HTTP (internal Docker network)
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│  OPENAPPSEC (ML WAF Layer)                                         │
│  • Machine-learning anomaly detection                               │
│  • Behavioral analysis (detects novel attacks Coraza rules miss)   │
│  • Attached to APISIX/OpenResty                                     │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│  APISIX (API Gateway Layer)                                        │
│  • JWT authentication (Keycloak JWKS)                              │
│  • Rate limiting per route                                          │
│  • Request routing to microservices                                 │
│  • CORS enforcement                                                 │
│  • Response caching (proxy-cache plugin)                            │
└──────────────────────────┬──────────────────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────────────────┐
│  GO BACKEND + MICROSERVICES                                        │
│  • Election management, results, biometrics                         │
│  • Dapr sidecars for pub/sub                                        │
│  • Permify for fine-grained authorization                           │
│  • TigerBeetle for ledger operations                                │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 4. Implementation Plan

To integrate Caddy into the INEC platform, the following changes are required:

### Step 1: Add Caddy to `docker-compose.yml`

```yaml
caddy:
  image: caddy:2-alpine
  # Use xcaddy build with coraza plugin for WAF support
  # build: ./caddy
  ports:
    - "80:80"
    - "443:443"
    - "443:443/udp"  # HTTP/3 QUIC
  volumes:
    - ./config/caddy/Caddyfile:/etc/caddy/Caddyfile
    - ./config/caddy/coraza.conf:/etc/caddy/coraza.conf
    - caddy_data:/data          # TLS certificates (persistent)
    - caddy_config:/config      # Caddy config cache
  environment:
    - CADDY_ADMIN=0.0.0.0:2019
  networks:
    - inec-network
  depends_on:
    - apisix
  restart: unless-stopped

volumes:
  caddy_data:
  caddy_config:
```

### Step 2: Remove Public Port Exposure from APISIX

```yaml
# BEFORE (insecure — APISIX exposed to internet)
apisix:
  ports:
    - "9080:9080"
    - "9443:9443"

# AFTER (secure — APISIX only reachable from Caddy on internal network)
apisix:
  expose:
    - "9080"
  # No ports: — not accessible from outside Docker network
```

### Step 3: Build Custom Caddy with Coraza Plugin

```dockerfile
# caddy/Dockerfile
FROM caddy:2-builder AS builder
RUN xcaddy build \
    --with github.com/corazawaf/coraza-caddy/v2 \
    --with github.com/greenpau/caddy-security \
    --with github.com/mholt/caddy-ratelimit

FROM caddy:2-alpine
COPY --from=builder /usr/bin/caddy /usr/bin/caddy
```

### Step 4: Configure Caddyfile

See Section 2.1 for the complete Caddyfile configuration.

---

## 5. Summary of Value Added

| Dimension | Without Caddy | With Caddy |
|---|---|---|
| TLS certificate management | Manual / missing | Fully automated, zero-downtime renewal |
| HTTP/3 (QUIC) support | Not available | Enabled by default |
| OWASP WAF | OpenAppSec (not running) | Coraza CRS + OpenAppSec (both active) |
| Security headers | Manual APISIX config | Enforced at edge for all traffic |
| Zero-downtime config reload | APISIX requires restart | Live reload via Admin API |
| Certificate storage for clusters | Not implemented | Redis-backed shared cert pool |
| Access log streaming | Not implemented | JSON logs → Fluvio → Lakehouse |
| APISIX internet exposure | Direct (security risk) | Hidden behind Caddy |
| mTLS for service-to-service | Not implemented | Native Caddy mTLS |
| Memory safety | APISIX uses C/LuaJIT | Go — memory-safe by design |

Caddy is a **force multiplier** for the INEC platform. It is not a replacement for APISIX or OpenAppSec — it is the missing outermost layer that makes the entire stack production-grade, secure by default, and compliant with international election infrastructure standards.
