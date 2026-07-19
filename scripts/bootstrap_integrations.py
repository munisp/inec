#!/usr/bin/env python3
"""Bootstrap APISIX integration state for the INEC platform.

APISIX is deployed in traditional mode (config.yaml -> deployment.etcd), so
routes live in etcd and must be pushed through the Admin API — the standalone
conf/apisix.yaml file is ignored in this mode. This script waits for the
Admin API, applies config/apisix/routes.json idempotently (PUT by id), then
verifies every object is readable back.

Usage:
    python3 scripts/bootstrap_integrations.py [--verify-only] [--timeout 120]

Environment:
    APISIX_ADMIN_URL   default http://localhost:9180
    APISIX_ADMIN_KEY   default: APISIX_API_KEY, then the compose dev key
    APISIX_API_KEY     fallback for APISIX_ADMIN_KEY
    APISIX_DATA_URL    default http://localhost:9080 (data-plane smoke check)
    ROUTES_FILE        default <repo>/config/apisix/routes.json

Stdlib only — safe to run inside any container or CI job.
"""
import json
import os
import sys
import time
import urllib.error
import urllib.request

DEV_KEY = "edd1c9f034335f136f87ad84b625c8f1"
ADMIN_URL = os.environ.get("APISIX_ADMIN_URL", "http://localhost:9180").rstrip("/")
API_KEY = os.environ.get("APISIX_ADMIN_KEY") or os.environ.get("APISIX_API_KEY") or DEV_KEY
DATA_URL = os.environ.get("APISIX_DATA_URL", "http://localhost:9080").rstrip("/")
ROUTES_FILE = os.environ.get(
    "ROUTES_FILE",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "config", "apisix", "routes.json"),
)


def api(method, path, body=None, timeout=10):
    """Call the APISIX Admin API. Returns (status, parsed_json_or_text)."""
    url = ADMIN_URL + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("X-API-KEY", API_KEY)
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode()
            try:
                return resp.status, json.loads(raw)
            except json.JSONDecodeError:
                return resp.status, raw
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(errors="replace")
    except Exception as e:  # connection refused, timeout, ...
        return 0, str(e)


def wait_for_admin(timeout_s):
    deadline = time.time() + timeout_s
    attempt = 0
    while time.time() < deadline:
        attempt += 1
        status, _ = api("GET", "/apisix/admin/routes", timeout=5)
        if status == 200:
            print(f"[bootstrap] admin API reachable at {ADMIN_URL} (attempt {attempt})")
            return True
        print(f"[bootstrap] waiting for admin API at {ADMIN_URL} (attempt {attempt}, status={status})")
        time.sleep(2)
    return False


def apply_table(table):
    """PUT every upstream/route/global_rule by id. Returns list of applied ids."""
    applied = []
    for upstream in table.get("upstreams", []):
        uid = upstream["id"]
        status, body = api("PUT", f"/apisix/admin/upstreams/{uid}", upstream)
        if status not in (200, 201):
            raise RuntimeError(f"upstream {uid}: HTTP {status}: {body}")
        applied.append(f"upstream/{uid}")
        print(f"[bootstrap] applied upstream {uid}")
    for route in table.get("routes", []):
        rid = route["id"]
        status, body = api("PUT", f"/apisix/admin/routes/{rid}", route)
        if status not in (200, 201):
            raise RuntimeError(f"route {rid}: HTTP {status}: {body}")
        applied.append(f"route/{rid}")
        print(f"[bootstrap] applied route {rid}")
    for rule in table.get("global_rules", []):
        gid = rule["id"]
        status, body = api("PUT", f"/apisix/admin/global_rules/{gid}", rule)
        if status not in (200, 201):
            raise RuntimeError(f"global_rule {gid}: HTTP {status}: {body}")
        applied.append(f"global_rule/{gid}")
        print(f"[bootstrap] applied global_rule {gid}")
    return applied


def verify_table(table):
    """Confirm every object in routes.json is readable back from the Admin API."""
    failures = []
    checks = (
        [("upstreams", u["id"]) for u in table.get("upstreams", [])]
        + [("routes", r["id"]) for r in table.get("routes", [])]
        + [("global_rules", g["id"]) for g in table.get("global_rules", [])]
    )
    for kind, oid in checks:
        status, _ = api("GET", f"/apisix/admin/{kind}/{oid}")
        ok = status == 200
        print(f"[verify] {kind}/{oid}: {'OK' if ok else f'FAIL (HTTP {status})'}")
        if not ok:
            failures.append(f"{kind}/{oid}")
    return failures


def verify_data_plane():
    """Smoke-check that the gateway data plane proxies /healthz to the backend."""
    req = urllib.request.Request(DATA_URL + "/healthz")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            ok = resp.status == 200
            print(f"[verify] data plane {DATA_URL}/healthz: {'OK' if ok else f'FAIL (HTTP {resp.status})'}")
            return ok
    except Exception as e:
        print(f"[verify] data plane {DATA_URL}/healthz: FAIL ({e})")
        return False


def main():
    verify_only = "--verify-only" in sys.argv
    timeout_s = 120
    if "--timeout" in sys.argv:
        timeout_s = int(sys.argv[sys.argv.index("--timeout") + 1])

    with open(ROUTES_FILE) as fh:
        table = json.load(fh)
    print(f"[bootstrap] loaded {ROUTES_FILE}: "
          f"{len(table.get('upstreams', []))} upstreams, "
          f"{len(table.get('routes', []))} routes, "
          f"{len(table.get('global_rules', []))} global rules")

    if not wait_for_admin(timeout_s):
        print("[bootstrap] FAIL: admin API did not become ready in time")
        return 1

    if not verify_only:
        try:
            apply_table(table)
        except RuntimeError as e:
            print(f"[bootstrap] FAIL: {e}")
            return 1

    failures = verify_table(table)
    dp_ok = verify_data_plane()
    if not dp_ok:
        failures.append("data-plane/healthz")

    if failures:
        print(f"[bootstrap] FAIL: {len(failures)} verification(s) failed: {', '.join(failures)}")
        return 1
    print("[bootstrap] SUCCESS: APISIX integration state applied and verified")
    return 0


if __name__ == "__main__":
    sys.exit(main())
