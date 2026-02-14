from fastapi import APIRouter, Depends, Query, Response, Request
from fastapi.responses import StreamingResponse
import io, csv, json, os, urllib.request
from app.database import get_db

router = APIRouter(prefix="/geo", tags=["geography"])

@router.get("/states")
def list_states(db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM states ORDER BY name")
    return [dict(row) for row in cursor.fetchall()]

@router.get("/states/{state_code}")
def get_state(state_code: str, db=Depends(get_db)):
    cursor = db.execute("SELECT * FROM states WHERE code=?", (state_code,))
    state = cursor.fetchone()
    if not state:
        return {"error": "State not found"}
    return dict(state)

@router.get("/lgas")
def list_lgas(state_code: str | None = None, db=Depends(get_db)):
    if state_code:
        cursor = db.execute("SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code WHERE l.state_code=? ORDER BY l.name", (state_code,))
    else:
        cursor = db.execute("SELECT l.*, s.name as state_name FROM lgas l JOIN states s ON s.code=l.state_code ORDER BY l.name")
    return [dict(row) for row in cursor.fetchall()]

@router.get("/wards")
def list_wards(lga_code: str | None = None, db=Depends(get_db)):
    if lga_code:
        cursor = db.execute("SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code WHERE w.lga_code=? ORDER BY w.name", (lga_code,))
    else:
        cursor = db.execute("SELECT w.*, l.name as lga_name FROM wards w JOIN lgas l ON l.code=w.lga_code ORDER BY w.name LIMIT 100")
    return [dict(row) for row in cursor.fetchall()]

@router.get("/polling-units")
def list_polling_units(ward_code: str | None = None, lga_code: str | None = None,
                       state_code: str | None = None, limit: int = 50, offset: int = 0, db=Depends(get_db)):
    query = """SELECT pu.*, w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
               FROM polling_units pu
               JOIN wards w ON w.code=pu.ward_code
               JOIN lgas l ON l.code=w.lga_code
               JOIN states s ON s.code=l.state_code"""
    params: list = []
    conditions = []
    if ward_code:
        conditions.append("pu.ward_code=?")
        params.append(ward_code)
    if lga_code:
        conditions.append("w.lga_code=?")
        params.append(lga_code)
    if state_code:
        conditions.append("l.state_code=?")
        params.append(state_code)
    if conditions:
        query += " WHERE " + " AND ".join(conditions)
    query += " ORDER BY pu.name LIMIT ? OFFSET ?"
    params.extend([limit, offset])
    cursor = db.execute(query, params)
    return [dict(row) for row in cursor.fetchall()]

@router.get("/polling-units/{pu_code}")
def get_polling_unit(pu_code: str, db=Depends(get_db)):
    cursor = db.execute("""SELECT pu.*, w.name as ward_name, w.lga_code, l.name as lga_name, l.state_code, s.name as state_name
                           FROM polling_units pu
                           JOIN wards w ON w.code=pu.ward_code
                           JOIN lgas l ON l.code=w.lga_code
                           JOIN states s ON s.code=l.state_code
                           WHERE pu.code=?""", (pu_code,))
    row = cursor.fetchone()
    if not row:
        return {"error": "Polling unit not found"}
    return dict(row)

@router.get("/map-data")
def get_map_data(election_id: int = Query(default=1), state_code: str | None = None, db=Depends(get_db)):
    state_query = """
        SELECT s.code, s.name, s.geo_zone, s.capital,
               COUNT(DISTINCT pu.code) as total_pus,
               COUNT(DISTINCT r.id) as reported_pus,
               COALESCE(SUM(r.total_valid_votes), 0) as total_votes,
               COALESCE(SUM(r.total_votes_cast), 0) as total_cast,
               COALESCE(SUM(r.accredited_voters), 0) as accredited,
               AVG(pu.latitude) as avg_lat, AVG(pu.longitude) as avg_lng
        FROM states s
        LEFT JOIN lgas l ON l.state_code = s.code
        LEFT JOIN wards w ON w.lga_code = l.code
        LEFT JOIN polling_units pu ON pu.ward_code = w.code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=? AND r.status IN ('finalized','validated')
        GROUP BY s.code ORDER BY s.name
    """
    cursor = db.execute(state_query, (election_id,))
    states = [dict(row) for row in cursor.fetchall()]

    for s in states:
        cursor2 = db.execute("""
            SELECT rps.party_code, p.abbreviation, p.color, SUM(rps.votes) as total_votes
            FROM result_party_scores rps
            JOIN results res ON res.id = rps.result_id
            JOIN polling_units pu ON pu.code = res.polling_unit_code
            JOIN wards w ON w.code = pu.ward_code
            JOIN lgas l ON l.code = w.lga_code
            JOIN parties p ON p.code = rps.party_code
            WHERE l.state_code=? AND res.election_id=? AND res.status IN ('finalized','validated')
            GROUP BY rps.party_code ORDER BY total_votes DESC
        """, (s["code"], election_id))
        s["party_scores"] = [dict(row) for row in cursor2.fetchall()]
        s["leading_party"] = s["party_scores"][0] if s["party_scores"] else None

    pu_query = """
        SELECT pu.code, pu.name, pu.latitude, pu.longitude, pu.registered_voters,
               w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
               r.id as result_id, r.status, r.total_valid_votes, r.total_votes_cast,
               r.tigerbeetle_status, r.hyperledger_status
        FROM polling_units pu
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
    """
    params: list = [election_id]
    if state_code:
        pu_query += " WHERE l.state_code=?"
        params.append(state_code)
    pu_query += " LIMIT 2000"
    cursor = db.execute(pu_query, params)
    polling_units = [dict(row) for row in cursor.fetchall()]

    for pu in polling_units:
        if pu.get("result_id"):
            cursor2 = db.execute("""
                SELECT rps.party_code, p.abbreviation, p.color, rps.votes
                FROM result_party_scores rps
                JOIN parties p ON p.code = rps.party_code
                WHERE rps.result_id=? ORDER BY rps.votes DESC
            """, (pu["result_id"],))
            pu["party_scores"] = [dict(row) for row in cursor2.fetchall()]
        else:
            pu["party_scores"] = []

    return {"states": states, "polling_units": polling_units}

from math import pi, atan, sinh, degrees

@router.get("/tiles/pus/{z}/{x}/{y}.mvt")
def get_pu_tile(z: int, x: int, y: int, request: Request, election_id: int = Query(default=1), db=Depends(get_db)):
    # Optional proxy to Go tile service for performance (Phase 1)
    go_tile = os.getenv('GO_TILE_URL')
    if go_tile:
        try:
            url = f"{go_tile.rstrip('/')}/tiles/pus/{z}/{x}/{y}.mvt?election_id={election_id}"
            req = urllib.request.Request(url, headers={"User-Agent": "INEC-TileProxy", "If-None-Match": request.headers.get('if-none-match', '') or ''})
            with urllib.request.urlopen(req) as r:
                data = r.read()
                resp = Response(content=data, media_type="application/vnd.mapbox-vector-tile", status_code=r.status)
                et = r.headers.get('ETag')
                cc = r.headers.get('Cache-Control')
                if et: resp.headers['ETag'] = et
                if cc: resp.headers['Cache-Control'] = cc
                return resp
        except urllib.error.HTTPError as e:
            if e.code == 304:
                return Response(status_code=304)
            # fall through to Python tilegen if proxy fails
        except Exception:
            pass
    try:
        from mapbox_vector_tile import encode
    except Exception:
        return Response(content=b"", media_type="application/vnd.mapbox-vector-tile")

    n = 2 ** z
    lon_min = x / n * 360.0 - 180.0
    lon_max = (x + 1) / n * 360.0 - 180.0
    lat_rad_max = atan(sinh(pi * (1 - 2 * y / n)))
    lat_rad_min = atan(sinh(pi * (1 - 2 * (y + 1) / n)))
    lat_max = degrees(lat_rad_max)
    lat_min = degrees(lat_rad_min)

    cursor = db.execute(
        """
        SELECT pu.code, pu.name, pu.latitude AS lat, pu.longitude AS lon,
               COALESCE(r.status, 'no_result') as status,
               r.submitted_at as submitted_at,
               CAST(strftime('%s', r.submitted_at) AS INTEGER) as submitted_ts
        FROM polling_units pu
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
        WHERE pu.longitude BETWEEN ? AND ? AND pu.latitude BETWEEN ? AND ?
        LIMIT 10000
        """,
        (election_id, lon_min, lon_max, lat_min, lat_max),
    )
    rows = [dict(r) for r in cursor.fetchall()]

    features = [
        {
            "geometry": {"type": "Point", "coordinates": [row["lon"], row["lat"]]},
            "properties": {
                "code": row["code"],
                "name": row["name"],
                "status": row["status"],
                "submitted_at": row.get("submitted_at"),
                "submitted_ts": row.get("submitted_ts"),
            },
        }
        for row in rows
    ]

    tile = encode({"pus": features}, quantize_bounds=(lon_min, lat_min, lon_max, lat_max), extents=4096)

    # ETag + caching headers
    try:
        import hashlib
        etag = 'W/"' + hashlib.md5(tile).hexdigest() + '"'
    except Exception:
        etag = None

    inm = request.headers.get('if-none-match') if request else None
    if etag and inm == etag:
        return Response(status_code=304)

    resp = Response(content=tile, media_type="application/vnd.mapbox-vector-tile")
    if etag:
        resp.headers["ETag"] = etag
    resp.headers["Cache-Control"] = "public, max-age=300, stale-while-revalidate=600"
    return resp

@router.get("/reports/polling-units.csv")
def export_pus_csv(election_id: int = Query(default=1), state_code: str | None = None, db=Depends(get_db)):
    query = """
        SELECT pu.code, pu.name, pu.latitude, pu.longitude, pu.registered_voters,
               w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
               COALESCE(r.status, 'no_result') as status, COALESCE(r.total_valid_votes,0) as total_valid_votes,
               COALESCE(r.total_votes_cast,0) as total_votes_cast, r.submitted_at
        FROM polling_units pu
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
    """
    params: list = [election_id]
    if state_code:
        query += " WHERE l.state_code=?"
        params.append(state_code)
    cursor = db.execute(query, params)
    rows = [dict(r) for r in cursor.fetchall()]

    buf = io.StringIO()
    writer = csv.writer(buf)
    headers = ["code","name","ward_name","lga_name","state_code","state_name","status","registered_voters","total_valid_votes","total_votes_cast","latitude","longitude","submitted_at"]
    writer.writerow(headers)
    for r in rows:
        writer.writerow([
            r["code"], r["name"], r["ward_name"], r["lga_name"], r["state_code"], r["state_name"], r["status"],
            r["registered_voters"], r["total_valid_votes"], r["total_votes_cast"], r["latitude"], r["longitude"], r.get("submitted_at")
        ])
    content = buf.getvalue().encode("utf-8")
    resp = Response(content=content, media_type="text/csv; charset=utf-8")
    filename = f"polling_units{'_'+state_code if state_code else ''}.csv"
    resp.headers["Content-Disposition"] = f"attachment; filename={filename}"
    resp.headers["Cache-Control"] = "no-store"
    return resp

@router.get("/reports/polling-units.geojson")
def export_pus_geojson(election_id: int = Query(default=1), state_code: str | None = None, db=Depends(get_db)):
    query = """
        SELECT pu.code, pu.name, pu.latitude, pu.longitude,
               w.name as ward_name, l.name as lga_name, l.state_code, s.name as state_name,
               COALESCE(r.status, 'no_result') as status, r.submitted_at
        FROM polling_units pu
        JOIN wards w ON w.code = pu.ward_code
        JOIN lgas l ON l.code = w.lga_code
        JOIN states s ON s.code = l.state_code
        LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id=?
    """
    params: list = [election_id]
    if state_code:
        query += " WHERE l.state_code=?"
        params.append(state_code)
    cursor = db.execute(query, params)
    rows = [dict(r) for r in cursor.fetchall()]

    features = []
    for r in rows:
        if r.get("longitude") is None or r.get("latitude") is None:
            continue
        features.append({
            "type": "Feature",
            "geometry": {"type": "Point", "coordinates": [r["longitude"], r["latitude"]]},
            "properties": {
                "code": r["code"],
                "name": r["name"],
                "ward": r["ward_name"],
                "lga": r["lga_name"],
                "state": r["state_name"],
                "status": r["status"],
                "submitted_at": r.get("submitted_at"),
            }
        })
    gj = {"type": "FeatureCollection", "features": features}
    content = json.dumps(gj).encode("utf-8")
    resp = Response(content=content, media_type="application/geo+json; charset=utf-8")
    filename = f"polling_units{'_'+state_code if state_code else ''}.geojson"
    resp.headers["Content-Disposition"] = f"attachment; filename={filename}"
    resp.headers["Cache-Control"] = "no-store"
    return resp
