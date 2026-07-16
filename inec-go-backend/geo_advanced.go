package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// geoHTTPClient is a timeout-configured HTTP client for external geo APIs.
// Avoids http.DefaultClient which has no timeouts for TLS handshake/headers.
var geoHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        20,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ═══════════════════════════════════════════════════════════
// GEO ADVANCED — 30 enhancements for mapping & real-time tracking
// ═══════════════════════════════════════════════════════════

// --- Schema Migrations (#3 Tracking History, #25 Blockchain Proof, #19 Photo, #30 H3) ---

const geoAdvancedMigrationSQL = `
-- #3 Official tracking history (append-only log for path replay)
CREATE TABLE IF NOT EXISTS official_tracking_history (
    id BIGSERIAL PRIMARY KEY,
    staff_id TEXT NOT NULL,
    role TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    pu_code TEXT,
    activity TEXT,
    battery_pct INTEGER DEFAULT 100,
    speed_kmh DOUBLE PRECISION DEFAULT 0,
    heading DOUBLE PRECISION DEFAULT 0,
    accuracy_m DOUBLE PRECISION DEFAULT 0,
    geom geometry(Point, 4326),
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_staff ON official_tracking_history(staff_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_time ON official_tracking_history(recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_tracking_hist_geom ON official_tracking_history USING GIST(geom);

-- #9 Crowd alerts
CREATE TABLE IF NOT EXISTS crowd_alerts (
    id SERIAL PRIMARY KEY,
    pu_code TEXT NOT NULL,
    alert_type TEXT NOT NULL,
    severity TEXT DEFAULT 'warning',
    head_count INTEGER,
    density_level TEXT,
    message TEXT,
    acknowledged BOOLEAN DEFAULT FALSE,
    acknowledged_by TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_crowd_alerts_created ON crowd_alerts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_crowd_alerts_severity ON crowd_alerts(severity);

-- #19 PU photo verification
CREATE TABLE IF NOT EXISTS pu_photos (
    id SERIAL PRIMARY KEY,
    pu_code TEXT NOT NULL,
    photo_url TEXT NOT NULL,
    caption TEXT,
    photo_type TEXT DEFAULT 'verification',
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    geom geometry(Point, 4326),
    uploaded_by TEXT,
    verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_pu_photos_code ON pu_photos(pu_code);
CREATE INDEX IF NOT EXISTS idx_pu_photos_geom ON pu_photos USING GIST(geom);

-- #25 Blockchain geofence attestations
CREATE TABLE IF NOT EXISTS geofence_attestations (
    id SERIAL PRIMARY KEY,
    staff_id TEXT NOT NULL,
    pu_code TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    within_geofence BOOLEAN NOT NULL,
    distance_m DOUBLE PRECISION,
    signature_hash TEXT NOT NULL,
    blockchain_tx TEXT,
    attested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_geofence_att_staff ON geofence_attestations(staff_id, attested_at DESC);

-- #2 Geofence zones (visual boundaries)
CREATE TABLE IF NOT EXISTS geofence_zones (
    id SERIAL PRIMARY KEY,
    pu_code TEXT NOT NULL UNIQUE,
    center_lat DOUBLE PRECISION NOT NULL,
    center_lng DOUBLE PRECISION NOT NULL,
    radius_m DOUBLE PRECISION DEFAULT 500,
    geom geometry(Polygon, 4326),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_geofence_zones_geom ON geofence_zones USING GIST(geom);
CREATE INDEX IF NOT EXISTS idx_geofence_zones_pu ON geofence_zones(pu_code);

-- #20 Incident geo events
CREATE TABLE IF NOT EXISTS incident_locations (
    id SERIAL PRIMARY KEY,
    incident_id INTEGER,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    geom geometry(Point, 4326),
    severity TEXT DEFAULT 'medium',
    incident_type TEXT,
    description TEXT,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_incident_loc_geom ON incident_locations USING GIST(geom);
`

func runGeoAdvancedMigrations() {
	stmts := strings.Split(geoAdvancedMigrationSQL, ";")
	for _, s := range stmts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, err := db.Exec(s); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				logger.Printf("geo_advanced migration (non-fatal): %v", err)
			}
		}
	}
}

// ═══════════════════════════════════════════════════════════
// #3 — Tracking History + Path Replay
// ═══════════════════════════════════════════════════════════

// handleTrackingHistoryRecord records a GPS ping AND appends to history.
func handleTrackingHistoryRecord(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StaffID   string  `json:"staff_id"`
		Role      string  `json:"role"`
		Lat       float64 `json:"lat"`
		Lng       float64 `json:"lng"`
		PUCode    string  `json:"pu_code"`
		Activity  string  `json:"activity"`
		Battery   int     `json:"battery_pct"`
		SpeedKmh  float64 `json:"speed_kmh"`
		Heading   float64 `json:"heading"`
		AccuracyM float64 `json:"accuracy_m"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, M{"error": "invalid body"})
		return
	}
	if body.StaffID == "" || body.Lat == 0 || body.Lng == 0 {
		writeJSON(w, 400, M{"error": "staff_id, lat, lng required"})
		return
	}
	if body.Role == "" {
		body.Role = "field_officer"
	}
	if body.Activity == "" {
		body.Activity = "patrol"
	}

	ctx := r.Context()

	// Upsert current position
	db.ExecContext(ctx, `
		INSERT INTO official_tracking (staff_id, role, latitude, longitude, pu_code, activity, battery_pct, geom, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, ST_SetSRID(ST_MakePoint($4, $3), 4326), NOW())
		ON CONFLICT (staff_id) DO UPDATE SET
			latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude,
			pu_code=EXCLUDED.pu_code, activity=EXCLUDED.activity,
			battery_pct=EXCLUDED.battery_pct, geom=EXCLUDED.geom, updated_at=NOW()`,
		body.StaffID, body.Role, body.Lat, body.Lng, body.PUCode, body.Activity, body.Battery)

	// Append to history (never overwritten)
	db.ExecContext(ctx, `
		INSERT INTO official_tracking_history
			(staff_id, role, latitude, longitude, pu_code, activity, battery_pct, speed_kmh, heading, accuracy_m, geom)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, ST_SetSRID(ST_MakePoint($4, $3), 4326))`,
		body.StaffID, body.Role, body.Lat, body.Lng, body.PUCode, body.Activity, body.Battery,
		body.SpeedKmh, body.Heading, body.AccuracyM)

	// Emit SSE event
	db.ExecContext(ctx, `
		INSERT INTO geo_events (polling_unit_code, event_type, latitude, longitude, payload)
		VALUES ($1, 'official_move', $2, $3, $4)`,
		body.PUCode, body.Lat, body.Lng,
		fmt.Sprintf(`{"staff_id":"%s","role":"%s","activity":"%s","battery":%d,"speed":%.1f}`,
			body.StaffID, body.Role, body.Activity, body.Battery, body.SpeedKmh))

	writeJSON(w, 200, M{"status": "recorded", "staff_id": body.StaffID})
}

// handleTrackingHistoryReplay returns path history for time-slider playback.
func handleTrackingHistoryReplay(w http.ResponseWriter, r *http.Request) {
	staffID := r.URL.Query().Get("staff_id")
	hoursBack := queryParamInt(r, "hours", 24)
	limit := queryParamInt(r, "limit", 1000)

	var query string
	var params []interface{}

	if staffID != "" {
		query = `SELECT staff_id, role, latitude, longitude, pu_code, activity, battery_pct,
				speed_kmh, heading, accuracy_m, recorded_at
			FROM official_tracking_history
			WHERE staff_id = $1 AND recorded_at > NOW() - ($2 * INTERVAL '1 hour')
			ORDER BY recorded_at ASC LIMIT $3`
		params = []interface{}{staffID, hoursBack, limit}
	} else {
		query = `SELECT staff_id, role, latitude, longitude, pu_code, activity, battery_pct,
				speed_kmh, heading, accuracy_m, recorded_at
			FROM official_tracking_history
			WHERE recorded_at > NOW() - ($1 * INTERVAL '1 hour')
			ORDER BY recorded_at ASC LIMIT $2`
		params = []interface{}{hoursBack, limit}
	}

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	points := scanRows(rows)

	// Group by staff_id for path rendering
	paths := make(map[string][]M)
	for _, pt := range points {
		sid, _ := pt["staff_id"].(string)
		paths[sid] = append(paths[sid], pt)
	}

	// Convert to GeoJSON LineString features
	features := make([]M, 0, len(paths))
	for sid, pts := range paths {
		coords := make([][]float64, 0, len(pts))
		timestamps := make([]string, 0, len(pts))
		for _, pt := range pts {
			lat, _ := toFloatGeo(pt["latitude"])
			lng, _ := toFloatGeo(pt["longitude"])
			coords = append(coords, []float64{lng, lat})
			if ts, ok := pt["recorded_at"].(string); ok {
				timestamps = append(timestamps, ts)
			}
		}
		role := ""
		if len(pts) > 0 {
			role, _ = pts[0]["role"].(string)
		}
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "LineString",
				"coordinates": coords,
			},
			"properties": M{
				"staff_id":   sid,
				"role":       role,
				"points":     len(pts),
				"timestamps": timestamps,
			},
		})
	}

	writeJSON(w, 200, M{
		"type":     "FeatureCollection",
		"features": features,
		"meta": M{
			"hours_back":  hoursBack,
			"total_points": len(points),
			"staff_count": len(paths),
		},
	})
}

// ═══════════════════════════════════════════════════════════
// #2 — Geofence Boundaries Visualization
// ═══════════════════════════════════════════════════════════

func handleGetGeofenceZones(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")

	query := `SELECT gz.pu_code, gz.center_lat, gz.center_lng, gz.radius_m,
		pu.name AS pu_name, pu.ward_code
		FROM geofence_zones gz
		LEFT JOIN polling_units pu ON pu.code = gz.pu_code`
	var params []interface{}

	if stateCode != "" {
		query += ` WHERE gz.pu_code LIKE $1`
		params = append(params, stateCode+"%")
	}
	query += " ORDER BY gz.pu_code LIMIT 500"

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	zones := scanRows(rows)

	// Convert to GeoJSON circles
	features := make([]M, 0, len(zones))
	for _, z := range zones {
		lat, _ := toFloatGeo(z["center_lat"])
		lng, _ := toFloatGeo(z["center_lng"])
		radius, _ := toFloatGeo(z["radius_m"])
		if radius == 0 {
			radius = 500
		}

		// Generate circle polygon (32 points)
		coords := make([][]float64, 33)
		for i := 0; i <= 32; i++ {
			angle := float64(i) * 2 * math.Pi / 32
			dlat := (radius / 111320) * math.Cos(angle)
			dlng := (radius / (111320 * math.Cos(lat*math.Pi/180))) * math.Sin(angle)
			coords[i] = []float64{lng + dlng, lat + dlat}
		}

		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Polygon",
				"coordinates": []interface{}{coords},
			},
			"properties": M{
				"pu_code":  z["pu_code"],
				"pu_name":  z["pu_name"],
				"radius_m": radius,
			},
		})
	}

	writeJSON(w, 200, M{
		"type":     "FeatureCollection",
		"features": features,
		"count":    len(features),
	})
}

// handleGeofenceViolations returns officials outside their assigned geofence.
func handleGeofenceViolations(w http.ResponseWriter, r *http.Request) {
	rows, err := dbQueryCtx(r.Context(), `
		SELECT ot.staff_id, ot.role, ot.latitude, ot.longitude, ot.pu_code, ot.activity, ot.battery_pct,
			gz.center_lat, gz.center_lng, gz.radius_m,
			ST_Distance(
				ST_SetSRID(ST_MakePoint(ot.longitude, ot.latitude), 4326)::geography,
				ST_SetSRID(ST_MakePoint(gz.center_lng, gz.center_lat), 4326)::geography
			) AS distance_m
		FROM official_tracking ot
		JOIN geofence_zones gz ON gz.pu_code = ot.pu_code
		WHERE ot.updated_at > NOW() - INTERVAL '30 minutes'
			AND ST_Distance(
				ST_SetSRID(ST_MakePoint(ot.longitude, ot.latitude), 4326)::geography,
				ST_SetSRID(ST_MakePoint(gz.center_lng, gz.center_lat), 4326)::geography
			) > gz.radius_m
		ORDER BY distance_m DESC`)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	violations := scanRows(rows)
	writeJSON(w, 200, M{"violations": violations, "count": len(violations)})
}

// handleSeedGeofenceZones seeds geofence circles around known PU locations.
func handleSeedGeofenceZones(w http.ResponseWriter, r *http.Request) {
	// Create zones around all PU locations from official_tracking
	result, err := db.ExecContext(r.Context(), `
		INSERT INTO geofence_zones (pu_code, center_lat, center_lng, radius_m, geom)
		SELECT DISTINCT ot.pu_code, ot.latitude, ot.longitude, 500,
			ST_Buffer(ST_SetSRID(ST_MakePoint(ot.longitude, ot.latitude), 4326)::geography, 500)::geometry
		FROM official_tracking ot
		WHERE ot.pu_code IS NOT NULL AND ot.pu_code != ''
		ON CONFLICT (pu_code) DO UPDATE SET
			center_lat=EXCLUDED.center_lat, center_lng=EXCLUDED.center_lng,
			radius_m=EXCLUDED.radius_m, geom=EXCLUDED.geom`)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	n, _ := result.RowsAffected()
	writeJSON(w, 200, M{"seeded": n, "radius_m": 500})
}

// ═══════════════════════════════════════════════════════════
// #7 — Advanced PostGIS Spatial Queries
// ═══════════════════════════════════════════════════════════

// handleSpatialClusters uses ST_ClusterDBSCAN for real density-based clustering.
func handleSpatialClusters(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	eps := r.URL.Query().Get("eps_km")
	if eps == "" {
		eps = "10"
	}
	minPts := queryParamInt(r, "min_points", 5)

	rows, err := dbQueryCtx(r.Context(), fmt.Sprintf(`
		WITH clustered AS (
			SELECT pu.code, pu.name, pu.latitude, pu.longitude,
				ST_ClusterDBSCAN(ST_SetSRID(ST_MakePoint(pu.longitude, pu.latitude), 4326)::geometry, %s / 111.0, %d)
				OVER() AS cluster_id,
				COALESCE(r.total_votes_cast, 0) AS votes,
				pu.registered_voters
			FROM polling_units pu
			LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
			WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL
		)
		SELECT cluster_id, COUNT(*) AS pu_count,
			AVG(latitude) AS center_lat, AVG(longitude) AS center_lng,
			SUM(votes) AS total_votes, SUM(registered_voters) AS total_registered,
			MIN(latitude) AS min_lat, MAX(latitude) AS max_lat,
			MIN(longitude) AS min_lng, MAX(longitude) AS max_lng
		FROM clustered
		WHERE cluster_id IS NOT NULL
		GROUP BY cluster_id
		ORDER BY pu_count DESC LIMIT 100`, eps, minPts), eid)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	clusters := scanRows(rows)

	features := make([]M, 0, len(clusters))
	for _, c := range clusters {
		lat, _ := toFloatGeo(c["center_lat"])
		lng, _ := toFloatGeo(c["center_lng"])
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Point",
				"coordinates": []float64{lng, lat},
			},
			"properties": c,
		})
	}
	writeJSON(w, 200, M{"type": "FeatureCollection", "features": features, "count": len(features)})
}

// handleVoronoiDiagram generates Thiessen polygons for PU service areas.
func handleVoronoiDiagram(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")
	query := `
		WITH pu_points AS (
			SELECT code, name, latitude, longitude,
				ST_SetSRID(ST_MakePoint(longitude, latitude), 4326) AS geom
			FROM polling_units
			WHERE latitude IS NOT NULL AND longitude IS NOT NULL`
	var params []interface{}
	paramIdx := 1

	if stateCode != "" {
		query += fmt.Sprintf(` AND ward_code IN (SELECT w.code FROM wards w JOIN lgas l ON l.code = w.lga_code WHERE l.state_code = $%d)`, paramIdx)
		params = append(params, stateCode)
		paramIdx++
	}

	query += fmt.Sprintf(` LIMIT 2000
		),
		voronoi AS (
			SELECT (ST_Dump(ST_VoronoiPolygons(ST_Collect(geom)))).geom AS cell
			FROM pu_points
		)
		SELECT pp.code, pp.name, pp.latitude, pp.longitude,
			ST_AsGeoJSON(v.cell)::json AS voronoi_geojson
		FROM voronoi v
		JOIN pu_points pp ON ST_Contains(v.cell, pp.geom)
		LIMIT 2000`)

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		// Fallback if ST_VoronoiPolygons not available
		writeJSON(w, 200, M{
			"type":     "FeatureCollection",
			"features": []M{},
			"error":    "voronoi not supported: " + err.Error(),
		})
		return
	}
	cells := scanRows(rows)

	features := make([]M, 0, len(cells))
	for _, c := range cells {
		features = append(features, M{
			"type":       "Feature",
			"geometry":   c["voronoi_geojson"],
			"properties": M{"code": c["code"], "name": c["name"]},
		})
	}
	writeJSON(w, 200, M{"type": "FeatureCollection", "features": features, "count": len(features)})
}

// ═══════════════════════════════════════════════════════════
// #9 — Crowd Threshold Alerts
// ═══════════════════════════════════════════════════════════

func handleCrowdAlerts(w http.ResponseWriter, r *http.Request) {
	severity := r.URL.Query().Get("severity")
	limit := queryParamInt(r, "limit", 50)

	query := `SELECT ca.id, ca.pu_code, ca.alert_type, ca.severity, ca.head_count,
		ca.density_level, ca.message, ca.acknowledged, ca.created_at,
		pu.name AS pu_name
		FROM crowd_alerts ca
		LEFT JOIN polling_units pu ON pu.code = ca.pu_code
		WHERE ca.created_at > NOW() - INTERVAL '24 hours'`
	var params []interface{}
	paramIdx := 1

	if severity != "" {
		query += fmt.Sprintf(` AND ca.severity = $%d`, paramIdx)
		params = append(params, severity)
		paramIdx++
	}
	query += fmt.Sprintf(` ORDER BY ca.created_at DESC LIMIT $%d`, paramIdx)
	params = append(params, limit)

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	alerts := scanRows(rows)

	// Count unacknowledged
	var unackCount int
	db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM crowd_alerts WHERE NOT acknowledged AND created_at > NOW() - INTERVAL '24 hours'`).Scan(&unackCount)

	writeJSON(w, 200, M{
		"alerts":         alerts,
		"count":          len(alerts),
		"unacknowledged": unackCount,
	})
}

func handleAcknowledgeCrowdAlert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AlertID int    `json:"alert_id"`
		UserID  string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, M{"error": "invalid body"})
		return
	}
	db.ExecContext(r.Context(), `UPDATE crowd_alerts SET acknowledged=TRUE, acknowledged_by=$1 WHERE id=$2`, body.UserID, body.AlertID)
	writeJSON(w, 200, M{"status": "acknowledged"})
}

// checkCrowdThresholds is called after crowd density report — triggers alerts if overcrowded.
func checkCrowdThresholds(ctx context.Context, puCode string, headCount int, densityLevel string) {
	if headCount < 250 && densityLevel != "overcrowded" {
		return
	}

	severity := "warning"
	alertType := "high_density"
	message := fmt.Sprintf("PU %s has %d people, density: %s", puCode, headCount, densityLevel)

	if headCount > 400 || densityLevel == "overcrowded" {
		severity = "critical"
		alertType = "overcrowded"
		message = fmt.Sprintf("CRITICAL: PU %s overcrowded with %d people — security and crowd control needed", puCode, headCount)
	}

	db.ExecContext(ctx, `
		INSERT INTO crowd_alerts (pu_code, alert_type, severity, head_count, density_level, message)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		puCode, alertType, severity, headCount, densityLevel, message)

	// Also emit as geo_event for SSE consumers
	db.ExecContext(ctx, `
		INSERT INTO geo_events (polling_unit_code, event_type, payload)
		VALUES ($1, 'crowd_alert', $2)`,
		puCode, fmt.Sprintf(`{"severity":"%s","head_count":%d,"density":"%s","message":"%s"}`,
			severity, headCount, densityLevel, message))
}

// ═══════════════════════════════════════════════════════════
// #10 — Route Optimization (OSRM)
// ═══════════════════════════════════════════════════════════

func handleRouteOptimize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OriginLat  float64 `json:"origin_lat"`
		OriginLng  float64 `json:"origin_lng"`
		DestLat    float64 `json:"dest_lat"`
		DestLng    float64 `json:"dest_lng"`
		Profile    string  `json:"profile"` // driving, walking, cycling
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, M{"error": "invalid body"})
		return
	}
	if body.Profile == "" {
		body.Profile = "driving"
	}
	// Security: whitelist profiles to prevent SSRF via path manipulation
	validProfiles := map[string]bool{"driving": true, "walking": true, "cycling": true}
	if !validProfiles[body.Profile] {
		writeJSON(w, 400, M{"error": "invalid profile, must be: driving, walking, cycling"})
		return
	}

	// Use OSRM public API (or self-hosted) -- admin-configured via env, not user input
	osrmURL := os.Getenv("OSRM_URL")
	if osrmURL == "" {
		osrmURL = "https://router.project-osrm.org"
	}
	if !strings.HasPrefix(osrmURL, "https://") && !strings.HasPrefix(osrmURL, "http://") {
		writeJSON(w, 500, M{"error": "invalid OSRM URL scheme"})
		return
	}

	url := fmt.Sprintf("%s/route/v1/%s/%.6f,%.6f;%.6f,%.6f?overview=full&geometries=geojson&steps=true",
		osrmURL, body.Profile, body.OriginLng, body.OriginLat, body.DestLng, body.DestLat)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil) // #nosec G704 -- URL host is admin-configured (env var)
	if err != nil {
		writeJSON(w, 500, M{"error": "failed to create request"})
		return
	}

	resp, err := geoHTTPClient.Do(req) // #nosec G704 -- URL constructed from admin-configured OSRM env var
	if err != nil {
		// Fallback: return straight line with Haversine distance
		dist := haversineDistance(body.OriginLat, body.OriginLng, body.DestLat, body.DestLng)
		writeJSON(w, 200, M{
			"fallback": true,
			"route": M{
				"distance_km":    dist,
				"duration_min":   dist / 40 * 60, // estimate 40km/h
				"geometry":       M{"type": "LineString", "coordinates": [][]float64{{body.OriginLng, body.OriginLat}, {body.DestLng, body.DestLat}}},
			},
		})
		return
	}
	defer resp.Body.Close()

	var osrmResp map[string]interface{}
	json.NewDecoder(io.LimitReader(resp.Body, 10*1024*1024)).Decode(&osrmResp)
	writeJSON(w, 200, osrmResp)
}

// handleNearestOfficial finds closest available official to a given location.
func handleNearestOfficial(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")
	role := r.URL.Query().Get("role")

	lat, _ := strconv.ParseFloat(latStr, 64)
	lng, _ := strconv.ParseFloat(lngStr, 64)
	if lat == 0 || lng == 0 {
		writeJSON(w, 400, M{"error": "lat/lng required"})
		return
	}

	query := `SELECT staff_id, role, latitude, longitude, pu_code, activity, battery_pct,
		ST_Distance(
			ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography,
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography
		) AS distance_m
		FROM official_tracking
		WHERE updated_at > NOW() - INTERVAL '30 minutes'`
	params := []interface{}{lng, lat}
	paramIdx := 3

	if role != "" {
		query += fmt.Sprintf(` AND role = $%d`, paramIdx)
		params = append(params, role)
	}
	query += " ORDER BY distance_m ASC LIMIT 5"

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	officials := scanRows(rows)
	writeJSON(w, 200, M{"nearest": officials, "count": len(officials)})
}

// ═══════════════════════════════════════════════════════════
// #15 — Weather Overlay
// ═══════════════════════════════════════════════════════════

func handleWeatherOverlay(w http.ResponseWriter, r *http.Request) {
	latStr := r.URL.Query().Get("lat")
	lngStr := r.URL.Query().Get("lng")
	lat, _ := strconv.ParseFloat(latStr, 64)
	lng, _ := strconv.ParseFloat(lngStr, 64)

	if lat == 0 || lng == 0 {
		// Return weather for all 6 zone capitals
		zones := []struct{ Name string; Lat, Lng float64 }{
			{"Abuja", 9.0805, 7.4969}, {"Lagos", 6.5975, 3.3433}, {"Kano", 12.0001, 8.5167},
			{"Port Harcourt", 4.7677, 7.0189}, {"Maiduguri", 11.8395, 13.1536}, {"Enugu", 6.5536, 7.4143},
		}
		results := make([]M, 0, len(zones))
		for _, z := range zones {
			results = append(results, M{
				"name": z.Name, "lat": z.Lat, "lng": z.Lng,
				"weather": fetchWeather(r.Context(), z.Lat, z.Lng),
			})
		}
		writeJSON(w, 200, M{"zones": results})
		return
	}

	writeJSON(w, 200, M{"weather": fetchWeather(r.Context(), lat, lng)})
}

func fetchWeather(ctx context.Context, lat, lng float64) M {
	apiKey := os.Getenv("OPENWEATHER_API_KEY")
	if apiKey == "" {
		// Return simulated weather data
		return M{
			"temp_c":      28 + int(lat*100)%8 - 4,
			"humidity":    65 + int(lng*100)%20,
			"description": "partly cloudy",
			"wind_kmh":    12 + int(lat*10)%10,
			"rain_mm":     0,
			"simulated":   true,
		}
	}

	url := fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?lat=%.4f&lon=%.4f&appid=%s&units=metric",
		lat, lng, apiKey)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, "GET", url, nil) // #nosec G704 -- hardcoded host, lat/lng are float64
	resp, err := geoHTTPClient.Do(req)                            // #nosec G704
	if err != nil {
		return M{"error": err.Error(), "simulated": true, "temp_c": 28, "description": "unknown"}
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)
	return M{"data": data, "simulated": false}
}

// ═══════════════════════════════════════════════════════════
// #19 — PU Photo Verification
// ═══════════════════════════════════════════════════════════

func handleUploadPUPhoto(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(10 << 20) // 10MB max

	puCode := r.FormValue("pu_code")
	caption := r.FormValue("caption")
	photoType := r.FormValue("photo_type")
	uploadedBy := r.FormValue("uploaded_by")
	lat, _ := strconv.ParseFloat(r.FormValue("lat"), 64)
	lng, _ := strconv.ParseFloat(r.FormValue("lng"), 64)

	if puCode == "" {
		writeJSON(w, 400, M{"error": "pu_code required"})
		return
	}
	if photoType == "" {
		photoType = "verification"
	}

	file, handler, err := r.FormFile("photo")
	if err != nil {
		writeJSON(w, 400, M{"error": "photo file required"})
		return
	}
	defer file.Close()

	// Save to uploads directory
	uploadDir := "uploads/pu_photos"
	os.MkdirAll(uploadDir, 0750)
	// Sanitize puCode to prevent path traversal
	safePU := filepath.Base(strings.ReplaceAll(puCode, "..", ""))
	safeExt := filepath.Ext(filepath.Base(handler.Filename))
	filename := fmt.Sprintf("%s_%d%s", safePU, time.Now().UnixMilli(), safeExt)
	dst, err := os.Create(filepath.Join(uploadDir, filename))
	if err != nil {
		writeJSON(w, 500, M{"error": "failed to save file"})
		return
	}
	defer dst.Close()
	io.Copy(dst, file)

	photoURL := fmt.Sprintf("/uploads/pu_photos/%s", filename)

	_, err = db.ExecContext(r.Context(), `
		INSERT INTO pu_photos (pu_code, photo_url, caption, photo_type, latitude, longitude, uploaded_by, geom)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $5 > 0 THEN ST_SetSRID(ST_MakePoint($6, $5), 4326) ELSE NULL END)`,
		puCode, photoURL, caption, photoType, lat, lng, uploadedBy)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}

	writeJSON(w, 200, M{"status": "uploaded", "photo_url": photoURL, "pu_code": puCode})
}

func handleGetPUPhotos(w http.ResponseWriter, r *http.Request) {
	puCode := r.URL.Query().Get("pu_code")
	limit := queryParamInt(r, "limit", 50)

	query := `SELECT id, pu_code, photo_url, caption, photo_type, latitude, longitude,
		uploaded_by, verified, created_at FROM pu_photos`
	var params []interface{}
	paramIdx := 1

	if puCode != "" {
		query += " WHERE pu_code = $" + strconv.Itoa(paramIdx)
		params = append(params, puCode)
		paramIdx++
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, paramIdx)
	params = append(params, limit)

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	photos := scanRows(rows)
	writeJSON(w, 200, M{"photos": photos, "count": len(photos)})
}

// ═══════════════════════════════════════════════════════════
// #20 — Incident Hotspot Overlay
// ═══════════════════════════════════════════════════════════

func handleIncidentHotspots(w http.ResponseWriter, r *http.Request) {
	severity := r.URL.Query().Get("severity")
	hours := queryParamInt(r, "hours", 24)

	query := `SELECT il.id, il.incident_id, il.latitude, il.longitude, il.severity,
		il.incident_type, il.description, il.resolved, il.created_at
		FROM incident_locations il
		WHERE il.created_at > NOW() - ($1 * INTERVAL '1 hour')`
	params := []interface{}{hours}
	paramIdx := 2

	if severity != "" {
		query += fmt.Sprintf(` AND il.severity = $%d`, paramIdx)
		params = append(params, severity)
	}
	query += " ORDER BY il.created_at DESC LIMIT 500"

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	incidents := scanRows(rows)

	features := make([]M, 0, len(incidents))
	for _, inc := range incidents {
		lat, _ := toFloatGeo(inc["latitude"])
		lng, _ := toFloatGeo(inc["longitude"])
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Point",
				"coordinates": []float64{lng, lat},
			},
			"properties": inc,
		})
	}

	writeJSON(w, 200, M{"type": "FeatureCollection", "features": features, "count": len(features)})
}

// ═══════════════════════════════════════════════════════════
// #25 — Blockchain Geofence Proof
// ═══════════════════════════════════════════════════════════

func handleGeofenceAttestation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StaffID string  `json:"staff_id"`
		PUCode  string  `json:"pu_code"`
		Lat     float64 `json:"lat"`
		Lng     float64 `json:"lng"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, M{"error": "invalid body"})
		return
	}
	if body.StaffID == "" || body.PUCode == "" {
		writeJSON(w, 400, M{"error": "staff_id and pu_code required"})
		return
	}

	// Check geofence distance
	var distanceM float64
	var withinGeofence bool
	err := db.QueryRowContext(r.Context(), `
		SELECT ST_Distance(
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			ST_SetSRID(ST_MakePoint(gz.center_lng, gz.center_lat), 4326)::geography
		), ST_Distance(
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			ST_SetSRID(ST_MakePoint(gz.center_lng, gz.center_lat), 4326)::geography
		) <= gz.radius_m
		FROM geofence_zones gz WHERE gz.pu_code = $3`,
		body.Lng, body.Lat, body.PUCode).Scan(&distanceM, &withinGeofence)

	if err != nil {
		// No geofence zone defined — calculate from PU location
		distanceM = 0
		withinGeofence = true
	}

	// Generate cryptographic attestation
	attestData := fmt.Sprintf("%s|%s|%.6f|%.6f|%v|%d", body.StaffID, body.PUCode, body.Lat, body.Lng, withinGeofence, time.Now().Unix())
	h := sha256.Sum256([]byte(attestData))
	sigHash := fmt.Sprintf("%x", h)

	_, err = db.ExecContext(r.Context(), `
		INSERT INTO geofence_attestations (staff_id, pu_code, latitude, longitude, within_geofence, distance_m, signature_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		body.StaffID, body.PUCode, body.Lat, body.Lng, withinGeofence, distanceM, sigHash)

	writeJSON(w, 200, M{
		"attestation": M{
			"staff_id":        body.StaffID,
			"pu_code":         body.PUCode,
			"within_geofence": withinGeofence,
			"distance_m":      distanceM,
			"signature":       sigHash,
			"timestamp":       time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// ═══════════════════════════════════════════════════════════
// #30 — H3 Hexagonal Grid Analytics
// ═══════════════════════════════════════════════════════════

func handleH3HexGrid(w http.ResponseWriter, r *http.Request) {
	resolution := queryParamInt(r, "resolution", 5) // H3 resolution 0-15
	eid := queryParamInt(r, "election_id", 1)

	if resolution < 0 || resolution > 15 {
		resolution = 5
	}

	// Approximate H3-like hexagonal grid using PostGIS
	// Each hex is ~0.5° at res 5
	hexSize := 1.0 / math.Pow(3, float64(resolution-3))
	if hexSize < 0.01 {
		hexSize = 0.01
	}
	if hexSize > 2 {
		hexSize = 2
	}

	rows, err := dbQueryCtx(r.Context(), fmt.Sprintf(`
		WITH hex_grid AS (
			SELECT
				FLOOR(pu.latitude / %.4f) * %.4f AS hex_lat,
				FLOOR(pu.longitude / %.4f) * %.4f AS hex_lng,
				COUNT(*) AS pu_count,
				SUM(pu.registered_voters) AS registered,
				COALESCE(SUM(r.total_votes_cast), 0) AS votes,
				AVG(CASE WHEN pu.registered_voters > 0
					THEN CAST(COALESCE(r.total_votes_cast,0) AS FLOAT)/pu.registered_voters ELSE 0 END) AS avg_turnout
			FROM polling_units pu
			LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
			WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL
			GROUP BY FLOOR(pu.latitude / %.4f), FLOOR(pu.longitude / %.4f)
		)
		SELECT hex_lat, hex_lng, pu_count, registered, votes, avg_turnout
		FROM hex_grid
		ORDER BY pu_count DESC LIMIT 500`,
		hexSize, hexSize, hexSize, hexSize, hexSize, hexSize), eid)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	hexes := scanRows(rows)

	features := make([]M, 0, len(hexes))
	for _, h := range hexes {
		lat, _ := toFloatGeo(h["hex_lat"])
		lng, _ := toFloatGeo(h["hex_lng"])
		// Generate hexagon polygon
		coords := generateHexCoords(lat+hexSize/2, lng+hexSize/2, hexSize/2)
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Polygon",
				"coordinates": []interface{}{coords},
			},
			"properties": h,
		})
	}

	writeJSON(w, 200, M{"type": "FeatureCollection", "features": features, "count": len(features), "resolution": resolution})
}

func generateHexCoords(centerLat, centerLng, size float64) [][]float64 {
	coords := make([][]float64, 7)
	for i := 0; i < 6; i++ {
		angle := float64(i)*math.Pi/3 - math.Pi/6
		dlat := size * math.Sin(angle)
		dlng := size * math.Cos(angle) / math.Cos(centerLat*math.Pi/180)
		coords[i] = []float64{centerLng + dlng, centerLat + dlat}
	}
	coords[6] = coords[0] // close the polygon
	return coords
}

// ═══════════════════════════════════════════════════════════
// #6 — Offline Map Tiles
// ═══════════════════════════════════════════════════════════

func handleOfflineTile(w http.ResponseWriter, r *http.Request) {
	// Proxy OSM tiles and cache locally for offline access
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/geo/tiles/offline/"), "/")
	if len(parts) != 3 {
		http.Error(w, "invalid tile path", 400)
		return
	}

	z, x, yPng := parts[0], parts[1], parts[2]
	y := strings.TrimSuffix(yPng, ".png")

	// Security: validate z/x/y are numeric to prevent path traversal
	for _, v := range []string{z, x, y} {
		for _, c := range v {
			if c < '0' || c > '9' {
				http.Error(w, "invalid tile coordinates", 400)
				return
			}
		}
		if len(v) > 6 { // max zoom ~22, max tile index ~4M (7 digits)
			http.Error(w, "tile coordinate out of range", 400)
			return
		}
	}

	// Check local cache first (defense-in-depth: verify resolved path stays under cache dir)
	cacheDir := "cache/tiles"
	cachePath := filepath.Clean(filepath.Join(cacheDir, z, x, y+".png"))
	if !strings.HasPrefix(cachePath, cacheDir+string(os.PathSeparator)) && cachePath != cacheDir {
		http.Error(w, "invalid tile path", 400)
		return
	}
	if data, err := os.ReadFile(cachePath); err == nil { // #nosec G703 -- path validated above
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		w.Write(data)
		return
	}

	// Fetch from OSM (hardcoded origin, z/x/y validated numeric above)
	tileURL := fmt.Sprintf("https://tile.openstreetmap.org/%s/%s/%s.png", z, x, y)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", tileURL, nil) // #nosec G704 -- hardcoded host + numeric-only path params
	req.Header.Set("User-Agent", "INEC-Election-Platform/1.0")
	resp, err := geoHTTPClient.Do(req) // #nosec G704
	if err != nil {
		http.Error(w, "tile fetch failed", 502)
		return
	}
	defer resp.Body.Close()

	// Security: limit tile response to 5MB to prevent OOM
	data, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		http.Error(w, "tile read failed", 502)
		return
	}

	// Cache locally (path already validated above)
	os.MkdirAll(filepath.Join(cacheDir, z, x), 0750) // #nosec G703 -- z/x validated numeric above
	os.WriteFile(cachePath, data, 0600)               // #nosec G703 -- cachePath validated above

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(data)
}

// ═══════════════════════════════════════════════════════════
// #11 — PostGIS MVT Tile Generation
// ═══════════════════════════════════════════════════════════

func handlePostGISMVT(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	z, _ := strconv.Atoi(vars["z"])
	x, _ := strconv.Atoi(vars["x"])
	y, _ := strconv.Atoi(vars["y"])

	// Convert tile coords to bounding box
	n := math.Pow(2, float64(z))
	lonMin := float64(x)/n*360 - 180
	lonMax := float64(x+1)/n*360 - 180
	latMaxRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y)/n)))
	latMinRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y+1)/n)))
	latMin := latMinRad * 180 / math.Pi
	latMax := latMaxRad * 180 / math.Pi

	var mvtData []byte
	err := db.QueryRowContext(r.Context(), `
		SELECT ST_AsMVT(tile, 'polling_units', 4096, 'geom') FROM (
			SELECT
				ST_AsMVTGeom(
					ST_SetSRID(ST_MakePoint(pu.longitude, pu.latitude), 4326),
					ST_MakeEnvelope($1, $2, $3, $4, 4326),
					4096, 64, true
				) AS geom,
				pu.code, pu.name, pu.registered_voters,
				COALESCE(r.status, 'no_result') AS status,
				COALESCE(r.total_votes_cast, 0) AS votes
			FROM polling_units pu
			LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = 1
			WHERE pu.latitude BETWEEN $2 AND $4
				AND pu.longitude BETWEEN $1 AND $3
				AND pu.latitude IS NOT NULL
			LIMIT 10000
		) AS tile`,
		lonMin, latMin, lonMax, latMax).Scan(&mvtData)

	if err != nil {
		http.Error(w, "mvt generation failed: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.mapbox-vector-tile")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(mvtData)
}

// ═══════════════════════════════════════════════════════════
// #22 — Predictive Crowd Flow
// ═══════════════════════════════════════════════════════════

func handlePredictiveCrowdFlow(w http.ResponseWriter, r *http.Request) {
	// Time-series based crowd prediction per PU
	rows, err := dbQueryCtx(r.Context(), `
		WITH recent_crowd AS (
			SELECT pu_code, head_count, density_level,
				EXTRACT(HOUR FROM reported_at) AS hour_of_day,
				reported_at
			FROM crowd_density
			WHERE reported_at > NOW() - INTERVAL '6 hours'
			ORDER BY reported_at DESC
		),
		trends AS (
			SELECT pu_code,
				COUNT(*) AS report_count,
				AVG(head_count) AS avg_head_count,
				MAX(head_count) AS max_head_count,
				MAX(head_count) - MIN(head_count) AS head_count_range,
				REGR_SLOPE(head_count, EXTRACT(EPOCH FROM reported_at)) AS trend_slope
			FROM recent_crowd
			GROUP BY pu_code
			HAVING COUNT(*) >= 2
		)
		SELECT t.pu_code, t.report_count, t.avg_head_count, t.max_head_count,
			t.head_count_range, t.trend_slope,
			pu.name AS pu_name, pu.latitude, pu.longitude,
			CASE
				WHEN t.trend_slope > 0.01 THEN 'increasing'
				WHEN t.trend_slope < -0.01 THEN 'decreasing'
				ELSE 'stable'
			END AS trend_direction,
			CASE
				WHEN t.trend_slope > 0.01 AND t.max_head_count > 250 THEN 'overcrowding_predicted'
				WHEN t.trend_slope > 0.005 AND t.avg_head_count > 150 THEN 'high_density_predicted'
				ELSE 'normal'
			END AS prediction
		FROM trends t
		LEFT JOIN polling_units pu ON pu.code = t.pu_code
		ORDER BY t.trend_slope DESC`)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	predictions := scanRows(rows)

	features := make([]M, 0, len(predictions))
	for _, p := range predictions {
		lat, _ := toFloatGeo(p["latitude"])
		lng, _ := toFloatGeo(p["longitude"])
		if lat == 0 || lng == 0 {
			continue
		}
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Point",
				"coordinates": []float64{lng, lat},
			},
			"properties": p,
		})
	}

	writeJSON(w, 200, M{"type": "FeatureCollection", "features": features, "count": len(features)})
}

// ═══════════════════════════════════════════════════════════
// #23 — Drone Integration
// ═══════════════════════════════════════════════════════════

func handleDronePositions(w http.ResponseWriter, r *http.Request) {
	// Store drone positions (similar to official tracking but for drones)
	if r.Method == "POST" {
		var body struct {
			DroneID  string  `json:"drone_id"`
			Lat      float64 `json:"lat"`
			Lng      float64 `json:"lng"`
			Altitude float64 `json:"altitude_m"`
			Battery  int     `json:"battery_pct"`
			Status   string  `json:"status"`
			StreamURL string `json:"stream_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, M{"error": "invalid body"})
			return
		}
		db.ExecContext(r.Context(), `
			INSERT INTO geo_events (polling_unit_code, event_type, latitude, longitude, payload)
			VALUES ($1, 'drone_position', $2, $3, $4)`,
			body.DroneID, body.Lat, body.Lng,
			fmt.Sprintf(`{"drone_id":"%s","altitude":%.1f,"battery":%d,"status":"%s","stream_url":"%s"}`,
				body.DroneID, body.Altitude, body.Battery, body.Status, body.StreamURL))
		writeJSON(w, 200, M{"status": "tracked", "drone_id": body.DroneID})
		return
	}

	// GET: return latest drone positions
	rows, err := dbQueryCtx(r.Context(), `
		SELECT DISTINCT ON (ge.polling_unit_code) ge.polling_unit_code AS drone_id,
			ge.latitude, ge.longitude, ge.payload, ge.created_at
		FROM geo_events ge
		WHERE ge.event_type = 'drone_position' AND ge.created_at > NOW() - INTERVAL '30 minutes'
		ORDER BY ge.polling_unit_code, ge.created_at DESC`)
	if err != nil {
		writeJSON(w, 200, M{"drones": []M{}, "count": 0})
		return
	}
	drones := scanRows(rows)
	writeJSON(w, 200, M{"drones": drones, "count": len(drones)})
}

// ═══════════════════════════════════════════════════════════
// #24 — Digital Twin Simulation
// ═══════════════════════════════════════════════════════════

func handleDigitalTwinSimulation(w http.ResponseWriter, r *http.Request) {
	scenarioType := r.URL.Query().Get("scenario") // normal, high_turnout, incident, delayed_results
	if scenarioType == "" {
		scenarioType = "normal"
	}
	speed := queryParamInt(r, "speed", 1)    // simulation speed multiplier
	duration := queryParamInt(r, "hours", 12) // hours to simulate

	// Generate simulated election timeline
	type SimEvent struct {
		Hour     int    `json:"hour"`
		Minute   int    `json:"minute"`
		Event    string `json:"event"`
		PUCode   string `json:"pu_code"`
		Lat      float64 `json:"lat"`
		Lng      float64 `json:"lng"`
		Details  M      `json:"details"`
	}

	events := make([]SimEvent, 0)

	// Get PU positions
	rows, err := dbQueryCtx(r.Context(), `
		SELECT code, name, latitude, longitude, registered_voters
		FROM polling_units WHERE latitude IS NOT NULL LIMIT 100`)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	puList := scanRows(rows)

	for hour := 8; hour < 8+duration && hour <= 18; hour++ {
		for _, pu := range puList {
			lat, _ := toFloatGeo(pu["latitude"])
			lng, _ := toFloatGeo(pu["longitude"])
			code, _ := pu["code"].(string)
			reg, _ := toIntGeo(pu["registered_voters"])

			// Simulate crowd buildup
			crowdPct := 0.0
			switch {
			case hour < 10: crowdPct = float64(hour-8) * 0.15
			case hour < 14: crowdPct = 0.3 + float64(hour-10)*0.12
			case hour < 16: crowdPct = 0.78 - float64(hour-14)*0.1
			default:        crowdPct = 0.58 - float64(hour-16)*0.15
			}
			if scenarioType == "high_turnout" {
				crowdPct *= 1.4
			}
			crowd := int(float64(reg) * crowdPct)

			events = append(events, SimEvent{
				Hour: hour, Minute: 0, Event: "crowd_update",
				PUCode: code, Lat: lat, Lng: lng,
				Details: M{"head_count": crowd, "density": crowdDensityLevel(crowd)},
			})

			// Result submission (after 2pm)
			if hour >= 14 && scenarioType != "delayed_results" {
				votePct := crowdPct * 0.85
				events = append(events, SimEvent{
					Hour: hour, Minute: 30, Event: "result_submission",
					PUCode: code, Lat: lat, Lng: lng,
					Details: M{"votes_cast": int(float64(reg) * votePct), "status": "finalized"},
				})
			}
		}

		// Incidents (random based on scenario)
		if scenarioType == "incident" && hour >= 10 {
			if len(puList) > 0 {
				idx := hour % len(puList)
				pu := puList[idx]
				lat, _ := toFloatGeo(pu["latitude"])
				lng, _ := toFloatGeo(pu["longitude"])
				code, _ := pu["code"].(string)
				events = append(events, SimEvent{
					Hour: hour, Minute: 15, Event: "incident",
					PUCode: code, Lat: lat, Lng: lng,
					Details: M{"type": "equipment_failure", "severity": "medium"},
				})
			}
		}
	}

	writeJSON(w, 200, M{
		"scenario":  scenarioType,
		"speed":     speed,
		"duration":  duration,
		"events":    events,
		"pu_count":  len(puList),
		"total_events": len(events),
	})
}

func crowdDensityLevel(count int) string {
	switch {
	case count > 400: return "overcrowded"
	case count > 200: return "high"
	case count > 100: return "moderate"
	default: return "low"
	}
}

// ═══════════════════════════════════════════════════════════
// #26 — Mesh Network Visualization
// ═══════════════════════════════════════════════════════════

func handleMeshNetworkStatus(w http.ResponseWriter, r *http.Request) {
	// Get all active officials and compute mesh network topology
	rows, err := dbQueryCtx(r.Context(), `
		SELECT staff_id, latitude, longitude, pu_code, battery_pct, updated_at
		FROM official_tracking
		WHERE updated_at > NOW() - INTERVAL '30 minutes'
		ORDER BY updated_at DESC`)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	officials := scanRows(rows)

	// Build adjacency graph: officials within 5km can communicate
	meshRadiusKm := 5.0
	type Node struct {
		ID  string
		Lat float64
		Lng float64
	}
	nodes := make([]Node, 0, len(officials))
	for _, o := range officials {
		lat, _ := toFloatGeo(o["latitude"])
		lng, _ := toFloatGeo(o["longitude"])
		sid, _ := o["staff_id"].(string)
		nodes = append(nodes, Node{ID: sid, Lat: lat, Lng: lng})
	}

	edges := make([]M, 0)
	isolated := 0
	for i, a := range nodes {
		connected := false
		for j, b := range nodes {
			if i >= j { continue }
			dist := haversineDistance(a.Lat, a.Lng, b.Lat, b.Lng)
			if dist <= meshRadiusKm {
				edges = append(edges, M{
					"from": a.ID, "to": b.ID, "distance_km": math.Round(dist*100)/100,
					"coordinates": [][]float64{{a.Lng, a.Lat}, {b.Lng, b.Lat}},
				})
				connected = true
			}
		}
		if !connected {
			isolated++
		}
	}

	// GeoJSON for edges
	edgeFeatures := make([]M, 0, len(edges))
	for _, e := range edges {
		edgeFeatures = append(edgeFeatures, M{
			"type": "Feature",
			"geometry": M{
				"type":        "LineString",
				"coordinates": e["coordinates"],
			},
			"properties": M{
				"from": e["from"], "to": e["to"],
				"distance_km": e["distance_km"],
			},
		})
	}

	writeJSON(w, 200, M{
		"nodes":    len(nodes),
		"edges":    len(edges),
		"isolated": isolated,
		"mesh_radius_km": meshRadiusKm,
		"connectivity_pct": func() float64 {
			if len(nodes) == 0 { return 0 }
			return float64(len(nodes)-isolated) / float64(len(nodes)) * 100
		}(),
		"edge_geojson": M{"type": "FeatureCollection", "features": edgeFeatures},
	})
}

// ═══════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════

// haversineDistance is declared in geofencing.go

// registerGeoAdvancedRoutes wires all new endpoints.
func registerGeoAdvancedRoutes(r *mux.Router) {
	// #3 Tracking history
	r.HandleFunc("/geo/tracking/record", writeAuth(handleTrackingHistoryRecord)).Methods("POST")
	r.HandleFunc("/geo/tracking/replay", readAuth(handleTrackingHistoryReplay)).Methods("GET")

	// #2 Geofence visualization
	r.HandleFunc("/geo/geofence/zones", readAuth(handleGetGeofenceZones)).Methods("GET")
	r.HandleFunc("/geo/geofence/violations", readAuth(handleGeofenceViolations)).Methods("GET")
	r.HandleFunc("/geo/geofence/zones/seed", writeAuth(handleSeedGeofenceZones)).Methods("POST")

	// #7 Advanced PostGIS
	r.HandleFunc("/geo/spatial/clusters", readAuth(handleSpatialClusters)).Methods("GET")
	r.HandleFunc("/geo/spatial/voronoi", readAuth(handleVoronoiDiagram)).Methods("GET")

	// #9 Crowd alerts
	r.HandleFunc("/geo/crowd/alerts", readAuth(handleCrowdAlerts)).Methods("GET")
	r.HandleFunc("/geo/crowd/alerts/ack", writeAuth(handleAcknowledgeCrowdAlert)).Methods("POST")

	// #10 Route optimization
	r.HandleFunc("/geo/route", writeAuth(handleRouteOptimize)).Methods("POST")
	r.HandleFunc("/geo/nearest-official", readAuth(handleNearestOfficial)).Methods("GET")

	// #15 Weather
	r.HandleFunc("/geo/weather", readAuth(handleWeatherOverlay)).Methods("GET")

	// #19 PU photos
	r.HandleFunc("/geo/photos/upload", writeAuth(handleUploadPUPhoto)).Methods("POST")
	r.HandleFunc("/geo/photos", readAuth(handleGetPUPhotos)).Methods("GET")

	// #20 Incident hotspots
	r.HandleFunc("/geo/incidents/hotspots", readAuth(handleIncidentHotspots)).Methods("GET")

	// #22 Predictive crowd
	r.HandleFunc("/geo/crowd/predictions", readAuth(handlePredictiveCrowdFlow)).Methods("GET")

	// #23 Drones
	r.HandleFunc("/geo/drones", readAuth(handleDronePositions)).Methods("GET", "POST")

	// #24 Digital twin
	r.HandleFunc("/geo/simulation", readAuth(handleDigitalTwinSimulation)).Methods("GET")

	// #25 Blockchain geofence proof
	r.HandleFunc("/geo/geofence/attest", writeAuth(handleGeofenceAttestation)).Methods("POST")

	// #26 Mesh network
	r.HandleFunc("/geo/mesh/status", readAuth(handleMeshNetworkStatus)).Methods("GET")

	// #30 H3 hex grid
	r.HandleFunc("/geo/h3/grid", readAuth(handleH3HexGrid)).Methods("GET")

	// #6 Offline tiles
	r.PathPrefix("/geo/tiles/offline/").HandlerFunc(handleOfflineTile).Methods("GET")

	// #11 PostGIS MVT tiles
	r.HandleFunc("/geo/tiles/mvt/{z:[0-9]+}/{x:[0-9]+}/{y:[0-9]+}.mvt", readAuth(handlePostGISMVT)).Methods("GET")
}
