#!/usr/bin/env python3
"""INEC platform smoke test — every stakeholder workflow, end to end.

Exercises the full election lifecycle through the public HTTP API exactly as
each stakeholder role would:

    public (unauthenticated)  health probes, geography, auth gatekeeping
    public (registered)       register, login, refresh, me, logout
    observer                  register, parties, incidents, dashboard, audit
    presiding_officer         result submission (EC8A), duplicate/overvote guards
    collation_officer/admin   election CRUD, FSM transitions, validate, finalize, dispute
    admin                     user promotion, election administration

Usage:
    python3 scripts/smoke_stakeholder_workflows.py [--base URL] [--list]

Environment:
    BASE_URL              default http://localhost:8088
    ADMIN_USERNAME        default admin          (seeded by seed.go)
    ADMIN_PASSWORD        default admin123       (SEED_ADMIN_PASSWORD)
    SMOKE_RUN             run suffix for unique usernames (default: epoch)

Stdlib only. Exit code 0 = all scenarios passed, 1 = at least one failure.
"""
import json
import os
import sys
import time
import urllib.error
import urllib.request

BASE = os.environ.get("BASE_URL", "http://localhost:8088").rstrip("/")
ADMIN_USER = os.environ.get("ADMIN_USERNAME", "admin")
ADMIN_PASS = os.environ.get("ADMIN_PASSWORD", "admin123")
RUN = os.environ.get("SMOKE_RUN", str(int(time.time())))

results = []          # (section, name, ok, detail)
SCENARIOS = {}        # fn_name -> (section, display_name, fn)
_ctx = {}             # shared state between scenarios


def call(method, path, body=None, token=None, headers=None):
    """HTTP helper -> (status, parsed_json_or_text)."""
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    for k, v in (headers or {}).items():
        req.add_header(k, v)
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read().decode()
            try:
                return resp.status, json.loads(raw)
            except json.JSONDecodeError:
                return resp.status, raw
    except urllib.error.HTTPError as e:
        raw = e.read().decode(errors="replace")
        try:
            return e.code, json.loads(raw)
        except json.JSONDecodeError:
            return e.code, raw
    except Exception as e:
        return 0, str(e)


def scenario(section, name):
    """Decorator: register fn for later execution by main()."""
    def wrap(fn):
        SCENARIOS[fn.__name__] = (section, name, fn)
        return fn
    return wrap


def run_scenario(sid):
    section, name, fn = SCENARIOS[sid]
    try:
        detail = fn() or "ok"
        results.append((section, name, True, str(detail)[:160]))
        print(f"  PASS  {name} — {str(detail)[:120]}")
    except AssertionError as e:
        results.append((section, name, False, str(e)[:200]))
        print(f"  FAIL  {name} — {str(e)[:200]}")
    except Exception as e:
        results.append((section, name, False, f"{type(e).__name__}: {e}"[:200]))
        print(f"  FAIL  {name} — {type(e).__name__}: {str(e)[:160]}")


def expect(status, want, body, what):
    assert status == want, f"{what}: expected HTTP {want}, got {status}: {str(body)[:200]}"


# ───────────────────────── 1. Public / unauthenticated ─────────────────────────

@scenario("public", "GET /healthz returns 200")
def s01():
    st, b = call("GET", "/healthz")
    expect(st, 200, b, "healthz")
    return f"status keys: {list(b)[:4] if isinstance(b, dict) else 'ok'}"

@scenario("public", "GET /readiness returns 200")
def s02():
    st, b = call("GET", "/readiness")
    expect(st, 200, b, "readiness")

@scenario("public", "GET /geo/states lists Nigerian states")
def s03():
    st, b = call("GET", "/geo/states")
    expect(st, 200, b, "states")
    assert isinstance(b, list) and len(b) > 0, f"states empty: {b}"
    _ctx["state_code"] = b[0].get("code")
    return f"{len(b)} states, first={_ctx['state_code']}"

@scenario("public", "GET /geo/polling-units lists polling units")
def s04():
    st, b = call("GET", "/geo/polling-units?limit=5")
    expect(st, 200, b, "polling-units")
    assert isinstance(b, list) and len(b) >= 2, f"need >=2 PUs for scenarios, got {len(b) if isinstance(b, list) else b}"
    _ctx["pus"] = b
    return f"{len(b)} PUs sampled"

@scenario("public", "GET /geo/polling-units/{code} exposes registered_voters")
def s05():
    pu = _ctx["pus"][0]
    st, b = call("GET", f"/geo/polling-units/{pu['code']}")
    expect(st, 200, b, "pu detail")
    rv = b.get("registered_voters")
    assert isinstance(rv, int) and rv > 0, f"registered_voters missing: {b}"
    _ctx["pu1"] = {"code": pu["code"], "registered": rv}
    _ctx["pu2"] = {"code": _ctx["pus"][1]["code"], "registered": _ctx["pus"][1].get("registered_voters") or rv}
    _ctx["pu3"] = {"code": _ctx["pus"][2]["code"], "registered": _ctx["pus"][2].get("registered_voters") or rv} if len(_ctx["pus"]) > 2 else _ctx["pu2"]
    return f"PU {pu['code']} registered={rv}"

@scenario("public", "GET /elections requires authentication")
def s06():
    st, b = call("GET", "/elections")
    expect(st, 401, b, "elections unauthenticated")

@scenario("public", "GET /parties requires authentication")
def s07():
    st, b = call("GET", "/parties")
    expect(st, 401, b, "parties unauthenticated")

# ───────────────────────── 2. Registration & session ─────────────────────────

@scenario("auth", "POST /auth/register creates public user (200, access_token)")
def s08():
    user = f"smoke_pub_{RUN}"
    st, b = call("POST", "/auth/register", {
        "username": user, "password": "SmokePass1", "full_name": "Smoke Public", "role": "public"})
    expect(st, 200, b, "register")
    assert b.get("access_token"), f"no access_token: {b}"
    assert b.get("user", {}).get("role") == "public", f"role wrong: {b}"
    _ctx["pub"] = {"user": user, "pw": "SmokePass1", "token": b["access_token"], "id": b["user"]["id"]}
    return f"user id={b['user']['id']}"

@scenario("auth", "duplicate registration rejected (400)")
def s09():
    st, b = call("POST", "/auth/register", {
        "username": f"smoke_pub_{RUN}", "password": "SmokePass1", "full_name": "Smoke Public"})
    expect(st, 400, b, "duplicate register")

@scenario("auth", "weak password rejected (400)")
def s10():
    st, b = call("POST", "/auth/register", {
        "username": f"smoke_weak_{RUN}", "password": "weakpass", "full_name": "Weak Pass"})
    expect(st, 400, b, "weak password")

@scenario("auth", "short username rejected (400)")
def s11():
    st, b = call("POST", "/auth/register", {
        "username": "ab", "password": "SmokePass1", "full_name": "Short Name"})
    expect(st, 400, b, "short username")

@scenario("auth", "missing full_name rejected (400)")
def s12():
    st, b = call("POST", "/auth/register", {
        "username": f"smoke_nofn_{RUN}", "password": "SmokePass1", "full_name": "X"})
    expect(st, 400, b, "missing full_name")

@scenario("auth", "elevated-role self-registration blocked (403)")
def s13():
    st, b = call("POST", "/auth/register", {
        "username": f"smoke_hack_{RUN}", "password": "SmokePass1", "full_name": "Hack Admin", "role": "admin"})
    expect(st, 403, b, "admin self-register")

@scenario("auth", "POST /auth/login returns access + refresh tokens")
def s14():
    st, b = call("POST", "/auth/login", {"username": _ctx["pub"]["user"], "password": _ctx["pub"]["pw"]})
    expect(st, 200, b, "login")
    assert b.get("access_token") and b.get("refresh_token"), f"tokens missing: {b}"
    _ctx["pub"]["token"] = b["access_token"]
    _ctx["pub"]["refresh"] = b["refresh_token"]
    return f"user={b['user']['username']} role={b['user']['role']}"

@scenario("auth", "wrong password rejected (401)")
def s15():
    st, b = call("POST", "/auth/login", {"username": _ctx["pub"]["user"], "password": "WrongPass9"})
    expect(st, 401, b, "bad login")

@scenario("auth", "POST /auth/refresh issues new tokens")
def s16():
    st, b = call("POST", "/auth/refresh", {"refresh_token": _ctx["pub"]["refresh"]})
    expect(st, 200, b, "refresh")
    assert b.get("access_token"), f"no access_token: {b}"
    _ctx["pub"]["token"] = b["access_token"]

@scenario("auth", "access token is not a refresh token (401)")
def s17():
    st, b = call("POST", "/auth/refresh", {"refresh_token": _ctx["pub"]["token"]})
    expect(st, 401, b, "access-as-refresh")

@scenario("auth", "GET /auth/me returns the session user")
def s18():
    st, b = call("GET", "/auth/me", token=_ctx["pub"]["token"])
    expect(st, 200, b, "me")
    assert b.get("username") == _ctx["pub"]["user"], f"wrong user: {b}"

@scenario("auth", "GET /auth/me without token rejected (401)")
def s19():
    st, b = call("GET", "/auth/me")
    expect(st, 401, b, "me unauthenticated")

@scenario("auth", "observer self-registration allowed")
def s20():
    user = f"smoke_obs_{RUN}"
    st, b = call("POST", "/auth/register", {
        "username": user, "password": "SmokePass1", "full_name": "Smoke Observer", "role": "observer"})
    expect(st, 200, b, "observer register")
    _ctx["obs"] = {"user": user, "pw": "SmokePass1", "token": b["access_token"], "id": b["user"]["id"]}

# ───────────────────────── 3. Admin: election lifecycle ─────────────────────────

@scenario("admin", "admin login (seeded account)")
def s21():
    st, b = call("POST", "/auth/login", {"username": ADMIN_USER, "password": ADMIN_PASS})
    expect(st, 200, b, "admin login")
    assert b["user"]["role"] == "admin", f"not admin: {b}"
    _ctx["admin"] = b["access_token"]

@scenario("admin", "election creation forbidden for public user (403)")
def s22():
    st, b = call("POST", "/elections", {
        "title": "Forbidden Election", "election_type": "presidential",
        "election_date": "2030-01-01"}, token=_ctx["pub"]["token"])
    expect(st, 403, b, "public create election")

@scenario("admin", "admin creates election (200, draft)")
def s23():
    date = time.strftime("%Y-%m-%d", time.gmtime(time.time() + 30 * 86400))
    st, b = call("POST", "/elections", {
        "title": f"Smoke Election {RUN}", "election_type": "presidential",
        "election_date": date, "description": "smoke test election"}, token=_ctx["admin"])
    expect(st, 200, b, "create election")
    assert b.get("id"), f"no id: {b}"
    _ctx["election_id"] = b["id"]
    return f"election id={b['id']} date={date}"

@scenario("admin", "GET /elections contains the new election as draft")
def s24():
    st, b = call("GET", "/elections", token=_ctx["admin"])
    expect(st, 200, b, "list elections")
    mine = [e for e in b if e.get("id") == _ctx["election_id"]]
    assert mine, f"election {_ctx['election_id']} not in list"
    assert mine[0].get("status") == "draft", f"status={mine[0].get('status')}"

@scenario("admin", "GET /elections/{id} returns the election")
def s25():
    st, b = call("GET", f"/elections/{_ctx['election_id']}", token=_ctx["admin"])
    expect(st, 200, b, "get election")
    assert b.get("title", "").startswith("Smoke Election"), f"title: {b.get('title')}"

@scenario("admin", "PATCH /elections/{id} updates title")
def s26():
    st, b = call("PATCH", f"/elections/{_ctx['election_id']}",
                 {"title": f"Smoke Election {RUN} (updated)"}, token=_ctx["admin"])
    expect(st, 200, b, "patch election")

@scenario("admin", "PATCH /elections/{id} forbidden for observer (403)")
def s27():
    st, b = call("PATCH", f"/elections/{_ctx['election_id']}",
                 {"title": "observer edit"}, token=_ctx["obs"]["token"])
    expect(st, 403, b, "observer patch election")

@scenario("admin", "FSM transition draft->scheduled")
def s28():
    st, b = call("POST", f"/ems/elections/{_ctx['election_id']}/fsm/transition",
                 {"event": "schedule"}, token=_ctx["admin"])
    expect(st, 200, b, "schedule")
    return str(b)[:100]

@scenario("admin", "FSM rejects unknown event (422)")
def s29():
    st, b = call("POST", f"/ems/elections/{_ctx['election_id']}/fsm/transition",
                 {"event": "explode"}, token=_ctx["admin"])
    expect(st, 422, b, "bad FSM event")

@scenario("admin", "FSM transition scheduled->active")
def s30():
    st, b = call("POST", f"/ems/elections/{_ctx['election_id']}/fsm/transition",
                 {"event": "activate"}, token=_ctx["admin"])
    expect(st, 200, b, "activate")
    st2, b2 = call("GET", f"/elections/{_ctx['election_id']}", token=_ctx["admin"])
    assert b2.get("status") == "active", f"status after activate: {b2.get('status')}"

@scenario("admin", "admin promotes user to presiding_officer")
def s31():
    st, b = call("POST", "/admin/users/promote",
                 {"user_id": _ctx["pub"]["id"], "role": "presiding_officer"}, token=_ctx["admin"])
    expect(st, 200, b, "promote")

@scenario("admin", "promoted officer re-login carries new role")
def s32():
    st, b = call("POST", "/auth/login", {"username": _ctx["pub"]["user"], "password": _ctx["pub"]["pw"]})
    expect(st, 200, b, "officer re-login")
    assert b["user"]["role"] == "presiding_officer", f"role: {b['user']['role']}"
    _ctx["officer"] = b["access_token"]

# ───────────────────────── 4. Results pipeline (EC8A) ─────────────────────────

def _party_codes():
    if "parties" not in _ctx:
        st, b = call("GET", "/parties", token=_ctx["admin"])
        expect(st, 200, b, "parties")
        assert len(b) >= 3, f"need >=3 seeded parties, got {len(b)}"
        _ctx["parties"] = [p["code"] for p in b[:3]]
    return _ctx["parties"]

def _result_body(pu, votes):
    codes = _party_codes()
    return {
        "election_id": _ctx["election_id"],
        "polling_unit_code": pu["code"],
        "party_scores": [
            {"party_code": codes[0], "votes": votes[0]},
            {"party_code": codes[1], "votes": votes[1]},
            {"party_code": codes[2], "votes": votes[2]},
        ],
        "accredited_voters": pu["registered"],
        "rejected_votes": 50,
    }

@scenario("results", "presiding officer submits EC8A result (200)")
def s33():
    pu = _ctx["pu1"]
    reg = pu["registered"]
    votes = [reg - 200, 100, 50]           # valid = reg-50 ; +50 rejected = reg polled <= accredited
    st, b = call("POST", "/results/submit", _result_body(pu, votes), token=_ctx["officer"])
    expect(st, 200, b, "submit result")
    assert b.get("id"), f"no result id: {b}"
    _ctx["result_id"] = b["id"]
    return f"result id={b['id']} status={b.get('status')}"

@scenario("results", "duplicate submission for same PU rejected (400)")
def s34():
    st, b = call("POST", "/results/submit", _result_body(_ctx["pu1"], [100, 80, 60]), token=_ctx["officer"])
    expect(st, 400, b, "duplicate submit")
    assert "already submitted" in str(b).lower(), f"unexpected message: {b}"

@scenario("results", "overvote rejected by EC8A validation (400)")
def s35():
    pu = _ctx["pu2"]
    reg = pu["registered"]
    st, b = call("POST", "/results/submit", _result_body(pu, [reg + 100, 50, 50]), token=_ctx["officer"])
    expect(st, 400, b, "overvote submit")

@scenario("results", "observer cannot submit results (403)")
def s36():
    st, b = call("POST", "/results/submit", _result_body(_ctx["pu3"], [100, 80, 60]), token=_ctx["obs"]["token"])
    expect(st, 403, b, "observer submit")

@scenario("results", "empty party_scores rejected (400)")
def s37():
    st, b = call("POST", "/results/submit", {
        "election_id": _ctx["election_id"], "polling_unit_code": _ctx["pu3"]["code"],
        "party_scores": [], "accredited_voters": 100, "rejected_votes": 0}, token=_ctx["officer"])
    expect(st, 400, b, "empty party_scores")

@scenario("results", "GET /results?election_id returns {total, results}")
def s38():
    st, b = call("GET", f"/results?election_id={_ctx['election_id']}", token=_ctx["admin"])
    expect(st, 200, b, "list results")
    assert b.get("total") == 1 and len(b.get("results", [])) == 1, f"shape: total={b.get('total')} len={len(b.get('results', []))}"
    assert b["results"][0]["id"] == _ctx["result_id"], f"id mismatch: {b['results'][0].get('id')}"

@scenario("results", "GET /results/{id} includes party_scores")
def s39():
    st, b = call("GET", f"/results/{_ctx['result_id']}", token=_ctx["admin"])
    expect(st, 200, b, "get result")
    ps = b.get("party_scores") or []
    assert len(ps) == 3, f"party_scores: {len(ps)}"

@scenario("results", "collation/admin validates result (200)")
def s40():
    st, b = call("POST", f"/results/{_ctx['result_id']}/validate", {}, token=_ctx["admin"])
    expect(st, 200, b, "validate")
    assert b.get("status") == "validated", f"status: {b}"

@scenario("results", "re-validation rejected as invalid transition (400)")
def s41():
    st, b = call("POST", f"/results/{_ctx['result_id']}/validate", {}, token=_ctx["admin"])
    expect(st, 400, b, "re-validate")

@scenario("results", "finalization posts dual ledger (200)")
def s42():
    st, b = call("POST", f"/results/{_ctx['result_id']}/finalize", {}, token=_ctx["admin"])
    expect(st, 200, b, "finalize")
    assert b.get("status") == "finalized", f"status: {b}"
    assert b.get("tigerbeetle_status") == "POSTED", f"tb: {b}"
    assert b.get("hyperledger_tx_id"), f"hl tx missing: {b}"
    return f"hl_tx={b.get('hyperledger_tx_id')}"

@scenario("results", "finalizing a pending result rejected (400)")
def s43():
    st, b = call("POST", "/results/submit", _result_body(_ctx["pu2"], [200, 150, 100]), token=_ctx["officer"])
    expect(st, 200, b, "submit second result")
    rid = b.get("id")
    _ctx["result2_id"] = rid
    st2, b2 = call("POST", f"/results/{rid}/finalize", {}, token=_ctx["admin"])
    expect(st2, 400, b2, "finalize pending")

@scenario("results", "dispute voids TigerBeetle transfer (200)")
def s44():
    st, b = call("POST", f"/results/{_ctx['result2_id']}/dispute", {}, token=_ctx["admin"])
    expect(st, 200, b, "dispute")
    assert b.get("status") == "disputed", f"status: {b}"
    assert b.get("tigerbeetle_status") == "VOIDED", f"tb: {b}"

# ───────────────────────── 5. Observer, audit & dashboards ─────────────────────────

@scenario("observer", "observer files an incident report (200)")
def s45():
    st, b = call("POST", "/incidents", {
        "election_id": _ctx["election_id"], "polling_unit_code": _ctx["pu1"]["code"],
        "incident_type": "ballot_box_snatching",
        "description": "Smoke test incident report from observer workflow",
        "severity": "high"}, token=_ctx["obs"]["token"])
    expect(st, 200, b, "create incident")
    assert b.get("id"), f"no id: {b}"
    _ctx["incident_id"] = b["id"]

@scenario("observer", "invalid incident severity rejected (400)")
def s46():
    st, b = call("POST", "/incidents", {
        "election_id": _ctx["election_id"], "polling_unit_code": _ctx["pu1"]["code"],
        "incident_type": "violence", "description": "bad severity test", "severity": "extreme"}, token=_ctx["obs"]["token"])
    expect(st, 400, b, "bad severity")

@scenario("observer", "incident status update to resolved (200)")
def s47():
    st, b = call("PATCH", f"/incidents/{_ctx['incident_id']}", {"status": "resolved"}, token=_ctx["admin"])
    expect(st, 200, b, "resolve incident")

@scenario("observer", "GET /incidents lists the filed incident")
def s48():
    st, b = call("GET", f"/incidents?election_id={_ctx['election_id']}", token=_ctx["obs"]["token"])
    expect(st, 200, b, "list incidents")
    ids = [i.get("id") for i in b] if isinstance(b, list) else []
    assert _ctx["incident_id"] in ids, f"incident not listed: {ids}"

@scenario("observer", "audit trail records the workflow (hash chain)")
def s49():
    st, b = call("GET", f"/audit/verify/{_ctx['result_id']}", token=_ctx["obs"]["token"])
    expect(st, 200, b, "audit verify")
    assert b.get("chain_valid") is True, f"chain invalid: {b}"
    assert len(b.get("audit_entries", [])) >= 2, f"entries: {len(b.get('audit_entries', []))}"
    return f"entries={len(b['audit_entries'])} chain_valid=true"

@scenario("observer", "dashboard stats expose dual-ledger reconciliation")
def s50():
    st, b = call("GET", f"/dashboard/stats?election_id={_ctx['election_id']}", token=_ctx["obs"]["token"])
    expect(st, 200, b, "dashboard stats")
    dl = b.get("dual_ledger") or {}
    assert dl.get("tigerbeetle_posted", 0) >= 1, f"dual_ledger: {dl}"
    assert b.get("results_received", 0) >= 1, f"results_received: {b.get('results_received')}"

@scenario("observer", "collation by state aggregates finalized votes")
def s51():
    st, b = call("GET", f"/dashboard/collation?level=state&election_id={_ctx['election_id']}", token=_ctx["obs"]["token"])
    expect(st, 200, b, "collation")
    assert isinstance(b, list), f"not a list: {type(b)}"

@scenario("observer", "live feed shows recent submissions")
def s52():
    st, b = call("GET", f"/dashboard/live-feed?election_id={_ctx['election_id']}", token=_ctx["obs"]["token"])
    expect(st, 200, b, "live feed")
    assert isinstance(b, list) and len(b) >= 1, f"feed empty: {b}"

@scenario("observer", "map data returns states + polling units")
def s53():
    st, b = call("GET", f"/geo/map-data?election_id={_ctx['election_id']}")
    expect(st, 200, b, "map data")
    assert "states" in b and "polling_units" in b, f"shape: {list(b) if isinstance(b, dict) else b}"

@scenario("observer", "GET /parties lists seeded parties (authenticated)")
def s54():
    st, b = call("GET", "/parties", token=_ctx["obs"]["token"])
    expect(st, 200, b, "parties")
    assert isinstance(b, list) and len(b) >= 3, f"parties: {len(b) if isinstance(b, list) else b}"

@scenario("observer", "GET /elections/{id}/stats reflects submitted results")
def s55():
    st, b = call("GET", f"/elections/{_ctx['election_id']}/stats", token=_ctx["obs"]["token"])
    expect(st, 200, b, "election stats")
    assert b.get("results_received", 0) >= 1, f"results_received: {b.get('results_received')}"
    assert (b.get("results_finalized") or 0) >= 1, f"finalized: {b.get('results_finalized')}"

SECTIONS = [
    ("1. Public / unauthenticated", ["s01", "s02", "s03", "s04", "s05", "s06", "s07"]),
    ("2. Registration & session", ["s08", "s09", "s10", "s11", "s12", "s13", "s14", "s15", "s16", "s17", "s18", "s19", "s20"]),
    ("3. Admin: election lifecycle", ["s21", "s22", "s23", "s24", "s25", "s26", "s27", "s28", "s29", "s30", "s31", "s32"]),
    ("4. Results pipeline (EC8A)", ["s33", "s34", "s35", "s36", "s37", "s38", "s39", "s40", "s41", "s42", "s43", "s44"]),
    ("5. Observer, audit & dashboards", ["s45", "s46", "s47", "s48", "s49", "s50", "s51", "s52", "s53", "s54", "s55"]),
]


def main():
    if "--list" in sys.argv:
        n = 0
        for title, ids in SECTIONS:
            print(title)
            for sid in ids:
                n += 1
                print(f"   {n:2d}. {SCENARIOS[sid][1]}")
        print(f"total: {n} scenarios")
        return 0

    base = BASE
    if "--base" in sys.argv:
        base = sys.argv[sys.argv.index("--base") + 1].rstrip("/")
        globals()["BASE"] = base

    print(f"INEC stakeholder workflow smoke test")
    print(f"target: {base}   run: {RUN}   admin: {ADMIN_USER}")
    print("=" * 72)

    for title, ids in SECTIONS:
        print(f"\n[{title}]")
        for sid in ids:
            run_scenario(sid)

    passed = sum(1 for r in results if r[2])
    failed = len(results) - passed
    print("\n" + "=" * 72)
    print(f"RESULT: {passed}/{len(results)} passed, {failed} failed")
    if failed:
        print("failures:")
        for section, name, ok, detail in results:
            if not ok:
                print(f"  - [{section}] {name}: {detail}")
        return 1
    print("ALL STAKEHOLDER WORKFLOWS OPERATIONAL")
    return 0


if __name__ == "__main__":
    sys.exit(main())
