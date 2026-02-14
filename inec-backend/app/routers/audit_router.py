from fastapi import APIRouter, Depends, Query
from app.database import get_db

router = APIRouter(prefix="/audit", tags=["audit"])

@router.get("/trail")
def get_audit_trail(entity_type: str | None = None, entity_id: str | None = None,
                    action: str | None = None, limit: int = 50, offset: int = 0, db=Depends(get_db)):
    query = """SELECT a.*, u.username, u.full_name
               FROM audit_log a
               LEFT JOIN users u ON u.id = a.user_id
               WHERE 1=1"""
    params: list = []
    if entity_type:
        query += " AND a.entity_type=?"
        params.append(entity_type)
    if entity_id:
        query += " AND a.entity_id=?"
        params.append(entity_id)
    if action:
        query += " AND a.action=?"
        params.append(action)

    count_q = query.replace("SELECT a.*, u.username, u.full_name", "SELECT COUNT(*) as total")
    cursor = db.execute(count_q, params)
    total = cursor.fetchone()["total"]

    query += " ORDER BY a.timestamp DESC LIMIT ? OFFSET ?"
    params.extend([limit, offset])
    cursor = db.execute(query, params)
    return {"total": total, "entries": [dict(row) for row in cursor.fetchall()]}

@router.get("/verify/{result_id}")
def verify_result(result_id: int, db=Depends(get_db)):
    cursor = db.execute("""
        SELECT a.* FROM audit_log a
        WHERE a.entity_type='result' AND a.entity_id=?
        ORDER BY a.timestamp ASC
    """, (str(result_id),))
    entries = [dict(row) for row in cursor.fetchall()]

    chain_valid = True
    for i in range(1, len(entries)):
        if entries[i]["prev_block_hash"] != entries[i-1]["block_hash"]:
            chain_valid = False
            break

    cursor = db.execute("SELECT * FROM results WHERE id=?", (result_id,))
    result = cursor.fetchone()

    return {
        "result_id": result_id,
        "audit_entries": entries,
        "chain_valid": chain_valid,
        "result_status": dict(result) if result else None,
        "dual_ledger": {
            "tigerbeetle_status": result["tigerbeetle_status"] if result else None,
            "hyperledger_status": result["hyperledger_status"] if result else None,
            "tigerbeetle_transfer_id": result["tigerbeetle_transfer_id"] if result else None,
            "hyperledger_tx_id": result["hyperledger_tx_id"] if result else None,
        } if result else None
    }

@router.get("/stats")
def audit_stats(db=Depends(get_db)):
    cursor = db.execute("SELECT action, COUNT(*) as count FROM audit_log GROUP BY action ORDER BY count DESC")
    action_counts = [dict(row) for row in cursor.fetchall()]

    cursor = db.execute("SELECT COUNT(*) as total FROM audit_log")
    total = cursor.fetchone()["total"]

    cursor = db.execute("SELECT block_hash FROM audit_log ORDER BY id DESC LIMIT 1")
    latest = cursor.fetchone()

    return {
        "total_entries": total,
        "action_counts": action_counts,
        "latest_block_hash": latest["block_hash"] if latest else None
    }
