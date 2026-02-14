from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from app.database import get_db
from app.auth import get_current_user

router = APIRouter(prefix="/incidents", tags=["incidents"])

class IncidentCreate(BaseModel):
    election_id: int
    polling_unit_code: str | None = None
    incident_type: str
    description: str
    severity: str = "medium"

@router.post("")
def create_incident(req: IncidentCreate, user=Depends(get_current_user), db=Depends(get_db)):
    cursor = db.execute(
        """INSERT INTO incidents (election_id, polling_unit_code, reported_by, incident_type, description, severity)
           VALUES (?,?,?,?,?,?)""",
        (req.election_id, req.polling_unit_code, int(user["sub"]), req.incident_type, req.description, req.severity)
    )
    db.commit()
    return {"id": cursor.lastrowid, "message": "Incident reported"}

@router.get("")
def list_incidents(election_id: int = 1, status: str | None = None, severity: str | None = None,
                   limit: int = 50, offset: int = 0, db=Depends(get_db)):
    query = "SELECT i.*, u.full_name as reporter_name FROM incidents i LEFT JOIN users u ON u.id=i.reported_by WHERE i.election_id=?"
    params: list = [election_id]
    if status:
        query += " AND i.status=?"
        params.append(status)
    if severity:
        query += " AND i.severity=?"
        params.append(severity)
    query += " ORDER BY i.reported_at DESC LIMIT ? OFFSET ?"
    params.extend([limit, offset])
    cursor = db.execute(query, params)
    return [dict(row) for row in cursor.fetchall()]

@router.patch("/{incident_id}")
def update_incident(incident_id: int, status: str, user=Depends(get_current_user), db=Depends(get_db)):
    resolved = ", resolved_at=CURRENT_TIMESTAMP" if status == "resolved" else ""
    db.execute(f"UPDATE incidents SET status=?{resolved} WHERE id=?", (status, incident_id))
    db.commit()
    return {"message": "Incident updated"}
