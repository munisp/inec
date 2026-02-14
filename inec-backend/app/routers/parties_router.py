from fastapi import APIRouter, Depends
from app.database import get_db

router = APIRouter(prefix="/parties", tags=["parties"])

@router.get("")
def list_parties(db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM parties WHERE is_active=1 ORDER BY name")
    return [dict(row) for row in cursor.fetchall()]
