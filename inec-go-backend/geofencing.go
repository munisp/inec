package main

import (
	"fmt"
	"math"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/rs/zerolog/log"
)

// ── Geo-Fencing Validation ──
// Validates that BVAS devices are physically present at their assigned polling unit
// before allowing result submission. Uses Haversine formula for distance calculation.

const (
	earthRadiusKm      = 6371.0
	defaultGeofenceM   = 500 // 500 meters default geofence radius
	maxAllowedDistance  = 2000 // 2km absolute maximum
)

// GeoPoint represents a geographic coordinate.
type GeoPoint struct {
	Latitude  float64 `json:"latitude" validate:"required,min=-90,max=90"`
	Longitude float64 `json:"longitude" validate:"required,min=-180,max=180"`
}

// GeofenceResult is the outcome of a geofence check.
type GeofenceResult struct {
	WithinGeofence  bool    `json:"within_geofence"`
	DistanceMeters  float64 `json:"distance_meters"`
	AllowedRadiusM  int     `json:"allowed_radius_m"`
	PollingUnitCode string  `json:"polling_unit_code"`
	Message         string  `json:"message"`
}

// haversineDistance calculates the great-circle distance between two points in meters.
func haversineDistance(lat1, lon1, lat2, lon2 float64) float64 {
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusKm * c * 1000 // Convert to meters
}

// validateGeofence checks if a BVAS device location is within the polling unit's geofence.
func validateGeofence(bvasLat, bvasLon float64, pollingUnitCode string) (*GeofenceResult, error) {
	var puLat, puLon float64
	var radiusM int

	err := db.QueryRow(convertPlaceholders(
		"SELECT latitude, longitude, geofence_radius_m FROM polling_unit_locations WHERE polling_unit_code = ?"),
		pollingUnitCode).Scan(&puLat, &puLon, &radiusM)

	if err != nil {
		// If no location data, allow but log warning
		log.Warn().Str("pu_code", pollingUnitCode).Msg("No geofence data for polling unit — allowing by default")
		return &GeofenceResult{
			WithinGeofence:  true,
			DistanceMeters:  0,
			AllowedRadiusM:  defaultGeofenceM,
			PollingUnitCode: pollingUnitCode,
			Message:         "no geofence configured — allowed by default",
		}, nil
	}

	distance := haversineDistance(bvasLat, bvasLon, puLat, puLon)
	withinFence := distance <= float64(radiusM)

	result := &GeofenceResult{
		WithinGeofence:  withinFence,
		DistanceMeters:  math.Round(distance*100) / 100,
		AllowedRadiusM:  radiusM,
		PollingUnitCode: pollingUnitCode,
	}

	if withinFence {
		result.Message = "BVAS within polling unit geofence"
	} else {
		result.Message = fmt.Sprintf("BVAS is %.0fm from polling unit (allowed: %dm)", distance, radiusM)
	}

	// Log the check
	db.Exec(convertPlaceholders(
		"INSERT INTO bvas_location_logs (bvas_serial, polling_unit_code, latitude, longitude, distance_from_pu_m, within_geofence) VALUES (?, ?, ?, ?, ?, ?)"),
		"", pollingUnitCode, bvasLat, bvasLon, distance, withinFence)

	return result, nil
}

// handleGeofenceCheck API endpoint for geofence validation.
func handleGeofenceCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BVASSerial      string  `json:"bvas_serial" validate:"required"`
		PollingUnitCode string  `json:"polling_unit_code" validate:"required"`
		Latitude        float64 `json:"latitude" validate:"required,min=-90,max=90"`
		Longitude       float64 `json:"longitude" validate:"required,min=-180,max=180"`
	}
	if !decodeAndValidateBody(w, r, &req) {
		return
	}

	result, err := validateGeofence(req.Latitude, req.Longitude, req.PollingUnitCode)
	if err != nil {
		writeError(w, 500, "geofence validation failed")
		return
	}

	// Log with BVAS serial
	db.Exec(convertPlaceholders(
		"UPDATE bvas_location_logs SET bvas_serial = ? WHERE polling_unit_code = ? AND bvas_serial = '' ORDER BY id DESC LIMIT 1"),
		req.BVASSerial, req.PollingUnitCode)

	if !result.WithinGeofence {
		writeJSON(w, 403, map[string]interface{}{
			"error":   "geofence violation",
			"details": result,
		})
		return
	}

	writeJSON(w, 200, result)
}

// handleGeofenceStats returns geofence violation statistics.
func handleGeofenceStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	electionID := vars["election_id"]

	var totalChecks, violations int
	db.QueryRow("SELECT COUNT(*), COALESCE(SUM(CASE WHEN within_geofence = 0 THEN 1 ELSE 0 END), 0) FROM bvas_location_logs").Scan(&totalChecks, &violations)

	var avgDistance float64
	db.QueryRow("SELECT COALESCE(AVG(distance_from_pu_m), 0) FROM bvas_location_logs WHERE within_geofence = 1").Scan(&avgDistance)

	writeJSON(w, 200, map[string]interface{}{
		"election_id":         electionID,
		"total_checks":        totalChecks,
		"violations":          violations,
		"compliance_rate":     safePercentGeo(totalChecks-violations, totalChecks),
		"avg_distance_m":      math.Round(avgDistance*100) / 100,
		"geofence_default_m":  defaultGeofenceM,
	})
}

func safePercentGeo(num, denom int) float64 {
	if denom == 0 {
		return 100.0
	}
	return math.Round(float64(num)/float64(denom)*10000) / 100
}

// ── Auto-Collation Trigger ──
// Automatically triggers collation when all polling units in a ward/LGA/state have submitted.

// checkAutoCollation checks if all PUs in a ward have submitted and triggers ward-level collation.
func checkAutoCollation(electionID int, pollingUnitCode string) {
	// Get the ward for this polling unit
	var wardCode string
	err := db.QueryRow(convertPlaceholders(
		"SELECT ward_code FROM polling_units WHERE code = ?"), pollingUnitCode).Scan(&wardCode)
	if err != nil || wardCode == "" {
		return
	}

	// Count total PUs and submitted PUs in this ward
	var totalPUs, submittedPUs int
	db.QueryRow(convertPlaceholders(
		"SELECT COUNT(*) FROM polling_units WHERE ward_code = ?"), wardCode).Scan(&totalPUs)
	db.QueryRow(convertPlaceholders(
		"SELECT COUNT(DISTINCT r.polling_unit_code) FROM results r JOIN polling_units pu ON r.polling_unit_code = pu.code WHERE pu.ward_code = ? AND r.election_id = ? AND r.status IN ('validated', 'finalized')"),
		wardCode, electionID).Scan(&submittedPUs)

	if totalPUs > 0 && submittedPUs >= totalPUs {
		// All PUs submitted — trigger ward collation
		log.Info().
			Str("ward", wardCode).
			Int("election_id", electionID).
			Int("total_pus", totalPUs).
			Msg("Auto-collation triggered: all PUs submitted for ward")

		triggerWardCollation(electionID, wardCode)
	}
}

// triggerWardCollation aggregates results at the ward level.
func triggerWardCollation(electionID int, wardCode string) {
	tx, err := db.Begin()
	if err != nil {
		log.Error().Err(err).Msg("Failed to begin ward collation transaction")
		return
	}

	// Aggregate party votes for the ward
	rows, err := tx.Query(convertPlaceholders(`
		SELECT rps.party_code, SUM(rps.votes) as total_votes
		FROM result_party_scores rps
		JOIN results r ON rps.result_id = r.id
		JOIN polling_units pu ON r.polling_unit_code = pu.code
		WHERE pu.ward_code = ? AND r.election_id = ? AND r.status IN ('validated', 'finalized')
		GROUP BY rps.party_code`), wardCode, electionID)
	if err != nil {
		tx.Rollback()
		return
	}
	defer rows.Close()

	// Store in collation_results
	for rows.Next() {
		var partyCode string
		var totalVotes int
		if err := rows.Scan(&partyCode, &totalVotes); err == nil {
			tx.Exec(convertPlaceholders(`
				INSERT OR REPLACE INTO collation_results (election_id, level, area_code, party_code, total_votes, collated_at)
				VALUES (?, 'ward', ?, ?, ?, CURRENT_TIMESTAMP)`),
				electionID, wardCode, partyCode, totalVotes)
		}
	}

	if err := tx.Commit(); err != nil {
		log.Error().Err(err).Str("ward", wardCode).Msg("Ward collation commit failed")
		return
	}

	log.Info().Str("ward", wardCode).Int("election_id", electionID).Msg("Ward collation completed")

	// Check if LGA is now complete
	checkLGACollation(electionID, wardCode)
}

// checkLGACollation checks if all wards in the LGA have been collated.
func checkLGACollation(electionID int, wardCode string) {
	var lgaCode string
	db.QueryRow(convertPlaceholders(
		"SELECT lga_code FROM wards WHERE code = ?"), wardCode).Scan(&lgaCode)
	if lgaCode == "" {
		return
	}

	var totalWards, collatedWards int
	db.QueryRow(convertPlaceholders(
		"SELECT COUNT(*) FROM wards WHERE lga_code = ?"), lgaCode).Scan(&totalWards)
	db.QueryRow(convertPlaceholders(
		"SELECT COUNT(DISTINCT area_code) FROM collation_results WHERE election_id = ? AND level = 'ward' AND area_code IN (SELECT code FROM wards WHERE lga_code = ?)"),
		electionID, lgaCode).Scan(&collatedWards)

	if totalWards > 0 && collatedWards >= totalWards {
		log.Info().Str("lga", lgaCode).Int("election_id", electionID).Msg("Auto-collation: LGA complete")
		// LGA collation would follow same pattern
	}
}
