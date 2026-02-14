from fastapi import APIRouter, Depends, HTTPException
from pydantic import BaseModel
from app.database import get_db
from app.auth import hash_password, verify_password, create_access_token, get_current_user

router = APIRouter(prefix="/auth", tags=["auth"])

class LoginRequest(BaseModel):
    username: str
    password: str

class RegisterRequest(BaseModel):
    username: str
    password: str
    full_name: str
    role: str = "public"
    staff_id: str | None = None
    state_code: str | None = None

class TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"
    user: dict

@router.post("/login", response_model=TokenResponse)
def login(req: LoginRequest, db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM users WHERE username=? AND is_active=1", (req.username,))
    user = cursor.fetchone()
    if not user or not verify_password(req.password, user["password_hash"]):
        raise HTTPException(status_code=401, detail="Invalid credentials")

    token = create_access_token({
        "sub": str(user["id"]),
        "username": user["username"],
        "role": user["role"],
        "full_name": user["full_name"]
    })

    return TokenResponse(
        access_token=token,
        user={
            "id": user["id"],
            "username": user["username"],
            "full_name": user["full_name"],
            "role": user["role"],
            "staff_id": user["staff_id"],
            "state_code": user["state_code"]
        }
    )

@router.post("/register", response_model=TokenResponse)
def register(req: RegisterRequest, db=Depends(get_db)):
    cursor = db.execute("SELECT id FROM users WHERE username=?", (req.username,))
    if cursor.fetchone():
        raise HTTPException(status_code=400, detail="Username already exists")

    pw_hash = hash_password(req.password)
    cursor = db.execute(
        "INSERT INTO users (username, password_hash, full_name, role, staff_id, state_code) VALUES (?,?,?,?,?,?)",
        (req.username, pw_hash, req.full_name, req.role, req.staff_id, req.state_code)
    )
    db.commit()
    user_id = cursor.lastrowid

    token = create_access_token({
        "sub": str(user_id),
        "username": req.username,
        "role": req.role,
        "full_name": req.full_name
    })

    return TokenResponse(
        access_token=token,
        user={
            "id": user_id,
            "username": req.username,
            "full_name": req.full_name,
            "role": req.role,
            "staff_id": req.staff_id,
            "state_code": req.state_code
        }
    )

@router.get("/me")
def get_me(user=Depends(get_current_user)):
    return user
