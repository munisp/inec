from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from app.database import get_db
from app.auth import get_current_user, require_role

router = APIRouter(prefix="/elections", tags=["elections"])

class ElectionCreate(BaseModel):
    title: str
    election_type: str
    election_date: str
    description: str | None = None
    status: str = "upcoming"

class ElectionUpdate(BaseModel):
    title: str | None = None
    status: str | None = None
    description: str | None = None

@router.get("")
def list_elections(status: str | None = None, db=Depends(get_db)):
    if status:
        cursor = db.execute("SELECT * FROM elections WHERE status=? ORDER BY election_date DESC", (status,))
    else:
        cursor = db.execute("SELECT * FROM elections ORDER BY election_date DESC")
    return [dict(row) for row in cursor.fetchall()]

@router.get("/{election_id}")
def get_election(election_id: int, db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM elections WHERE id=?", (election_id,))
    row = cursor.fetchone()
    if not row:
        raise HTTPException(status_code=404, detail="Election not found")
    return dict(row)

@router.post("")
def create_election(req: ElectionCreate, user=Depends(require_role("admin")), db=Depends(get_db)):
    cursor = db.execute(
        "INSERT INTO elections (title, election_type, election_date, status, description) VALUES (?,?,?,?,?)",
        (req.title, req.election_type, req.election_date, req.status, req.description)
    )
    db.commit()
    return {"id": cursor.lastrowid, "message": "Election created"}

@router.patch("/{election_id}")
def update_election(election_id: int, req: ElectionUpdate, user=Depends(require_role("admin")), db=Depends(get_db)):
    updates = []
    values = []
    if req.title:
        updates.append("title=?")
        values.append(req.title)
    if req.status:
        updates.append("status=?")
        values.append(req.status)
    if req.description:
        updates.append("description=?")
        values.append(req.description)
    if not updates:
        raise HTTPException(status_code=400, detail="No fields to update")
    updates.append("updated_at=CURRENT_TIMESTAMP")
    values.append(election_id)
    db.execute(f"UPDATE elections SET {','.join(updates)} WHERE id=?", values)
    db.commit()
    return {"message": "Election updated"}

@router.get("/{election_id}/stats")
def get_election_stats(election_id: int, db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM elections WHERE id=?", (election_id,))
    election = cursor.fetchone()
    if not election:
        raise HTTPException(status_code=404, detail="Election not found")

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=?", (election_id,))
    total_results = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=? AND status='finalized'", (election_id,))
    finalized = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=? AND status='validated'", (election_id,))
    validated = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=? AND status='pending'", (election_id,))
    pending = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM results WHERE election_id=? AND status='disputed'", (election_id,))
    disputed = cursor.fetchone()["total"]

    cursor = db.execute("SELECT COUNT(*) as total FROM polling_units")
    total_pus = cursor.fetchone()["total"]

    cursor = db.execute("""
        SELECT SUM(total_valid_votes) as valid, SUM(rejected_votes) as rejected,
               SUM(total_votes_cast) as cast, SUM(accredited_voters) as accredited
        FROM results WHERE election_id=? AND status IN ('finalized','validated')
    """, (election_id,))
    vote_totals = cursor.fetchone()

    cursor = db.execute("""
        SELECT rps.party_code, p.name as party_name, p.color, SUM(rps.votes) as total_votes
        FROM result_party_scores rps
        JOIN results r ON r.id = rps.result_id
        JOIN parties p ON p.code = rps.party_code
        WHERE r.election_id=? AND r.status IN ('finalized','validated')
        GROUP BY rps.party_code
        ORDER BY total_votes DESC
    """, (election_id,))
    party_scores = [dict(row) for row in cursor.fetchall()]

    return {
        "election": dict(election),
        "total_polling_units": total_pus,
        "results_received": total_results,
        "results_finalized": finalized,
        "results_validated": validated,
        "results_pending": pending,
        "results_disputed": disputed,
        "completion_percentage": round((total_results / total_pus * 100), 2) if total_pus > 0 else 0,
        "total_valid_votes": vote_totals["valid"] or 0,
        "total_rejected_votes": vote_totals["rejected"] or 0,
        "total_votes_cast": vote_totals["cast"] or 0,
        "total_accredited_voters": vote_totals["accredited"] or 0,
        "party_scores": party_scores
    }
