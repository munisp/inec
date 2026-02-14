from fastapi import APIRouter, Depends, Query, Request
from app.database import get_db
import json, datetime

router = APIRouter(prefix="/dashboard", tags=["dashboard"])

@router.get("/stats")
def get_dashboard_stats(election_id: int = Query(default=1), db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM elections WHERE id=?", (election_id,))
    election = cursor.fetchone()
    if not election:
        return {"error": "Election not found"}

    cursor = db.execute("SELECT COUNT(*) as total FROM polling_units")
    total_pus = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=?", (election_id,))
    results_received = cursor.fetchone()["total"]

    cursor = db.execute("SELECT status, COUNT(*) as count FROM results WHERE election_id=? GROUP BY status", (election_id,))
    status_counts = {row["status"]: row["count"] for row in cursor.fetchall()}

    cursor = db.execute("""
        SELECT SUM(total_valid_votes) as valid, SUM(rejected_votes) as rejected,
               SUM(total_votes_cast) as cast_votes, SUM(accredited_voters) as accredited
        FROM results WHERE election_id=? AND status IN ('finalized','validated')
    """, (election_id,))
    totals = cursor.fetchone()

    cursor = db.execute("""
        SELECT rps.party_code, p.name as party_name, p.color, p.abbreviation,
               SUM(rps.votes) as total_votes
        FROM result_party_scores rps
        JOIN results r ON r.id = rps.result_id
        JOIN parties p ON p.code = rps.party_code
        WHERE r.election_id=? AND r.status IN ('finalized','validated')
        GROUP BY rps.party_code
        ORDER BY total_votes DESC
    """, (election_id,))
    party_scores = [dict(row) for row in cursor.fetchall()]

    cursor = db.execute("""
        SELECT s.code, s.name, s.geo_zone, COUNT(r.id) as results_count,
               SUM(r.total_valid_votes) as total_votes
        FROM states s
        LEFT JOIN lgas l ON l.state_code = s.code
        LEFT JOIN wards w ON w.lga_code = l.code
        LEFT JOIN polling_units pu ON pu.ward_code = w.code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
        GROUP BY s.code
        ORDER BY s.name
    """, (election_id,))
    state_results = [dict(row) for row in cursor.fetchall()]

    cursor = db.execute("""
        SELECT COUNT(*) as tb_posted FROM results WHERE election_id=? AND tigerbeetle_status='POSTED'
    """, (election_id,))
    tb_posted = cursor.fetchone()["tb_posted"]

    cursor = db.execute("""
        SELECT COUNT(*) as hl_confirmed FROM results WHERE election_id=? AND hyperledger_status='CONFIRMED'
    """, (election_id,))
    hl_confirmed = cursor.fetchone()["hl_confirmed"]

    cursor = db.execute("""
        SELECT s.geo_zone, SUM(r.total_valid_votes) as total_votes, COUNT(r.id) as results_count
        FROM results r
        JOIN polling_units pu ON pu.code = r.polling_unit_code
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        WHERE r.election_id=? AND r.status IN ('finalized','validated')
        GROUP BY s.geo_zone
    """, (election_id,))
    zone_results = [dict(row) for row in cursor.fetchall()]

    return {
        "election": dict(election),
        "total_polling_units": total_pus,
        "results_received": results_received,
        "completion_percentage": round((results_received / total_pus * 100), 2) if total_pus > 0 else 0,
        "status_breakdown": {
            "finalized": status_counts.get("finalized", 0),
            "validated": status_counts.get("validated", 0),
            "pending": status_counts.get("pending", 0),
            "disputed": status_counts.get("disputed", 0),
            "voided": status_counts.get("voided", 0),
        },
        "vote_totals": {
            "valid": totals["valid"] or 0,
            "rejected": totals["rejected"] or 0,
            "cast": totals["cast_votes"] or 0,
            "accredited": totals["accredited"] or 0,
        },
        "party_scores": party_scores,
        "state_results": state_results,
        "zone_results": zone_results,
        "dual_ledger": {
            "tigerbeetle_posted": tb_posted,
            "hyperledger_confirmed": hl_confirmed,
            "total_results": results_received,
            "reconciliation_variance": round(abs(tb_posted - hl_confirmed) / max(results_received, 1) * 100, 4)
        }
    }

@router.get("/live-feed")
def get_live_feed(election_id: int = Query(default=1), limit: int = 20, db=Depends(get_db)):
    cursor = db.execute("""
        SELECT r.id, r.polling_unit_code, r.status, r.total_votes_cast,
               r.tigerbeetle_status, r.hyperledger_status, r.submitted_at,
               pu.name as pu_name, w.name as ward_name, l.name as lga_name,
               s.name as state_name, s.code as state_code
        FROM results r
        JOIN polling_units pu ON pu.code = r.polling_unit_code
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        WHERE r.election_id=?
        ORDER BY r.submitted_at DESC
        LIMIT ?
    """, (election_id, limit))
    return [dict(row) for row in cursor.fetchall()]

@router.get("/collation")
def get_collation(election_id: int = Query(default=1), level: str = "state", parent_code: str | None = None, db=Depends(get_db)):
    if level == "state":
        cursor = db.execute("""
            SELECT s.code, s.name, s.geo_zone,
                   COUNT(DISTINCT pu.code) as total_pus,
                   COUNT(DISTINCT r.id) as reported_pus,
                   SUM(r.total_valid_votes) as total_valid_votes,
                   SUM(r.rejected_votes) as rejected_votes,
                   SUM(r.total_votes_cast) as total_votes_cast
            FROM states s
            LEFT JOIN lgas l ON l.state_code = s.code
            LEFT JOIN wards w ON w.lga_code = l.code
            LEFT JOIN polling_units pu ON pu.ward_code = w.code
            LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
            GROUP BY s.code
            ORDER BY s.name
        """, (election_id,))
        results = [dict(row) for row in cursor.fetchall()]

        for r in results:
            cursor2 = db.execute("""
                SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
                FROM result_party_scores rps
                JOIN results res ON res.id = rps.result_id
                JOIN polling_units pu ON pu.code = res.polling_unit_code
                JOIN wards w ON w.code = pu.ward_code
                JOIN lgas l ON l.code = w.lga_code
                JOIN parties p ON p.code = rps.party_code
                WHERE l.state_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
                GROUP BY rps.party_code
                ORDER BY total_votes DESC
            """, (r["code"], election_id))
            r["party_scores"] = [dict(row) for row in cursor2.fetchall()]
        return results

    elif level == "lga" and parent_code:
        cursor = db.execute("""
            SELECT l.code, l.name,
                   COUNT(DISTINCT pu.code) as total_pus,
                   COUNT(DISTINCT r.id) as reported_pus,
                   SUM(r.total_valid_votes) as total_valid_votes,
                   SUM(r.rejected_votes) as rejected_votes,
                   SUM(r.total_votes_cast) as total_votes_cast
            FROM lgas l
            LEFT JOIN wards w ON w.lga_code = l.code
            LEFT JOIN polling_units pu ON pu.ward_code = w.code
            LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
            WHERE l.state_code=?
            GROUP BY l.code
            ORDER BY l.name
        """, (election_id, parent_code))
        results = [dict(row) for row in cursor.fetchall()]

        for r in results:
            cursor2 = db.execute("""
                SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
                FROM result_party_scores rps
                JOIN results res ON res.id = rps.result_id
                JOIN polling_units pu ON pu.code = res.polling_unit_code
                JOIN wards w ON w.code = pu.ward_code
                JOIN parties p ON p.code = rps.party_code
                WHERE w.lga_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
                GROUP BY rps.party_code
                ORDER BY total_votes DESC
            """, (r["code"], election_id))
            r["party_scores"] = [dict(row) for row in cursor2.fetchall()]
        return results

    elif level == "ward" and parent_code:
        cursor = db.execute("""
            SELECT w.code, w.name,
                   COUNT(DISTINCT pu.code) as total_pus,
                   COUNT(DISTINCT r.id) as reported_pus,
                   SUM(r.total_valid_votes) as total_valid_votes,
                   SUM(r.rejected_votes) as rejected_votes,
                   SUM(r.total_votes_cast) as total_votes_cast
            FROM wards w
            LEFT JOIN polling_units pu ON pu.ward_code = w.code
            LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
            WHERE w.lga_code=?
            GROUP BY w.code
            ORDER BY w.name
        """, (election_id, parent_code))
        results = [dict(row) for row in cursor.fetchall()]

        for r in results:
            cursor2 = db.execute("""
                SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
                FROM result_party_scores rps
                JOIN results res ON res.id = rps.result_id
                JOIN polling_units pu ON pu.code = res.polling_unit_code
                JOIN parties p ON p.code = rps.party_code
                WHERE pu.ward_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
                GROUP BY rps.party_code
                ORDER BY total_votes DESC
            """, (r["code"], election_id))
            r["party_scores"] = [dict(row) for row in cursor2.fetchall()]
        return results

    elif level == "pu" and parent_code:
        cursor = db.execute("""
            SELECT pu.code, pu.name, pu.registered_voters,
                   r.id as result_id, r.status, r.total_valid_votes, r.rejected_votes,
                   r.total_votes_cast, r.accredited_voters,
                   r.tigerbeetle_status, r.hyperledger_status
            FROM polling_units pu
            LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
            WHERE pu.ward_code=?
            ORDER BY pu.name
        """, (election_id, parent_code))
        results = [dict(row) for row in cursor.fetchall()]

        for r in results:
            if r["result_id"]:
                cursor2 = db.execute("""
                    SELECT rps.party_code, p.abbreviation, p.color, rps.votes
                    FROM result_party_scores rps
                    JOIN parties p ON p.code = rps.party_code
                    WHERE rps.result_id=?
                    ORDER BY rps.votes DESC
                """, (r["result_id"],))
                r["party_scores"] = [dict(row) for row in cursor2.fetchall()]
            else:
                r["party_scores"] = []
        return results

    return []

@router.post("/metrics/client")
async def post_client_metric(request: Request, db=Depends(get_db)):
    try:
        payload = await request.json()
    except Exception:
        payload = {}
    ua = request.headers.get("user-agent", "")
    ip = request.client.host if request.client else ""
    db.execute("""
        CREATE TABLE IF NOT EXISTS metrics_client (
            id INTEGER PRIMARY KEY,
            ts TEXT,
            event TEXT,
            data TEXT,
            ua TEXT,
            ip TEXT
        )
    """)
    db.execute(
        "INSERT INTO metrics_client (ts, event, data, ua, ip) VALUES (?,?,?,?,?)",
        (
            datetime.datetime.utcnow().isoformat()+"Z",
            str(payload.get("event", "unknown")),
            json.dumps(payload.get("data", {})),
            ua,
            ip,
        ),
    )
    db.commit()
    return {"ok": True}

@router.get("/metrics/client/recent")
def recent_client_metrics(limit: int = 50, db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM metrics_client ORDER BY id DESC LIMIT ?", (limit,))
    return [dict(row) for row in cursor.fetchall()]
