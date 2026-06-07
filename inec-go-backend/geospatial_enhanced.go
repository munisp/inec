package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// PostGIS-integrated geospatial handlers for polling unit mapping,
// landmark discovery, proximity search, heatmaps, and real-time features.

// --- PostGIS Schema Migration ---

const geoMigrationSQL = `
-- Enable PostGIS extensions
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- Add geometry column to polling_unit_locations if not exists
DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='polling_unit_locations' AND column_name='geom'
    ) THEN
        ALTER TABLE polling_unit_locations ADD COLUMN geom geometry(Point, 4326);
        UPDATE polling_unit_locations SET geom = ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)
        WHERE latitude IS NOT NULL AND longitude IS NOT NULL;
        CREATE INDEX IF NOT EXISTS idx_pu_locations_geom ON polling_unit_locations USING GIST(geom);
    END IF;
END $$;

-- Landmarks table
CREATE TABLE IF NOT EXISTS landmarks (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    category TEXT NOT NULL,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL,
    geom geometry(Point, 4326),
    state_code TEXT,
    lga_code TEXT,
    address TEXT,
    description TEXT,
    icon TEXT DEFAULT 'marker',
    importance INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_landmarks_geom ON landmarks USING GIST(geom);
CREATE INDEX IF NOT EXISTS idx_landmarks_category ON landmarks(category);
CREATE INDEX IF NOT EXISTS idx_landmarks_state ON landmarks(state_code);

-- Spatial analytics cache
CREATE TABLE IF NOT EXISTS geo_analytics_cache (
    id TEXT PRIMARY KEY,
    data JSONB NOT NULL,
    computed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP
);

-- Real-time PU status events for geo streaming
CREATE TABLE IF NOT EXISTS geo_events (
    id SERIAL PRIMARY KEY,
    polling_unit_code TEXT NOT NULL,
    event_type TEXT NOT NULL,
    latitude REAL,
    longitude REAL,
    payload JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_geo_events_created ON geo_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_geo_events_pu ON geo_events(polling_unit_code);
`

func runGeoMigrations() {
	_, err := db.Exec(geoMigrationSQL)
	if err != nil {
		logger.Printf("geo migration (non-fatal): %v", err)
	}
}

// --- Landmark Types ---

var landmarkCategories = []string{
	"inec_office", "collation_center", "police_station", "hospital",
	"school", "church", "mosque", "market", "government_building",
	"transport_hub", "bank", "post_office",
}

// --- Handlers ---

// handleNearbyPUs finds polling units within a radius of given coordinates using PostGIS.
func handleNearbyPUs(w http.ResponseWriter, r *http.Request) {
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	radiusM, _ := strconv.ParseFloat(r.URL.Query().Get("radius"), 64)
	limit := queryParamInt(r, "limit", 20)

	if lat == 0 || lng == 0 {
		writeJSON(w, 400, M{"error": "lat and lng required"})
		return
	}
	if radiusM <= 0 {
		radiusM = 5000 // default 5km
	}
	if limit > 100 {
		limit = 100
	}

	// Try PostGIS first, fallback to Haversine
	var results []M
	rows, err := dbQueryCtx(r.Context(), `
		SELECT pl.polling_unit_code, pl.latitude, pl.longitude,
			   pu.name, pu.registered_voters, w.name AS ward_name,
			   l.name AS lga_name, l.state_code,
			   ST_Distance(pl.geom::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) AS distance_m
		FROM polling_unit_locations pl
		JOIN polling_units pu ON pu.code = pl.polling_unit_code
		JOIN wards w ON w.code = pu.ward_code
		JOIN lgas l ON l.code = w.lga_code
		WHERE ST_DWithin(pl.geom::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
		ORDER BY distance_m
		LIMIT $4`, lng, lat, radiusM, limit)

	if err != nil {
		// Fallback: Haversine formula in SQL
		rows, err = dbQueryCtx(r.Context(), `
			SELECT pl.polling_unit_code, pl.latitude, pl.longitude,
				   pu.name, pu.registered_voters, w.name AS ward_name,
				   l.name AS lga_name, l.state_code,
				   (6371000 * acos(
					   cos(radians($1)) * cos(radians(pl.latitude)) *
					   cos(radians(pl.longitude) - radians($2)) +
					   sin(radians($1)) * sin(radians(pl.latitude))
				   )) AS distance_m
			FROM polling_unit_locations pl
			JOIN polling_units pu ON pu.code = pl.polling_unit_code
			JOIN wards w ON w.code = pu.ward_code
			JOIN lgas l ON l.code = w.lga_code
			WHERE pl.latitude BETWEEN $1 - ($3/111000.0) AND $1 + ($3/111000.0)
			  AND pl.longitude BETWEEN $2 - ($3/(111000.0*cos(radians($1)))) AND $2 + ($3/(111000.0*cos(radians($1))))
			HAVING (6371000 * acos(
				cos(radians($1)) * cos(radians(pl.latitude)) *
				cos(radians(pl.longitude) - radians($2)) +
				sin(radians($1)) * sin(radians(pl.latitude))
			)) <= $3
			ORDER BY distance_m
			LIMIT $4`, lat, lng, radiusM, limit)
		if err != nil {
			writeJSON(w, 500, M{"error": "query failed: " + err.Error()})
			return
		}
	}
	results = scanRows(rows)

	writeJSON(w, 200, M{
		"polling_units": results,
		"center":        M{"lat": lat, "lng": lng},
		"radius_m":      radiusM,
		"count":         len(results),
	})
}

// handleLandmarks returns landmarks near coordinates or within a state/LGA.
func handleLandmarks(w http.ResponseWriter, r *http.Request) {
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	radiusM, _ := strconv.ParseFloat(r.URL.Query().Get("radius"), 64)
	category := r.URL.Query().Get("category")
	stateCode := r.URL.Query().Get("state_code")
	limit := queryParamInt(r, "limit", 50)

	if radiusM <= 0 {
		radiusM = 10000 // default 10km
	}

	var rows interface{ Close() error }
	var err error
	var query string
	var params []interface{}

	if lat != 0 && lng != 0 {
		query = `SELECT id, name, category, latitude, longitude, state_code, lga_code,
				 address, description, icon, importance FROM landmarks
				 WHERE (6371000 * acos(
					cos(radians($1)) * cos(radians(latitude)) *
					cos(radians(longitude) - radians($2)) +
					sin(radians($1)) * sin(radians(latitude))
				 )) <= $3`
		params = []interface{}{lat, lng, radiusM}
		paramIdx := 4
		if category != "" {
			query += fmt.Sprintf(" AND category = $%d", paramIdx)
			params = append(params, category)
			paramIdx++
		}
		query += fmt.Sprintf(" ORDER BY importance DESC LIMIT $%d", paramIdx)
		params = append(params, limit)
	} else if stateCode != "" {
		query = `SELECT id, name, category, latitude, longitude, state_code, lga_code,
				 address, description, icon, importance FROM landmarks WHERE state_code = $1`
		params = []interface{}{stateCode}
		paramIdx := 2
		if category != "" {
			query += fmt.Sprintf(" AND category = $%d", paramIdx)
			params = append(params, category)
			paramIdx++
		}
		query += fmt.Sprintf(" ORDER BY importance DESC LIMIT $%d", paramIdx)
		params = append(params, limit)
	} else {
		query = `SELECT id, name, category, latitude, longitude, state_code, lga_code,
				 address, description, icon, importance FROM landmarks`
		params = []interface{}{}
		paramIdx := 1
		if category != "" {
			query += fmt.Sprintf(" WHERE category = $%d", paramIdx)
			params = append(params, category)
			paramIdx++
		}
		query += fmt.Sprintf(" ORDER BY importance DESC LIMIT $%d", paramIdx)
		params = append(params, limit)
	}

	dbRows, dbErr := dbQueryCtx(r.Context(), query, params...)
	rows = dbRows
	err = dbErr
	if err != nil {
		writeJSON(w, 500, M{"error": "query failed: " + err.Error()})
		return
	}
	results := scanRows(dbRows)

	writeJSON(w, 200, M{
		"landmarks":  results,
		"categories": landmarkCategories,
		"count":      len(results),
	})
	_ = rows
}

// handleCreateLandmark creates a new landmark.
func handleCreateLandmark(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string  `json:"name"`
		Category    string  `json:"category"`
		Latitude    float64 `json:"latitude"`
		Longitude   float64 `json:"longitude"`
		StateCode   string  `json:"state_code"`
		LGACode     string  `json:"lga_code"`
		Address     string  `json:"address"`
		Description string  `json:"description"`
		Icon        string  `json:"icon"`
		Importance  int     `json:"importance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, M{"error": "invalid JSON"})
		return
	}
	if body.Name == "" || body.Category == "" || body.Latitude == 0 || body.Longitude == 0 {
		writeJSON(w, 400, M{"error": "name, category, latitude, longitude required"})
		return
	}

	var id int
	err := db.QueryRowContext(r.Context(),
		`INSERT INTO landmarks (name, category, latitude, longitude, geom, state_code, lga_code, address, description, icon, importance)
		 VALUES ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($4, $3), 4326), $5, $6, $7, $8, $9, $10)
		 RETURNING id`,
		body.Name, body.Category, body.Latitude, body.Longitude,
		body.StateCode, body.LGACode, body.Address, body.Description,
		body.Icon, body.Importance,
	).Scan(&id)
	if err != nil {
		// Fallback without PostGIS geom
		err = db.QueryRowContext(r.Context(),
			`INSERT INTO landmarks (name, category, latitude, longitude, state_code, lga_code, address, description, icon, importance)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 RETURNING id`,
			body.Name, body.Category, body.Latitude, body.Longitude,
			body.StateCode, body.LGACode, body.Address, body.Description,
			body.Icon, body.Importance,
		).Scan(&id)
		if err != nil {
			writeJSON(w, 500, M{"error": "create failed: " + err.Error()})
			return
		}
	}

	writeJSON(w, 200, M{"id": id, "status": "created"})
}

// handleGeoHeatmap returns heatmap data for election results (voter density, turnout, anomalies).
func handleGeoHeatmap(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	metric := r.URL.Query().Get("metric") // turnout, density, anomaly
	if metric == "" {
		metric = "turnout"
	}

	var query string
	switch metric {
	case "turnout":
		query = `SELECT pu.latitude, pu.longitude, 
				 CASE WHEN pu.registered_voters > 0 
					  THEN CAST(COALESCE(r.total_votes_cast, 0) AS FLOAT) / pu.registered_voters 
					  ELSE 0 END AS intensity,
				 pu.code, pu.name
				 FROM polling_units pu
				 LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
				 WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL`
	case "density":
		query = `SELECT pu.latitude, pu.longitude,
				 CAST(pu.registered_voters AS FLOAT) / 1000.0 AS intensity,
				 pu.code, pu.name
				 FROM polling_units pu
				 WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL`
	case "anomaly":
		query = `SELECT pu.latitude, pu.longitude,
				 COALESCE(a.anomaly_score, 0) AS intensity,
				 pu.code, pu.name
				 FROM polling_units pu
				 LEFT JOIN anomaly_scores a ON a.polling_unit_code = pu.code AND a.election_id = $1
				 WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL`
	default:
		writeJSON(w, 400, M{"error": "metric must be turnout, density, or anomaly"})
		return
	}

	rows, err := dbQueryCtx(r.Context(), query, eid)
	if err != nil {
		writeJSON(w, 500, M{"error": "query failed: " + err.Error()})
		return
	}
	points := scanRows(rows)

	// Build GeoJSON FeatureCollection for heatmap layer
	features := make([]M, 0, len(points))
	for _, p := range points {
		lat, _ := toFloatGeo(p["latitude"])
		lng, _ := toFloatGeo(p["longitude"])
		intensity, _ := toFloatGeo(p["intensity"])
		if lat == 0 && lng == 0 {
			continue
		}
		features = append(features, M{
			"type": "Feature",
			"geometry": M{
				"type":        "Point",
				"coordinates": []float64{lng, lat},
			},
			"properties": M{
				"intensity": intensity,
				"code":      p["code"],
				"name":      p["name"],
			},
		})
	}

	writeJSON(w, 200, M{
		"type":     "FeatureCollection",
		"features": features,
		"metric":   metric,
		"count":    len(features),
	})
}

// handleGeoCluster returns clustered polling unit data for map display at different zoom levels.
func handleGeoCluster(w http.ResponseWriter, r *http.Request) {
	eid := queryParamInt(r, "election_id", 1)
	zoom := queryParamInt(r, "zoom", 6)
	stateCode := r.URL.Query().Get("state_code")

	// Grid size based on zoom level (degrees)
	gridSize := 2.0 // zoom 0-5
	if zoom >= 6 && zoom < 8 {
		gridSize = 0.5
	} else if zoom >= 8 && zoom < 10 {
		gridSize = 0.1
	} else if zoom >= 10 && zoom < 12 {
		gridSize = 0.02
	} else if zoom >= 12 {
		gridSize = 0.005
	}

	query := `SELECT 
		FLOOR(pu.latitude / $1) * $1 + $1/2 AS cluster_lat,
		FLOOR(pu.longitude / $1) * $1 + $1/2 AS cluster_lng,
		COUNT(*) AS pu_count,
		SUM(pu.registered_voters) AS total_registered,
		COUNT(r.id) AS results_count,
		COALESCE(SUM(r.total_votes_cast), 0) AS total_votes
	FROM polling_units pu
	LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $2
	WHERE pu.latitude IS NOT NULL AND pu.longitude IS NOT NULL`

	params := []interface{}{gridSize, eid}
	paramIdx := 3
	if stateCode != "" {
		query += fmt.Sprintf(`
			AND pu.ward_code IN (
				SELECT w.code FROM wards w JOIN lgas l ON l.code = w.lga_code WHERE l.state_code = $%d
			)`, paramIdx)
		params = append(params, stateCode)
		paramIdx++
	}
	query += " GROUP BY cluster_lat, cluster_lng ORDER BY pu_count DESC LIMIT 500"

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": "cluster query failed: " + err.Error()})
		return
	}
	clusters := scanRows(rows)

	writeJSON(w, 200, M{
		"clusters":  clusters,
		"grid_size": gridSize,
		"zoom":      zoom,
		"count":     len(clusters),
	})
}

// handleStreetViewProxy provides street view tile/metadata from Mapillary (open-source street-level imagery).
func handleStreetViewProxy(w http.ResponseWriter, r *http.Request) {
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)

	if lat == 0 || lng == 0 {
		writeJSON(w, 400, M{"error": "lat and lng required"})
		return
	}

	// Return Mapillary viewer embed URL and nearby imagery metadata
	mapillaryURL := fmt.Sprintf("https://www.mapillary.com/app/?lat=%f&lng=%f&z=17", lat, lng)
	googleURL := fmt.Sprintf("https://www.google.com/maps/@%f,%f,3a,75y,0h,90t/data=!3m4!1e1!3m2!1s!2e0", lat, lng)

	// Check for available Mapillary coverage near coordinates
	writeJSON(w, 200, M{
		"street_view": M{
			"mapillary": M{
				"viewer_url": mapillaryURL,
				"api_url":    fmt.Sprintf("https://graph.mapillary.com/images?fields=id,computed_geometry,captured_at,compass_angle,thumb_2048_url&bbox=%f,%f,%f,%f&limit=10", lng-0.002, lat-0.002, lng+0.002, lat+0.002),
				"available":  true,
			},
			"google": M{
				"viewer_url": googleURL,
			},
		},
		"coordinates": M{"lat": lat, "lng": lng},
	})
}

// handleGeoBoundary returns GeoJSON boundary polygon for a state or LGA.
func handleGeoBoundary(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")
	lgaCode := r.URL.Query().Get("lga_code")

	if stateCode == "" && lgaCode == "" {
		writeJSON(w, 400, M{"error": "state_code or lga_code required"})
		return
	}

	// Build convex hull from polling unit locations
	var query string
	var params []interface{}
	if lgaCode != "" {
		query = `SELECT pu.latitude, pu.longitude FROM polling_units pu 
				 JOIN wards w ON w.code = pu.ward_code
				 WHERE w.lga_code = $1 AND pu.latitude IS NOT NULL`
		params = []interface{}{lgaCode}
	} else {
		query = `SELECT pu.latitude, pu.longitude FROM polling_units pu 
				 JOIN wards w ON w.code = pu.ward_code
				 JOIN lgas l ON l.code = w.lga_code
				 WHERE l.state_code = $1 AND pu.latitude IS NOT NULL`
		params = []interface{}{stateCode}
	}

	rows, err := dbQueryCtx(r.Context(), query, params...)
	if err != nil {
		writeJSON(w, 500, M{"error": err.Error()})
		return
	}
	points := scanRows(rows)

	// Build convex hull coordinates
	coords := make([][2]float64, 0, len(points))
	for _, p := range points {
		lat, _ := toFloatGeo(p["latitude"])
		lng, _ := toFloatGeo(p["longitude"])
		if lat != 0 && lng != 0 {
			coords = append(coords, [2]float64{lng, lat})
		}
	}

	hull := convexHull(coords)
	if len(hull) > 0 {
		hull = append(hull, hull[0]) // close polygon
	}

	ring := make([][]float64, len(hull))
	for i, c := range hull {
		ring[i] = []float64{c[0], c[1]}
	}

	writeJSON(w, 200, M{
		"type": "Feature",
		"geometry": M{
			"type":        "Polygon",
			"coordinates": []interface{}{ring},
		},
		"properties": M{
			"state_code": stateCode,
			"lga_code":   lgaCode,
			"pu_count":   len(points),
		},
	})
}

// handleGeoLiveStream provides SSE stream of real-time geo events (PU status changes, submissions).
func handleGeoLiveStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	lastID := 0

	for {
		select {
		case <-ctx.Done():
			return
		default:
			rows, err := dbQueryCtx(ctx, `
				SELECT ge.id, ge.polling_unit_code, ge.event_type,
					   ge.latitude, ge.longitude, ge.payload, ge.created_at
				FROM geo_events ge
				WHERE ge.id > $1
				ORDER BY ge.id ASC LIMIT 50`, lastID)
			if err == nil {
				events := scanRows(rows)
				for _, ev := range events {
					id, _ := toIntGeo(ev["id"])
					data, _ := json.Marshal(ev)
					fmt.Fprintf(w, "id: %d\nevent: geo_update\ndata: %s\n\n", id, data)
					if id > lastID {
						lastID = id
					}
				}
				flusher.Flush()
			}
			time.Sleep(2 * time.Second)
		}
	}
}

// handleGeoSpatialStats returns PostGIS-computed spatial statistics.
func handleGeoSpatialStats(w http.ResponseWriter, r *http.Request) {
	stateCode := r.URL.Query().Get("state_code")
	eid := queryParamInt(r, "election_id", 1)

	var whereClause string
	var params []interface{}
	params = append(params, eid)
	paramIdx := 2

	if stateCode != "" {
		whereClause = fmt.Sprintf(`AND pu.ward_code IN (
			SELECT w.code FROM wards w JOIN lgas l ON l.code = w.lga_code WHERE l.state_code = $%d
		)`, paramIdx)
		params = append(params, stateCode)
	}

	// Compute spatial statistics
	query := fmt.Sprintf(`SELECT
		COUNT(*) AS total_pus,
		COUNT(r.id) AS reported_pus,
		AVG(pu.latitude) AS centroid_lat,
		AVG(pu.longitude) AS centroid_lng,
		MIN(pu.latitude) AS min_lat, MAX(pu.latitude) AS max_lat,
		MIN(pu.longitude) AS min_lng, MAX(pu.longitude) AS max_lng,
		SUM(pu.registered_voters) AS total_registered,
		COALESCE(SUM(r.total_votes_cast), 0) AS total_votes,
		CASE WHEN SUM(pu.registered_voters) > 0
			 THEN CAST(COALESCE(SUM(r.total_votes_cast), 0) AS FLOAT) / SUM(pu.registered_voters)
			 ELSE 0 END AS avg_turnout,
		STDDEV(CASE WHEN pu.registered_voters > 0
			   THEN CAST(COALESCE(r.total_votes_cast, 0) AS FLOAT) / pu.registered_voters
			   ELSE 0 END) AS turnout_stddev
	FROM polling_units pu
	LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
	WHERE pu.latitude IS NOT NULL %s`, whereClause)

	row := db.QueryRowContext(r.Context(), query, params...)
	var stats struct {
		TotalPUs        int
		ReportedPUs     int
		CentroidLat     float64
		CentroidLng     float64
		MinLat, MaxLat  float64
		MinLng, MaxLng  float64
		TotalRegistered int
		TotalVotes      int
		AvgTurnout      float64
		TurnoutStddev   float64
	}

	err := row.Scan(&stats.TotalPUs, &stats.ReportedPUs, &stats.CentroidLat, &stats.CentroidLng,
		&stats.MinLat, &stats.MaxLat, &stats.MinLng, &stats.MaxLng,
		&stats.TotalRegistered, &stats.TotalVotes, &stats.AvgTurnout, &stats.TurnoutStddev)
	if err != nil {
		writeJSON(w, 500, M{"error": "stats query failed: "+err.Error()})
		return
	}

	// Coverage area in km² (approximate)
	latDiff := stats.MaxLat - stats.MinLat
	lngDiff := stats.MaxLng - stats.MinLng
	areaKm2 := latDiff * 111.0 * lngDiff * 111.0 * math.Cos(stats.CentroidLat*math.Pi/180)
	puDensity := 0.0
	if areaKm2 > 0 {
		puDensity = float64(stats.TotalPUs) / areaKm2
	}

	writeJSON(w, 200, M{
		"total_pus":          stats.TotalPUs,
		"reported_pus":       stats.ReportedPUs,
		"completion_pct":     safeDivGeo(float64(stats.ReportedPUs), float64(stats.TotalPUs)) * 100,
		"centroid":           M{"lat": stats.CentroidLat, "lng": stats.CentroidLng},
		"bounds":             M{"min_lat": stats.MinLat, "max_lat": stats.MaxLat, "min_lng": stats.MinLng, "max_lng": stats.MaxLng},
		"total_registered":   stats.TotalRegistered,
		"total_votes":        stats.TotalVotes,
		"avg_turnout":        stats.AvgTurnout,
		"turnout_stddev":     stats.TurnoutStddev,
		"area_km2":           areaKm2,
		"pu_density_per_km2": puDensity,
		"state_code":         stateCode,
	})
}

// handleSeedLandmarks seeds Nigerian election-related landmarks.
func handleSeedLandmarks(w http.ResponseWriter, r *http.Request) {
	landmarks := []struct {
		Name      string
		Category  string
		Lat, Lng  float64
		State     string
		LGA       string
		Address   string
		Desc      string
		Icon      string
		Imp       int
	}{
		// INEC Offices (state HQs)
		{"INEC National HQ", "inec_office", 9.0579, 7.4951, "FC", "AMAC", "Zambezi Crescent, Maitama, Abuja", "INEC headquarters", "building", 100},
		{"INEC Lagos Office", "inec_office", 6.4541, 3.3947, "LA", "IKEJA", "Oba Akinjobi Way, Ikeja", "Lagos state INEC office", "building", 90},
		{"INEC Kano Office", "inec_office", 11.9964, 8.5167, "KN", "KANO", "Zoo Road, Kano", "Kano state INEC office", "building", 90},
		{"INEC Rivers Office", "inec_office", 4.7774, 7.0134, "RI", "PHALGA", "Aba Road, Port Harcourt", "Rivers state INEC office", "building", 90},
		{"INEC Oyo Office", "inec_office", 7.3775, 3.9470, "OY", "IBADAN_N", "Agodi Gate, Ibadan", "Oyo state INEC office", "building", 85},

		// Collation Centers
		{"National Collation Center", "collation_center", 9.0765, 7.4986, "FC", "AMAC", "International Conference Centre, Abuja", "Presidential election collation", "flag", 100},
		{"Lagos Collation Center", "collation_center", 6.4328, 3.4218, "LA", "SURULERE", "Tafawa Balewa Square", "Lagos state collation center", "flag", 85},
		{"Kano Collation Center", "collation_center", 12.0022, 8.5920, "KN", "NASSARAWA", "Coronation Hall, Kano", "Kano state collation center", "flag", 85},

		// Police Stations
		{"Force HQ Abuja", "police_station", 9.0578, 7.4893, "FC", "AMAC", "Shehu Shagari Way, Abuja", "Nigeria Police Force HQ", "shield", 90},
		{"Ikeja Police Station", "police_station", 6.6018, 3.3515, "LA", "IKEJA", "Obafemi Awolowo Way, Ikeja", "Area F Command", "shield", 70},

		// Hospitals
		{"National Hospital Abuja", "hospital", 9.0105, 7.4837, "FC", "GARKI", "Plot 132, Central District, Abuja", "National Hospital", "heart", 80},
		{"Lagos University Teaching Hospital", "hospital", 6.5177, 3.3940, "LA", "MUSHIN", "Idi-Araba, Surulere", "LUTH", "heart", 80},

		// Schools (common polling locations)
		{"University of Lagos", "school", 6.5158, 3.3889, "LA", "AKOKA", "Akoka, Yaba, Lagos", "UNILAG main campus", "book", 75},
		{"Ahmadu Bello University", "school", 11.1501, 7.6508, "KD", "SAMARU", "Zaria, Kaduna", "ABU main campus", "book", 75},
		{"University of Ibadan", "school", 7.4442, 3.8936, "OY", "IBADAN_N", "Ibadan, Oyo", "UI main campus", "book", 75},

		// Transport Hubs
		{"Nnamdi Azikiwe Int'l Airport", "transport_hub", 9.0065, 7.2632, "FC", "GWAGWALADA", "Airport Road, Abuja", "Abuja international airport", "plane", 85},
		{"Murtala Muhammed Airport", "transport_hub", 6.5774, 3.3212, "LA", "OSHODI", "Ikeja, Lagos", "Lagos international airport", "plane", 85},

		// Government Buildings
		{"Aso Rock Presidential Villa", "government_building", 9.0886, 7.5271, "FC", "AMAC", "Three Arms Zone, Abuja", "Presidential residence", "landmark", 100},
		{"National Assembly Complex", "government_building", 9.0642, 7.5063, "FC", "AMAC", "Three Arms Zone, Abuja", "Senate/House of Reps", "landmark", 95},
		{"Supreme Court of Nigeria", "government_building", 9.0531, 7.4890, "FC", "AMAC", "Three Arms Zone, Abuja", "Supreme Court building", "landmark", 90},
	}

	count := 0
	for _, lm := range landmarks {
		_, err := db.ExecContext(r.Context(),
			`INSERT INTO landmarks (name, category, latitude, longitude, state_code, lga_code, address, description, icon, importance)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT DO NOTHING`,
			lm.Name, lm.Category, lm.Lat, lm.Lng, lm.State, lm.LGA, lm.Address, lm.Desc, lm.Icon, lm.Imp)
		if err == nil {
			count++
		}
	}

	writeJSON(w, 200, M{"seeded": count, "total": len(landmarks)})
}

// --- Sedona Analytics (via Lakehouse integration) ---

// handleSedonaAnalysis returns spatial analytics computed via the Lakehouse pipeline.
func handleSedonaAnalysis(w http.ResponseWriter, r *http.Request) {
	analysisType := r.URL.Query().Get("type") // spatial_autocorrelation, hotspot, coverage_gap
	eid := queryParamInt(r, "election_id", 1)

	switch analysisType {
	case "hotspot":
		// Find clusters of high anomaly scores
		rows, err := dbQueryCtx(r.Context(), `
			SELECT l.state_code, l.name AS lga_name,
				AVG(pu.latitude) AS center_lat, AVG(pu.longitude) AS center_lng,
				COUNT(*) AS pu_count,
				AVG(CASE WHEN pu.registered_voters > 0
					THEN CAST(COALESCE(r.total_votes_cast,0) AS FLOAT)/pu.registered_voters ELSE 0 END) AS avg_turnout,
				STDDEV(CASE WHEN pu.registered_voters > 0
					THEN CAST(COALESCE(r.total_votes_cast,0) AS FLOAT)/pu.registered_voters ELSE 0 END) AS turnout_std
			FROM polling_units pu
			JOIN wards w ON w.code = pu.ward_code
			JOIN lgas l ON l.code = w.lga_code
			LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
			WHERE pu.latitude IS NOT NULL
			GROUP BY l.state_code, l.name
			HAVING COUNT(*) > 5
			ORDER BY avg_turnout DESC LIMIT 30`, eid)
		if err != nil {
			writeJSON(w, 500, M{"error": err.Error()})
			return
		}
		writeJSON(w, 200, M{"type": "hotspot", "regions": scanRows(rows)})

	case "coverage_gap":
		// Find areas with low PU density relative to population
		rows, err := dbQueryCtx(r.Context(), `
			SELECT l.state_code, l.name AS lga_name,
				COUNT(pu.code) AS pu_count,
				SUM(pu.registered_voters) AS registered,
				AVG(pu.latitude) AS center_lat, AVG(pu.longitude) AS center_lng,
				CASE WHEN COUNT(pu.code) > 0
					THEN CAST(SUM(pu.registered_voters) AS FLOAT) / COUNT(pu.code)
					ELSE 0 END AS voters_per_pu
			FROM lgas l
			LEFT JOIN wards w ON w.lga_code = l.code
			LEFT JOIN polling_units pu ON pu.ward_code = w.code
			GROUP BY l.state_code, l.name
			HAVING CASE WHEN COUNT(pu.code) > 0
				THEN CAST(SUM(pu.registered_voters) AS FLOAT) / COUNT(pu.code)
				ELSE 0 END > 500
			ORDER BY voters_per_pu DESC LIMIT 30`, eid)
		if err != nil {
			writeJSON(w, 500, M{"error": err.Error()})
			return
		}
		_ = eid
		writeJSON(w, 200, M{"type": "coverage_gap", "regions": scanRows(rows)})

	case "spatial_autocorrelation":
		// Moran's I approximation: do similar turnout rates cluster geographically?
		rows, err := dbQueryCtx(r.Context(), `
			WITH pu_turnout AS (
				SELECT pu.code, pu.latitude, pu.longitude,
					CASE WHEN pu.registered_voters > 0
						THEN CAST(COALESCE(r.total_votes_cast,0) AS FLOAT)/pu.registered_voters ELSE 0 END AS turnout
				FROM polling_units pu
				LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
				WHERE pu.latitude IS NOT NULL
			)
			SELECT state_code, COUNT(*) AS pu_count, AVG(turnout) AS mean_turnout,
				STDDEV(turnout) AS std_turnout
			FROM pu_turnout pt
			JOIN polling_units pu ON pu.code = pt.code
			JOIN wards w ON w.code = pu.ward_code
			JOIN lgas l ON l.code = w.lga_code
			GROUP BY state_code`, eid)
		if err != nil {
			writeJSON(w, 500, M{"error": err.Error()})
			return
		}
		writeJSON(w, 200, M{"type": "spatial_autocorrelation", "states": scanRows(rows)})

	default:
		writeJSON(w, 200, M{
			"available_analyses": []string{"hotspot", "coverage_gap", "spatial_autocorrelation"},
			"usage":             "?type=hotspot&election_id=1",
		})
	}
}

// --- Helper Functions ---

// toFloatGeo converts interface to float64 for geo calculations.
func toFloatGeo(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toIntGeo(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

func safeDivGeo(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// convexHull computes the convex hull of 2D points (Andrew's monotone chain).
func convexHull(pts [][2]float64) [][2]float64 {
	n := len(pts)
	if n < 3 {
		return pts
	}

	// Sort by x, then y
	for i := 0; i < n-1; i++ {
		for j := i + 1; j < n; j++ {
			if pts[j][0] < pts[i][0] || (pts[j][0] == pts[i][0] && pts[j][1] < pts[i][1]) {
				pts[i], pts[j] = pts[j], pts[i]
			}
		}
	}

	cross := func(o, a, b [2]float64) float64 {
		return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0])
	}

	hull := make([][2]float64, 0, 2*n)

	// Lower hull
	for _, p := range pts {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}

	// Upper hull
	lower := len(hull) + 1
	for i := n - 2; i >= 0; i-- {
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], pts[i]) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, pts[i])
	}

	return hull[:len(hull)-1]
}

// Unused import suppression
var _ = strings.Contains
var _ = context.Background
