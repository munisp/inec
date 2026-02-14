import hashlib
import json
import random
from datetime import datetime, timezone
from fastapi import APIRouter, Depends, HTTPException, WebSocket, WebSocketDisconnect
from pydantic import BaseModel
from app.database import get_db
import asyncio
from app.auth import get_current_user, require_role

router = APIRouter(prefix="/results", tags=["results"]) 

# Simple in-memory websocket client registry for broadcasts
_ws_clients: set[WebSocket] = set()

async def _broadcast(msg: dict):
    dead: list[WebSocket] = []
    for ws in list(_ws_clients):
        try:
            await ws.send_text(json.dumps(msg))
        except Exception:
            dead.append(ws)
    for ws in dead:
        try:
            _ws_clients.remove(ws)
        except KeyError:
            pass

def _notify_update(msg: dict):
    try:
        loop = asyncio.get_event_loop()
        loop.create_task(_broadcast(msg))
    except RuntimeError:
        # no running loop; best-effort fallback
        asyncio.run(_broadcast(msg))

@router.websocket("/ws/updates")
async def ws_updates(ws: WebSocket):
    await ws.accept()
    _ws_clients.add(ws)
    try:
        while True:
            # Keep-alive / ignore incoming
            await ws.receive_text()
    except WebSocketDisconnect:
        try:
            _ws_clients.remove(ws)
        except KeyError:
            pass

class PartyScore(BaseModel):
    party_code: str
    votes: int

class ResultSubmission(BaseModel):
    election_id: int
    polling_unit_code: str
    party_scores: list[PartyScore]
    accredited_voters: int
    rejected_votes: int = 0

class ResultUpdate(BaseModel):
    status: str | None = None

def _log_audit(db, action, entity_type, entity_id, user_id, details):
    cursor = db.execute("SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1")
    last = cursor.fetchone()
    prev_hash = last["block_hash"] if last else "0" * 64
    block_data = f"{prev_hash}{action}{entity_id}{datetime.now(timezone.utc).isoformat()}"
    block_hash = hashlib.sha256(block_data.encode()).hexdigest()
    db.execute(
        "INSERT INTO audit_log (action, entity_type, entity_id, user_id, details, block_hash, prev_block_hash) VALUES (?,?,?,?,?,?,?)",
        (action, entity_type, str(entity_id), user_id, json.dumps(details), block_hash, prev_hash)
    )

@router.post("/submit")
def submit_result(req: ResultSubmission, user=Depends(require_role("admin", "presiding_officer")), db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM elections WHERE id=? AND status='active'", (req.election_id,))
    if not cursor.fetchone():
        raise HTTPException(status_code=400, detail="Election not found or not active")

    cursor = db.execute("SELECT * FROM polling_units WHERE code=?", (req.polling_unit_code,))
    pu = cursor.fetchone()
    if not pu:
        raise HTTPException(status_code=400, detail="Polling unit not found")

    cursor = db.execute("SELECT id FROM results WHERE election_id=? AND polling_unit_code=?",
                        (req.election_id, req.polling_unit_code))
    if cursor.fetchone():
        raise HTTPException(status_code=400, detail="Result already submitted for this polling unit")

    total_valid = sum(ps.votes for ps in req.party_scores)
    total_cast = total_valid + req.rejected_votes

    if total_cast > req.accredited_voters:
        raise HTTPException(status_code=400, detail="Total votes cast exceeds accredited voters")
    if req.accredited_voters > pu["registered_voters"]:
        raise HTTPException(status_code=400, detail="Accredited voters exceeds registered voters")

    tb_id = f"TB-{random.randint(100000,999999)}"
    ec8a_hash = f"sha256:{hashlib.sha256(req.polling_unit_code.encode()).hexdigest()}"

    cursor = db.execute(
        """INSERT INTO results (election_id, polling_unit_code, presiding_officer_id, status,
           total_valid_votes, rejected_votes, total_votes_cast, accredited_voters,
           ec8a_hash, tigerbeetle_transfer_id, tigerbeetle_status, hyperledger_status)
           VALUES (?,?,?,?,?,?,?,?,?,?,?,?)""",
        (req.election_id, req.polling_unit_code, int(user["sub"]), "pending",
         total_valid, req.rejected_votes, total_cast, req.accredited_voters,
         ec8a_hash, tb_id, "PENDING", "PENDING")
    )
    result_id = cursor.lastrowid

    for ps in req.party_scores:
        db.execute("INSERT INTO result_party_scores (result_id, party_code, votes) VALUES (?,?,?)",
                   (result_id, ps.party_code, ps.votes))

    _log_audit(db, "RESULT_SUBMITTED", "result", result_id, int(user["sub"]),
               {"phase": "Pre-Validation", "polling_unit": req.polling_unit_code, "tigerbeetle_id": tb_id})

    db.commit()

    _notify_update({"type": "result_updated", "pu_code": req.polling_unit_code, "election_id": req.election_id})

    return {
        "id": result_id,
        "status": "pending",
        "tigerbeetle_transfer_id": tb_id,
        "phase": "Pre-Validation",
        "message": "Result submitted. Proceeding to Edge Validation."
    }

@router.post("/{result_id}/validate")
def validate_result(result_id: int, user=Depends(require_role("admin", "collation_officer")), db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM results WHERE id=?", (result_id,))
    result = cursor.fetchone()
    if not result:
        raise HTTPException(status_code=404, detail="Result not found")
    if result["status"] != "pending":
        raise HTTPException(status_code=400, detail=f"Result is already {result['status']}")

    db.execute("UPDATE results SET status='validated', validated_at=CURRENT_TIMESTAMP WHERE id=?", (result_id,))
    _log_audit(db, "RESULT_VALIDATED", "result", result_id, int(user["sub"]),
               {"phase": "Edge Validation", "polling_unit": result["polling_unit_code"]})
    db.commit()
    _notify_update({"type": "result_updated", "result_id": result_id})
    return {"status": "validated", "phase": "Edge Validation"}

@router.post("/{result_id}/finalize")
def finalize_result(result_id: int, user=Depends(require_role("admin", "collation_officer")), db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM results WHERE id=?", (result_id,))
    result = cursor.fetchone()
    if not result:
        raise HTTPException(status_code=404, detail="Result not found")
    if result["status"] not in ("pending", "validated"):
        raise HTTPException(status_code=400, detail=f"Cannot finalize result with status {result['status']}")

    hl_tx = f"0x{random.randbytes(16).hex()}"
    ipfs_cid = f"Qm{random.randbytes(22).hex()}"

    db.execute("""UPDATE results SET status='finalized', finalized_at=CURRENT_TIMESTAMP,
                  tigerbeetle_status='POSTED', hyperledger_status='CONFIRMED',
                  hyperledger_tx_id=?, ipfs_cid=?
                  WHERE id=?""", (hl_tx, ipfs_cid, result_id))

    _log_audit(db, "RESULT_FINALIZED", "result", result_id, int(user["sub"]),
               {"phase": "Finalization", "polling_unit": result["polling_unit_code"],
                "hyperledger_tx": hl_tx, "ipfs_cid": ipfs_cid})
    db.commit()
    return {
        "status": "finalized",
        "phase": "Finalization",
        "hyperledger_tx_id": hl_tx,
        "ipfs_cid": ipfs_cid,
        "tigerbeetle_status": "POSTED",
        "hyperledger_status": "CONFIRMED"
    }

@router.post("/{result_id}/dispute")
def dispute_result(result_id: int, user=Depends(require_role("admin", "observer")), db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM results WHERE id=?", (result_id,))
    result = cursor.fetchone()
    if not result:
        raise HTTPException(status_code=404, detail="Result not found")

    db.execute("UPDATE results SET status='disputed', tigerbeetle_status='VOIDED' WHERE id=?", (result_id,))
    _log_audit(db, "RESULT_DISPUTED", "result", result_id, int(user["sub"]),
               {"phase": "Dispute", "polling_unit": result["polling_unit_code"]})
    db.commit()
    _notify_update({"type": "result_updated", "result_id": result_id})
    return {"status": "disputed", "tigerbeetle_status": "VOIDED"}

@router.get("")
def list_results(election_id: int, status: str | None = None, state_code: str | None = None,
                 lga_code: str | None = None, limit: int = 50, offset: int = 0, db=Depends(get_db)):
    query = """
        SELECT r.*, pu.name as pu_name, pu.ward_code,
               w.name as ward_name, w.lga_code,
               l.name as lga_name, l.state_code,
               s.name as state_name
        FROM results r
        JOIN polling_units pu ON pu.code = r.polling_unit_code
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        WHERE r.election_id=?
    """
    params: list = [election_id]

    if status:
        query += " AND r.status=?"
        params.append(status)
    if state_code:
        query += " AND l.state_code=?"
        params.append(state_code)
    if lga_code:
        query += " AND w.lga_code=?"
        params.append(lga_code)

    count_query = query.replace("SELECT r.*, pu.name as pu_name, pu.ward_code,\n               w.name as ward_name, w.lga_code,\n               l.name as lga_name, l.state_code,\n               s.name as state_name", "SELECT COUNT(*) as total")
    cursor = db.execute(count_query, params)
    total = cursor.fetchone()["total"]

    query += " ORDER BY r.submitted_at DESC LIMIT ? OFFSET ?"
    params.extend([limit, offset])
    cursor = db.execute(query, params)
    results = [dict(row) for row in cursor.fetchall()]

    for r in results:
        cursor = db.execute("""
            SELECT rps.party_code, p.name as party_name, p.color, rps.votes
            FROM result_party_scores rps
            JOIN parties p ON p.code = rps.party_code
            WHERE rps.result_id=?
            ORDER BY rps.votes DESC
        """, (r["id"],))
        r["party_scores"] = [dict(row) for row in cursor.fetchall()]

    return {"total": total, "results": results}

@router.get("/{result_id}")
def get_result(result_id: int, db=Depends(get_db)):
    cursor = db.execute("""
        SELECT r.*, pu.name as pu_name, pu.ward_code, pu.registered_voters,
               w.name as ward_name, w.lga_code,
               l.name as lga_name, l.state_code,
               s.name as state_name
        FROM results r
        JOIN polling_units pu ON pu.code = r.polling_unit_code
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        WHERE r.id=?
    """, (result_id,))
    result = cursor.fetchone()
    if not result:
        raise HTTPException(status_code=404, detail="Result not found")

    result_dict = dict(result)
    cursor = db.execute("""
        SELECT rps.party_code, p.name as party_name, p.color, rps.votes
        FROM result_party_scores rps
        JOIN parties p ON p.code = rps.party_code
        WHERE rps.result_id=?
        ORDER BY rps.votes DESC
    """, (result_id,))
    result_dict["party_scores"] = [dict(row) for row in cursor.fetchall()]

    return result_dict
