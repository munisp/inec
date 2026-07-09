package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
)

// ═══════════════════════════════════════════════════════════════════════════
// GeoLibre Integration — GeoJSON endpoints for the GeoLibre GIS viewer
// ═══════════════════════════════════════════════════════════════════════════

// GeoJSON types following RFC 7946
type geoJSONFeatureCollection struct {
	Type     string           `json:"type"`
	Features []geoJSONFeature `json:"features"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

type geoJSONFeature struct {
	Type       string         `json:"type"`
	Geometry   geoJSONGeom    `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type geoJSONGeom struct {
	Type        string `json:"type"`
	Coordinates any    `json:"coordinates"`
}

// ─── Polling Units as GeoJSON FeatureCollection ─────────────────────────

func handleGeoLibrePollingUnits(w http.ResponseWriter, r *http.Request) {
	electionID, _ := strconv.Atoi(r.URL.Query().Get("election_id"))
	stateCode := r.URL.Query().Get("state_code")
	format := r.URL.Query().Get("format") // geojson (default) or csv

	if electionID == 0 {
		electionID = 1
	}

	query := `
		SELECT p.polling_unit_code, p.name, p.latitude, p.longitude,
			p.registered_voters, p.ward_name, p.lga_name, p.state_name, p.state_code,
			r.status, r.total_votes_cast, r.total_valid_votes,
			COALESCE(r.total_votes_cast::float / NULLIF(p.registered_voters, 0) * 100, 0) as turnout_pct
		FROM polling_units p
		LEFT JOIN results r ON r.polling_unit_code = p.polling_unit_code AND r.election_id = $1
		WHERE p.latitude IS NOT NULL AND p.longitude IS NOT NULL
	`
	args := []any{electionID}

	if stateCode != "" {
		query += " AND p.state_code = $2"
		args = append(args, stateCode)
	}

	query += " ORDER BY p.state_code, p.lga_name, p.ward_name, p.polling_unit_code"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("query error: %v", err), 500)
		return
	}
	defer rows.Close()

	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{
			"source":      "INEC Election Management System",
			"election_id": electionID,
			"format":      "GeoJSON (RFC 7946)",
			"crs":         "EPSG:4326",
		},
	}

	for rows.Next() {
		var code, name, wardName, lgaName, stateName, stateCode string
		var lat, lng, turnoutPct float64
		var registeredVoters int
		var status *string
		var totalVotesCast, totalValidVotes *int

		if err := rows.Scan(&code, &name, &lat, &lng, &registeredVoters,
			&wardName, &lgaName, &stateName, &stateCode,
			&status, &totalVotesCast, &totalValidVotes, &turnoutPct); err != nil {
			continue
		}

		statusVal := "no_result"
		if status != nil {
			statusVal = *status
		}

		props := map[string]any{
			"code":              code,
			"name":              name,
			"ward_name":         wardName,
			"lga_name":          lgaName,
			"state_name":        stateName,
			"state_code":        stateCode,
			"registered_voters": registeredVoters,
			"status":            statusVal,
			"total_votes_cast":  totalVotesCast,
			"total_valid_votes": totalValidVotes,
			"turnout_pct":       math.Round(turnoutPct*100) / 100,
		}

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:       "Feature",
			Geometry:   geoJSONGeom{Type: "Point", Coordinates: [2]float64{lng, lat}},
			Properties: props,
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=inec-polling-units.csv")
		fmt.Fprintln(w, "code,name,latitude,longitude,state_code,lga_name,ward_name,registered_voters,status,turnout_pct")
		for _, f := range fc.Features {
			coords := f.Geometry.Coordinates.([2]float64)
			fmt.Fprintf(w, "%s,%s,%f,%f,%s,%s,%s,%d,%s,%.2f\n",
				f.Properties["code"], f.Properties["name"],
				coords[1], coords[0],
				f.Properties["state_code"], f.Properties["lga_name"], f.Properties["ward_name"],
				f.Properties["registered_voters"], f.Properties["status"], f.Properties["turnout_pct"])
		}
		return
	}

	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── Incidents as GeoJSON ───────────────────────────────────────────────

func handleGeoLibreIncidents(w http.ResponseWriter, r *http.Request) {
	electionID, _ := strconv.Atoi(r.URL.Query().Get("election_id"))
	if electionID == 0 {
		electionID = 1
	}
	severity := r.URL.Query().Get("severity")

	query := `
		SELECT i.id, i.incident_type, i.severity, i.description,
			i.polling_unit_code, i.state_code, i.latitude, i.longitude,
			i.reported_at, i.status, i.reporter_name
		FROM incidents i
		WHERE i.election_id = $1
			AND i.latitude IS NOT NULL AND i.longitude IS NOT NULL
	`
	args := []any{electionID}

	if severity != "" {
		query += " AND i.severity = $2"
		args = append(args, severity)
	}
	query += " ORDER BY i.reported_at DESC"

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("query error: %v", err), 500)
		return
	}
	defer rows.Close()

	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{
			"source":      "INEC Incident Reporting System",
			"election_id": electionID,
		},
	}

	for rows.Next() {
		var id int
		var incType, sev, desc, puCode, stateCode, reporter string
		var lat, lng float64
		var reportedAt, status string

		if err := rows.Scan(&id, &incType, &sev, &desc, &puCode, &stateCode,
			&lat, &lng, &reportedAt, &status, &reporter); err != nil {
			continue
		}

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: geoJSONGeom{Type: "Point", Coordinates: [2]float64{lng, lat}},
			Properties: map[string]any{
				"id":          id,
				"type":        incType,
				"severity":    sev,
				"description": desc,
				"pu_code":     puCode,
				"state_code":  stateCode,
				"reported_at": reportedAt,
				"status":      status,
				"reporter":    reporter,
			},
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── State Results Choropleth as GeoJSON ────────────────────────────────

func handleGeoLibreStateChoropleth(w http.ResponseWriter, r *http.Request) {
	electionID, _ := strconv.Atoi(r.URL.Query().Get("election_id"))
	if electionID == 0 {
		electionID = 1
	}

	query := `
		SELECT p.state_code, p.state_name,
			COUNT(DISTINCT p.polling_unit_code) as total_pus,
			COUNT(DISTINCT r.polling_unit_code) as reported_pus,
			SUM(p.registered_voters) as total_registered,
			COALESCE(SUM(r.total_votes_cast), 0) as total_cast
		FROM polling_units p
		LEFT JOIN results r ON r.polling_unit_code = p.polling_unit_code AND r.election_id = $1
		GROUP BY p.state_code, p.state_name
		ORDER BY p.state_code
	`

	rows, err := db.Query(query, electionID)
	if err != nil {
		http.Error(w, fmt.Sprintf("query error: %v", err), 500)
		return
	}
	defer rows.Close()

	// State centroids for Nigeria (approximate)
	centroids := map[string][2]float64{
		"AB": {7.52, 5.45}, "AD": {12.40, 9.33}, "AK": {7.85, 5.01},
		"AN": {6.94, 6.21}, "BA": {9.84, 10.31}, "BY": {6.07, 4.77},
		"BE": {8.77, 7.34}, "BO": {13.15, 11.85}, "CR": {8.53, 5.87},
		"DE": {5.68, 5.89}, "EB": {8.07, 6.26}, "ED": {5.93, 6.63},
		"EK": {5.31, 7.72}, "EN": {7.50, 6.45}, "FC": {7.49, 9.06},
		"GO": {11.17, 10.29}, "IM": {7.06, 5.57}, "JI": {9.56, 12.23},
		"KD": {7.43, 10.52}, "KN": {8.52, 12.00}, "KT": {7.60, 12.99},
		"KE": {4.20, 12.45}, "KO": {6.74, 7.80}, "KW": {4.55, 8.50},
		"LA": {3.35, 6.60}, "NA": {8.52, 8.54}, "NI": {5.60, 9.93},
		"OG": {3.35, 7.16}, "ON": {5.19, 7.25}, "OS": {4.57, 7.77},
		"OY": {3.93, 7.85}, "PL": {9.52, 9.22}, "RI": {7.03, 4.82},
		"SO": {5.24, 13.06}, "TA": {11.36, 8.89}, "YO": {11.50, 12.00},
		"ZA": {6.66, 12.17},
	}

	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{
			"source":      "INEC State-Level Election Data",
			"election_id": electionID,
			"note":        "Approximate state boundaries using centroid polygons. For precise boundaries, load Nigeria admin boundary shapefiles into GeoLibre.",
		},
	}

	for rows.Next() {
		var stateCode, stateName string
		var totalPUs, reportedPUs int
		var totalRegistered, totalCast int64

		if err := rows.Scan(&stateCode, &stateName, &totalPUs, &reportedPUs, &totalRegistered, &totalCast); err != nil {
			continue
		}

		centroid, ok := centroids[stateCode]
		if !ok {
			continue
		}

		completionPct := float64(0)
		if totalPUs > 0 {
			completionPct = float64(reportedPUs) / float64(totalPUs) * 100
		}
		turnoutPct := float64(0)
		if totalRegistered > 0 {
			turnoutPct = float64(totalCast) / float64(totalRegistered) * 100
		}

		// Generate approximate state polygon (0.5 degree square around centroid)
		d := 0.5
		lng, lat := centroid[0], centroid[1]
		polygon := [][][2]float64{{
			{lng - d, lat - d}, {lng + d, lat - d},
			{lng + d, lat + d}, {lng - d, lat + d},
			{lng - d, lat - d},
		}}

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: geoJSONGeom{Type: "Polygon", Coordinates: polygon},
			Properties: map[string]any{
				"code":             stateCode,
				"name":             stateName,
				"total_pus":        totalPUs,
				"reported_pus":     reportedPUs,
				"completion_pct":   math.Round(completionPct*100) / 100,
				"total_registered": totalRegistered,
				"total_cast":       totalCast,
				"turnout_pct":      math.Round(turnoutPct*100) / 100,
			},
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── BVAS Devices as GeoJSON ────────────────────────────────────────────

func handleGeoLibreBVAS(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	query := `
		SELECT d.id, d.polling_unit_code, d.status,
			d.battery_level, d.accreditation_count,
			p.latitude, p.longitude,
			d.last_sync_at, d.firmware_version
		FROM bvas_devices d
		JOIN polling_units p ON p.code = d.polling_unit_code
		WHERE p.latitude IS NOT NULL AND p.longitude IS NOT NULL
	`
	args := []any{}

	if status != "" {
		query += " AND d.status = $1"
		args = append(args, status)
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("query error: %v", err), 500)
		return
	}
	defer rows.Close()

	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{"source": "INEC BVAS Device Registry"},
	}

	for rows.Next() {
		var deviceID, puCode, devStatus, lastSync, firmware string
		var batteryLevel, accreditations int
		var lat, lng float64

		if err := rows.Scan(&deviceID, &puCode, &devStatus, &batteryLevel,
			&accreditations, &lat, &lng, &lastSync, &firmware); err != nil {
			continue
		}

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: geoJSONGeom{Type: "Point", Coordinates: [2]float64{lng, lat}},
			Properties: map[string]any{
				"device_id":       deviceID,
				"pu_code":         puCode,
				"status":          devStatus,
				"battery_pct":     batteryLevel,
				"accreditations":  accreditations,
				"last_sync":       lastSync,
				"firmware_version": firmware,
			},
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── Officials Tracking as GeoJSON ──────────────────────────────────────

func handleGeoLibreOfficials(w http.ResponseWriter, r *http.Request) {
	query := `
		SELECT staff_id, role, latitude, longitude, pu_code,
			activity, battery_pct, recorded_at
		FROM official_tracking_history
		WHERE recorded_at > NOW() - INTERVAL '1 hour'
		ORDER BY recorded_at DESC
	`

	rows, err := db.Query(query)
	if err != nil {
		// Fallback: if table doesn't exist, return empty
		fc := geoJSONFeatureCollection{Type: "FeatureCollection", Features: []geoJSONFeature{}}
		w.Header().Set("Content-Type", "application/geo+json")
		json.NewEncoder(w).Encode(fc)
		return
	}
	defer rows.Close()

	seen := map[string]bool{}
	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{"source": "INEC Official Tracking (last 1hr)"},
	}

	for rows.Next() {
		var staffID, role, puCode, activity, recordedAt string
		var lat, lng float64
		var batteryPct int

		if err := rows.Scan(&staffID, &role, &lat, &lng, &puCode,
			&activity, &batteryPct, &recordedAt); err != nil {
			continue
		}

		// Only latest position per official
		if seen[staffID] {
			continue
		}
		seen[staffID] = true

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: geoJSONGeom{Type: "Point", Coordinates: [2]float64{lng, lat}},
			Properties: map[string]any{
				"staff_id":    staffID,
				"role":        role,
				"pu_code":     puCode,
				"activity":    activity,
				"battery_pct": batteryPct,
				"updated_at":  recordedAt,
			},
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── GeoLibre Project Export ────────────────────────────────────────────

func handleGeoLibreProjectExport(w http.ResponseWriter, r *http.Request) {
	electionID, _ := strconv.Atoi(r.URL.Query().Get("election_id"))
	if electionID == 0 {
		electionID = 1
	}

	// Build a .geolibre.json project file with election data layers
	project := map[string]any{
		"version":     "1.0",
		"name":        fmt.Sprintf("INEC Election %d — GeoLibre Analysis", electionID),
		"description": "Exported from INEC Election Management Platform for GeoLibre GIS analysis",
		"center":      [2]float64{8.0, 9.5},
		"zoom":        5.8,
		"basemap":     "https://tiles.openfreemap.org/styles/liberty",
		"layers": []map[string]any{
			{
				"name":    "Polling Units",
				"type":    "geojson-url",
				"visible": true,
				"url":     fmt.Sprintf("/geolibre/geojson/polling-units?election_id=%d", electionID),
			},
			{
				"name":    "State Choropleth",
				"type":    "geojson-url",
				"visible": true,
				"url":     fmt.Sprintf("/geolibre/geojson/states?election_id=%d", electionID),
			},
			{
				"name":    "Incidents",
				"type":    "geojson-url",
				"visible": true,
				"url":     fmt.Sprintf("/geolibre/geojson/incidents?election_id=%d", electionID),
			},
			{
				"name":    "BVAS Devices",
				"type":    "geojson-url",
				"visible": false,
				"url":     "/geolibre/geojson/bvas",
			},
			{
				"name":    "Official Tracking",
				"type":    "geojson-url",
				"visible": false,
				"url":     "/geolibre/geojson/officials",
			},
		},
		"metadata": map[string]any{
			"platform":    "INEC Election Management System",
			"generated":   "auto",
			"election_id": electionID,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=inec-election-%d.geolibre.json", electionID))
	json.NewEncoder(w).Encode(project)
}

// ─── Spatial Query endpoint (buffer, within, nearest) ───────────────────

func handleGeoLibreSpatialQuery(w http.ResponseWriter, r *http.Request) {
	queryType := r.URL.Query().Get("type")      // buffer, nearest, within
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	radius, _ := strconv.ParseFloat(r.URL.Query().Get("radius_km"), 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	if lat == 0 || lng == 0 {
		http.Error(w, "lat and lng are required", 400)
		return
	}
	if radius == 0 {
		radius = 5.0
	}
	if limit == 0 {
		limit = 50
	}

	var query string
	var args []any

	switch strings.ToLower(queryType) {
	case "nearest":
		// Find N nearest polling units to a point
		query = `
			SELECT polling_unit_code, name, latitude, longitude, registered_voters,
				state_code, lga_name,
				(6371 * acos(cos(radians($1)) * cos(radians(latitude))
				* cos(radians(longitude) - radians($2))
				+ sin(radians($1)) * sin(radians(latitude)))) as distance_km
			FROM polling_units
			WHERE latitude IS NOT NULL AND longitude IS NOT NULL
			ORDER BY distance_km
			LIMIT $3
		`
		args = []any{lat, lng, limit}

	case "within":
		// Find all PUs within radius_km of a point
		query = `
			SELECT polling_unit_code, name, latitude, longitude, registered_voters,
				state_code, lga_name,
				(6371 * acos(cos(radians($1)) * cos(radians(latitude))
				* cos(radians(longitude) - radians($2))
				+ sin(radians($1)) * sin(radians(latitude)))) as distance_km
			FROM polling_units
			WHERE latitude IS NOT NULL AND longitude IS NOT NULL
			HAVING distance_km <= $3
			ORDER BY distance_km
		`
		args = []any{lat, lng, radius}

	default: // buffer
		query = `
			SELECT polling_unit_code, name, latitude, longitude, registered_voters,
				state_code, lga_name,
				(6371 * acos(cos(radians($1)) * cos(radians(latitude))
				* cos(radians(longitude) - radians($2))
				+ sin(radians($1)) * sin(radians(latitude)))) as distance_km
			FROM polling_units
			WHERE latitude IS NOT NULL AND longitude IS NOT NULL
				AND (6371 * acos(cos(radians($1)) * cos(radians(latitude))
				* cos(radians(longitude) - radians($2))
				+ sin(radians($1)) * sin(radians(latitude)))) <= $3
			ORDER BY distance_km
		`
		args = []any{lat, lng, radius}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("query error: %v", err), 500)
		return
	}
	defer rows.Close()

	fc := geoJSONFeatureCollection{
		Type:     "FeatureCollection",
		Features: []geoJSONFeature{},
		Metadata: map[string]any{
			"query_type": queryType,
			"center":     [2]float64{lng, lat},
			"radius_km":  radius,
		},
	}

	for rows.Next() {
		var code, name, stateCode, lgaName string
		var puLat, puLng, distKm float64
		var registeredVoters int

		if err := rows.Scan(&code, &name, &puLat, &puLng, &registeredVoters,
			&stateCode, &lgaName, &distKm); err != nil {
			continue
		}

		fc.Features = append(fc.Features, geoJSONFeature{
			Type:     "Feature",
			Geometry: geoJSONGeom{Type: "Point", Coordinates: [2]float64{puLng, puLat}},
			Properties: map[string]any{
				"code":              code,
				"name":              name,
				"state_code":        stateCode,
				"lga_name":          lgaName,
				"registered_voters": registeredVoters,
				"distance_km":       math.Round(distKm*1000) / 1000,
			},
		})
	}

	fc.Metadata["feature_count"] = len(fc.Features)
	w.Header().Set("Content-Type", "application/geo+json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(fc)
}

// ─── Register GeoLibre routes ───────────────────────────────────────────

func registerGeoLibreRoutes(r *mux.Router) {
	gl := r.PathPrefix("/geolibre").Subrouter()

	// GeoJSON data endpoints
	gl.HandleFunc("/geojson/polling-units", handleGeoLibrePollingUnits).Methods("GET")
	gl.HandleFunc("/geojson/incidents", handleGeoLibreIncidents).Methods("GET")
	gl.HandleFunc("/geojson/states", handleGeoLibreStateChoropleth).Methods("GET")
	gl.HandleFunc("/geojson/bvas", handleGeoLibreBVAS).Methods("GET")
	gl.HandleFunc("/geojson/officials", handleGeoLibreOfficials).Methods("GET")

	// GeoLibre project export
	gl.HandleFunc("/project", handleGeoLibreProjectExport).Methods("GET")

	// Spatial queries
	gl.HandleFunc("/spatial/query", handleGeoLibreSpatialQuery).Methods("GET")
}
