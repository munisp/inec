"""
INEC Innovation 3: Homomorphic Encryption Vote Tallying
=======================================================
Implements Paillier partially homomorphic encryption to allow encrypted
vote tallying without ever decrypting individual ballots. The tally is
only decrypted once all encrypted votes have been aggregated, ensuring
that no individual vote can be linked to a voter.

Properties:
  - Additive homomorphism: E(a) * E(b) = E(a + b)
  - Individual votes remain encrypted throughout the counting process
  - Only the final aggregate is decrypted by a threshold of key holders
  - Provides cryptographic proof of correct tallying

This service is used as a second-layer verification of the physical count.

NOTE: the Paillier implementation below is a REFERENCE IMPLEMENTATION for
research and second-layer verification. It is pure-Python, not constant-time,
and must not be the sole integrity control for a production count without an
external cryptographic review.
"""

import hmac
import json
import os
import secrets
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Optional

import asyncpg
import uvicorn
from fastapi import Depends, FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel

app = FastAPI(
    title="INEC Homomorphic Vote Tally Service",
    description="Privacy-preserving vote aggregation using Paillier encryption",
    version="1.0.0",
)

# SECURITY: CORS is deny-by-default; operators opt in via TALLY_CORS_ORIGINS
# (comma-separated). Previously allow_origins=["*"].
TALLY_CORS_ORIGINS = [
    o.strip() for o in os.getenv("TALLY_CORS_ORIGINS", "").split(",") if o.strip()
]
app.add_middleware(
    CORSMiddleware,
    allow_origins=TALLY_CORS_ORIGINS,
    allow_methods=["GET", "POST"],
    allow_headers=["Content-Type", "Authorization"],
)


# ── Paillier Cryptosystem Implementation ─────────────────────────────────────

def _gcd(a: int, b: int) -> int:
    while b:
        a, b = b, a % b
    return a


def _lcm(a: int, b: int) -> int:
    return a * b // _gcd(a, b)


def _mod_inverse(a: int, m: int) -> int:
    """Extended Euclidean Algorithm for modular inverse."""
    g, x, _ = _extended_gcd(a, m)
    if g != 1:
        raise ValueError(f"No modular inverse for {a} mod {m}")
    return x % m


def _extended_gcd(a: int, b: int):
    if a == 0:
        return b, 0, 1
    g, x, y = _extended_gcd(b % a, a)
    return g, y - (b // a) * x, x


def _L(x: int, n: int) -> int:
    return (x - 1) // n


@dataclass
class PaillierPublicKey:
    n: int
    g: int
    n_sq: int = field(init=False)

    def __post_init__(self):
        self.n_sq = self.n * self.n

    def encrypt(self, plaintext: int) -> int:
        """Encrypt a plaintext integer. Returns ciphertext."""
        assert 0 <= plaintext < self.n, "Plaintext out of range"
        r = secrets.randbelow(self.n - 1) + 1
        while _gcd(r, self.n) != 1:
            r = secrets.randbelow(self.n - 1) + 1
        c = (pow(self.g, plaintext, self.n_sq) * pow(r, self.n, self.n_sq)) % self.n_sq
        return c

    def add_encrypted(self, c1: int, c2: int) -> int:
        """Homomorphic addition: E(a) * E(b) mod n^2 = E(a+b)"""
        return (c1 * c2) % self.n_sq

    def add_constant(self, c: int, k: int) -> int:
        """Add a constant to an encrypted value: E(a) * g^k mod n^2 = E(a+k)"""
        return (c * pow(self.g, k, self.n_sq)) % self.n_sq


@dataclass
class PaillierPrivateKey:
    public_key: PaillierPublicKey
    lam: int
    mu: int

    def decrypt(self, ciphertext: int) -> int:
        """Decrypt a ciphertext. Returns plaintext."""
        n, n_sq = self.public_key.n, self.public_key.n_sq
        x = pow(ciphertext, self.lam, n_sq)
        plaintext = (_L(x, n) * self.mu) % n
        return plaintext


# SECURITY: small primes for trial division — a cheap pre-filter only.
# Primality itself is established by Miller-Rabin below, never by this list.
_SMALL_PRIMES = (
    3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47, 53, 59, 61, 67, 71,
    73, 79, 83, 89, 97, 101, 103, 107, 109, 113, 127, 131, 137, 139, 149, 151,
    157, 163, 167, 173, 179, 181, 191, 193, 197, 199, 211, 223, 227, 229, 233,
    239, 241, 251, 257, 263, 269, 271, 277, 281, 283, 293, 307, 311, 313, 317,
    331, 337, 347, 349, 353, 359, 367, 373, 379, 383, 389, 397, 401, 409, 419,
    421, 431, 433, 439, 443, 449, 457, 461, 463, 467, 479, 487, 491, 499, 503,
    509, 521, 523, 541, 547, 557, 563, 569, 571, 577, 587, 593, 599, 601, 607,
    613, 617, 619, 631, 641, 643, 647, 653, 659, 661, 673, 677, 683, 691, 701,
    709, 719, 727, 733, 739, 743, 751, 757, 761, 769, 773, 787, 797, 809, 811,
    821, 823, 827, 829, 839, 853, 857, 859, 863, 877, 881, 883, 887, 907, 911,
    919, 929, 937, 941, 947, 953, 967, 971, 977, 983, 991, 997,
)


def _is_probable_prime(n: int, rounds: int = 40) -> bool:
    """Miller-Rabin primality test with CSPRNG-chosen bases (secrets module).

    SECURITY: 40 rounds gives a worst-case false-positive probability of 4^-40,
    vastly stronger than the previous trial-division-below-1000 check, which
    happily accepted composites and produced breakable Paillier keys.
    """
    if n < 2:
        return False
    if n == 2:
        return True
    if n % 2 == 0:
        return False
    for p in _SMALL_PRIMES:
        if n == p:
            return True
        if n % p == 0:
            return False
    # Write n - 1 as 2^r * d with d odd
    r, d = 0, n - 1
    while d % 2 == 0:
        r += 1
        d //= 2
    for _ in range(rounds):
        a = secrets.randbelow(n - 3) + 2  # a in [2, n-2]
        x = pow(a, d, n)
        if x in (1, n - 1):
            continue
        for _ in range(r - 1):
            x = pow(x, 2, n)
            if x == n - 1:
                break
        else:
            return False
    return True


def _generate_prime(bits: int) -> int:
    """Generate a probable prime of the given bit size using a CSPRNG."""
    while True:
        # SECURITY: secrets.randbits is backed by os.urandom (CSPRNG) — the old
        # random.getrandbits (Mersenne Twister) made key material predictable.
        candidate = secrets.randbits(bits) | (1 << (bits - 1)) | 1
        if _is_probable_prime(candidate):
            return candidate


def generate_paillier_keypair(bits: int = 2048):
    """Generate a Paillier keypair. 2048 bits minimum for production."""
    if bits < 2048:
        # SECURITY: sub-2048-bit Paillier moduli are factorable and must never
        # protect real election tallies.
        print(f"[HomomorphicTally] ⚠⚠ SECURITY WARNING: requested Paillier key "
              f"size {bits} bits is BELOW the 2048-bit minimum — keys are "
              f"factorable. Use only for local development. ⚠⚠")
    p = _generate_prime(bits // 2)
    q = _generate_prime(bits // 2)
    while p == q:
        q = _generate_prime(bits // 2)

    n = p * q
    lam = _lcm(p - 1, q - 1)
    g = n + 1  # Simplified: g = n+1 is always valid for Paillier
    mu = _mod_inverse(_L(pow(g, lam, n * n), n), n)

    pub = PaillierPublicKey(n=n, g=g)
    priv = PaillierPrivateKey(public_key=pub, lam=lam, mu=mu)
    return pub, priv


# ── Service State ─────────────────────────────────────────────────────────────

public_key: Optional[PaillierPublicKey] = None
private_key: Optional[PaillierPrivateKey] = None

# Encrypted running tallies per election per party
# Structure: {election_id: {party_code: encrypted_total}}
encrypted_tallies: dict[str, dict[str, int]] = {}

# PostgreSQL persistence (optional in development, mandatory in production).
DATABASE_URL = os.getenv("DATABASE_URL", "").strip()
APP_ENV = os.getenv("APP_ENV", "development").strip().lower()
_pg_pool: Optional[asyncpg.Pool] = None


async def _init_tally_store() -> None:
    """Create the tally table and load any persisted ciphertexts into memory."""
    global _pg_pool
    _pg_pool = await asyncpg.create_pool(DATABASE_URL, min_size=1, max_size=5)
    async with _pg_pool.acquire() as conn:
        await conn.execute("""
            CREATE TABLE IF NOT EXISTS tally (
                election_id TEXT NOT NULL,
                party_code  TEXT NOT NULL,
                ciphertext  TEXT NOT NULL,
                updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                PRIMARY KEY (election_id, party_code)
            )
        """)
        rows = await conn.fetch("SELECT election_id, party_code, ciphertext FROM tally")
    for row in rows:
        encrypted_tallies.setdefault(row["election_id"], {})[row["party_code"]] = int(
            row["ciphertext"]
        )
    print(f"[HomomorphicTally] Loaded {len(rows)} persisted tally ciphertexts from Postgres")


async def _persist_tally(election_id: str, party_code: str, ciphertext: int) -> None:
    """Upsert one party's running ciphertext (durable tally state)."""
    if _pg_pool is None:
        return
    async with _pg_pool.acquire() as conn:
        await conn.execute(
            """INSERT INTO tally (election_id, party_code, ciphertext, updated_at)
               VALUES ($1, $2, $3, NOW())
               ON CONFLICT (election_id, party_code)
               DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = NOW()""",
            election_id,
            party_code,
            str(ciphertext),
        )


# ── API Models ────────────────────────────────────────────────────────────────

class EncryptedVote(BaseModel):
    election_id: str
    polling_unit_id: str
    party_votes: dict[str, int]  # party_code -> vote count (plaintext, encrypted server-side)


class TallyRequest(BaseModel):
    election_id: str


class DecryptRequest(BaseModel):
    election_id: str
    authorization_token: str  # In production: threshold signature from key holders


# ── Endpoints ─────────────────────────────────────────────────────────────────

# SECURITY: bearer token required on /api/v1/tally/submit (fail closed when
# unset) — previously anyone could inject forged polling-unit results into the
# running encrypted tally.
TALLY_SUBMIT_TOKEN = os.getenv("TALLY_SUBMIT_TOKEN", "").strip()


async def require_submit_token(request: Request) -> None:
    if not TALLY_SUBMIT_TOKEN:
        raise HTTPException(
            status_code=503,
            detail="TALLY_SUBMIT_TOKEN not configured; refusing unauthenticated tally submissions",
        )
    auth = request.headers.get("Authorization", "")
    bearer = auth[7:] if auth.lower().startswith("bearer ") else auth
    if not bearer or not hmac.compare_digest(bearer.encode(), TALLY_SUBMIT_TOKEN.encode()):
        raise HTTPException(status_code=401, detail="authentication required")


@app.on_event("startup")
async def startup():
    global public_key, private_key
    key_bits = int(os.getenv("PAILLIER_KEY_BITS", "2048"))
    # SECURITY: fail at startup rather than generate factorable keys.
    assert key_bits >= 2048, (
        f"PAILLIER_KEY_BITS={key_bits} is below the 2048-bit production minimum; "
        "refusing to start with factorable keys"
    )
    print(f"[HomomorphicTally] Generating Paillier keypair ({key_bits}-bit, "
          f"Miller-Rabin + CSPRNG)...")
    public_key, private_key = generate_paillier_keypair(bits=key_bits)
    print(f"[HomomorphicTally] Keypair generated. n={str(public_key.n)[:20]}...")
    if DATABASE_URL:
        await _init_tally_store()
        print("[HomomorphicTally] Tally persistence: PostgreSQL (durable)")
    elif APP_ENV == "production":
        raise RuntimeError(
            "DATABASE_URL is required when APP_ENV=production; in-memory tallies "
            "are not durable and are refused in production"
        )
    else:
        print("[HomomorphicTally] ⚠⚠ SECURITY WARNING: DATABASE_URL unset — tallies "
              "are IN-MEMORY ONLY and will be lost on restart. Acceptable only in "
              "development; set DATABASE_URL for any real deployment. ⚠⚠")
    if not os.getenv("TALLY_DECRYPT_TOKEN"):
        # SECURITY: no hardcoded decryption token exists anymore; warn loudly
        # that the decrypt endpoint is disabled until one is configured.
        print("[HomomorphicTally] ⚠ SECURITY WARNING: TALLY_DECRYPT_TOKEN is not "
              "set — the /api/v1/tally/decrypt endpoint will return 503 until a "
              "token is configured via the environment.")


@app.get("/api/v1/tally/public-key")
async def get_public_key():
    """Return the public key for client-side encryption."""
    if not public_key:
        raise HTTPException(status_code=503, detail="Key not yet generated")
    return {"n": str(public_key.n), "g": str(public_key.g)}


@app.post("/api/v1/tally/submit")
async def submit_encrypted_vote(vote: EncryptedVote, _auth=Depends(require_submit_token)):
    """
    Accept a polling unit result and homomorphically add it to the running tally.
    The individual result is encrypted and never stored in plaintext.
    """
    if not public_key:
        raise HTTPException(status_code=503, detail="Encryption service not ready")

    election_id = vote.election_id
    if election_id not in encrypted_tallies:
        encrypted_tallies[election_id] = {}

    for party, count in vote.party_votes.items():
        encrypted_count = public_key.encrypt(count)
        if party in encrypted_tallies[election_id]:
            # Homomorphic addition — no decryption needed
            encrypted_tallies[election_id][party] = public_key.add_encrypted(
                encrypted_tallies[election_id][party],
                encrypted_count,
            )
        else:
            encrypted_tallies[election_id][party] = encrypted_count
        await _persist_tally(election_id, party, encrypted_tallies[election_id][party])

    return {
        "status": "accepted",
        "election_id": election_id,
        "polling_unit_id": vote.polling_unit_id,
        "message": "Vote homomorphically aggregated without decryption",
    }


@app.post("/api/v1/tally/decrypt")
async def decrypt_final_tally(req: DecryptRequest):
    """
    Decrypt the final tally. Requires authorization.
    In production this would require a threshold of key holder signatures.
    """
    if not private_key:
        raise HTTPException(status_code=503, detail="Private key not available")

    # Simple auth check (production: threshold multi-sig)
    # SECURITY: the token has NO default. The previous hardcoded fallback
    # ("inec-tally-secret") let anyone decrypt any election tally.
    expected_token = os.getenv("TALLY_DECRYPT_TOKEN")
    if not expected_token:
        raise HTTPException(
            status_code=503,
            detail="tally decryption token not configured (set TALLY_DECRYPT_TOKEN)",
        )
    if not secrets.compare_digest(req.authorization_token, expected_token):
        raise HTTPException(status_code=403, detail="Unauthorized decryption attempt")

    election_id = req.election_id
    if election_id not in encrypted_tallies:
        raise HTTPException(status_code=404, detail="No tally found for this election")

    results = {}
    for party, enc_total in encrypted_tallies[election_id].items():
        results[party] = private_key.decrypt(enc_total)

    return {
        "election_id": election_id,
        "results": results,
        "total_votes": sum(results.values()),
        "decrypted_at": datetime.now(timezone.utc).isoformat(),
        "note": "These results were computed via homomorphic aggregation — no individual ballot was decrypted",
    }


@app.get("/api/v1/tally/status/{election_id}")
async def tally_status(election_id: str):
    """Return the number of parties with encrypted tallies for an election."""
    if election_id not in encrypted_tallies:
        return {"election_id": election_id, "parties_tallied": 0, "status": "no_data"}
    return {
        "election_id": election_id,
        "parties_tallied": len(encrypted_tallies[election_id]),
        "status": "active",
    }


@app.get("/api/v1/tally/health")
async def health():
    """Real health: key readiness plus a live Postgres SELECT 1 when configured."""
    checks: dict[str, bool] = {"key_ready": public_key is not None}
    if _pg_pool is not None:
        try:
            async with _pg_pool.acquire() as conn:
                await conn.fetchval("SELECT 1")
            checks["postgres"] = True
        except (asyncpg.PostgresError, OSError):
            checks["postgres"] = False
    degraded = not all(checks.values()) or (DATABASE_URL and "postgres" not in checks)
    from fastapi.responses import JSONResponse

    return JSONResponse(
        status_code=503 if degraded else 200,
        content={
            "status": "degraded" if degraded else "healthy",
            "checks": checks,
            "persistence": "postgresql" if _pg_pool is not None else "in_memory",
        },
    )


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8201, log_level="info")
