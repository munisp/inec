// Package geo provides geospatial operations using PostGIS —
// official tracking, geofencing, landmarks, crowd density, and polling unit mapping.
package geo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// Point represents a geographic coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Official represents a tracked election official.
type Official struct {
	ID           int       `json:"id"`
	StaffID      string    `json:"staff_id"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Location     Point     `json:"location"`
	Speed        float64   `json:"speed_kmh"`
	Heading      float64   `json:"heading"`
	Battery      int       `json:"battery_pct"`
	Status       string    `json:"status"`
	UpdatedAt    time.Time `json:"updated_at"`
	PollingUnit  string    `json:"polling_unit,omitempty"`
}

// Geofence defines a monitored geographic boundary.
type Geofence struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // polling_unit, restricted_zone, lga_boundary
	Center    Point     `json:"center"`
	RadiusM   float64   `json:"radius_meters"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

// GeofenceEvent records boundary crossings.
type GeofenceEvent struct {
	OfficialID  int       `json:"official_id"`
	GeofenceID  int       `json:"geofence_id"`
	EventType   string    `json:"event_type"` // enter, exit, dwell
	Timestamp   time.Time `json:"timestamp"`
	Location    Point     `json:"location"`
}

// Landmark represents a point of interest on the map.
type Landmark struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Location Point  `json:"location"`
	State    string `json:"state"`
	LGA      string `json:"lga"`
}

// CrowdDensity represents a crowd density observation.
type CrowdDensity struct {
	ID        int       `json:"id"`
	Location  Point     `json:"location"`
	Density   int       `json:"density"` // estimated crowd size
	Level     string    `json:"level"`   // low, medium, high, critical
	Source    string    `json:"source"`
	ReportedAt time.Time `json:"reported_at"`
}

// HeatmapPoint for turnout/density visualization.
type HeatmapPoint struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Intensity float64 `json:"intensity"`
}

// Service provides geospatial operations.
type Service struct {
	db *sql.DB
}

// NewService creates a new geo service backed by PostGIS.
func NewService(db *sql.DB) *Service {
	return &Service{db: db}
}

// UpdateOfficialLocation records a new GPS position for an official.
func (s *Service) UpdateOfficialLocation(ctx context.Context, staffID string, loc Point, speed, heading float64, battery int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE officials SET
		 location = ST_SetSRID(ST_MakePoint($1, $2), 4326),
		 speed = $3, heading = $4, battery_level = $5, last_seen = NOW()
		 WHERE staff_id = $6`,
		loc.Lng, loc.Lat, speed, heading, battery, staffID)
	if err != nil {
		return fmt.Errorf("update location: %w", err)
	}

	// Check geofence triggers
	go s.checkGeofences(context.Background(), staffID, loc)
	return nil
}

// GetActiveOfficials returns officials with recent GPS data.
func (s *Service) GetActiveOfficials(ctx context.Context, since time.Duration) ([]Official, error) {
	cutoff := time.Now().Add(-since)
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.staff_id, o.name, o.role,
		 ST_Y(o.location::geometry) as lat, ST_X(o.location::geometry) as lng,
		 COALESCE(o.speed, 0), COALESCE(o.heading, 0),
		 COALESCE(o.battery_level, 100), COALESCE(o.status, 'active'), o.last_seen
		 FROM officials o
		 WHERE o.last_seen > $1 AND o.location IS NOT NULL
		 ORDER BY o.last_seen DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var officials []Official
	for rows.Next() {
		var o Official
		if err := rows.Scan(&o.ID, &o.StaffID, &o.Name, &o.Role,
			&o.Location.Lat, &o.Location.Lng,
			&o.Speed, &o.Heading, &o.Battery, &o.Status, &o.UpdatedAt); err != nil {
			continue
		}
		officials = append(officials, o)
	}
	return officials, nil
}

// GetOfficialsByState returns officials within a state boundary.
func (s *Service) GetOfficialsByState(ctx context.Context, stateCode string) ([]Official, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.staff_id, o.name, o.role,
		 ST_Y(o.location::geometry) as lat, ST_X(o.location::geometry) as lng,
		 COALESCE(o.speed, 0), COALESCE(o.heading, 0),
		 COALESCE(o.battery_level, 100), COALESCE(o.status, 'active'), o.last_seen
		 FROM officials o
		 WHERE o.state_code = $1 AND o.location IS NOT NULL
		 ORDER BY o.last_seen DESC`, stateCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var officials []Official
	for rows.Next() {
		var o Official
		if err := rows.Scan(&o.ID, &o.StaffID, &o.Name, &o.Role,
			&o.Location.Lat, &o.Location.Lng,
			&o.Speed, &o.Heading, &o.Battery, &o.Status, &o.UpdatedAt); err != nil {
			continue
		}
		officials = append(officials, o)
	}
	return officials, nil
}

// NearbyOfficials finds officials within a radius (meters) of a point.
func (s *Service) NearbyOfficials(ctx context.Context, center Point, radiusMeters float64) ([]Official, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT o.id, o.staff_id, o.name, o.role,
		 ST_Y(o.location::geometry) as lat, ST_X(o.location::geometry) as lng,
		 COALESCE(o.speed, 0), COALESCE(o.heading, 0),
		 COALESCE(o.battery_level, 100), COALESCE(o.status, 'active'), o.last_seen,
		 ST_Distance(o.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) as distance
		 FROM officials o
		 WHERE ST_DWithin(o.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)
		 AND o.location IS NOT NULL
		 ORDER BY distance`, center.Lng, center.Lat, radiusMeters)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var officials []Official
	for rows.Next() {
		var o Official
		var dist float64
		if err := rows.Scan(&o.ID, &o.StaffID, &o.Name, &o.Role,
			&o.Location.Lat, &o.Location.Lng,
			&o.Speed, &o.Heading, &o.Battery, &o.Status, &o.UpdatedAt, &dist); err != nil {
			continue
		}
		officials = append(officials, o)
	}
	return officials, nil
}

// CreateGeofence creates a circular geofence.
func (s *Service) CreateGeofence(ctx context.Context, fence *Geofence) (int, error) {
	var id int
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO geofences (name, type, center, radius_meters, active)
		 VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6)
		 RETURNING id`,
		fence.Name, fence.Type, fence.Center.Lng, fence.Center.Lat, fence.RadiusM, fence.Active).
		Scan(&id)
	return id, err
}

// CheckPointInGeofence tests if a point is within a geofence.
func (s *Service) CheckPointInGeofence(ctx context.Context, point Point, geofenceID int) (bool, error) {
	var inside bool
	err := s.db.QueryRowContext(ctx,
		`SELECT ST_DWithin(
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			center::geography,
			radius_meters
		) FROM geofences WHERE id = $3`,
		point.Lng, point.Lat, geofenceID).Scan(&inside)
	return inside, err
}

// GetTurnoutHeatmap returns polling unit turnout as heatmap data.
func (s *Service) GetTurnoutHeatmap(ctx context.Context, electionID int) ([]HeatmapPoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ST_Y(pu.location::geometry), ST_X(pu.location::geometry),
		 CAST(COALESCE(r.accredited_voters, 0) AS FLOAT) / GREATEST(pu.registered_voters, 1)
		 FROM polling_units pu
		 LEFT JOIN results r ON r.polling_unit_code = pu.code AND r.election_id = $1
		 WHERE pu.location IS NOT NULL`, electionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []HeatmapPoint
	for rows.Next() {
		var p HeatmapPoint
		if err := rows.Scan(&p.Lat, &p.Lng, &p.Intensity); err != nil {
			continue
		}
		points = append(points, p)
	}
	return points, nil
}

// GetLandmarks returns landmarks, optionally filtered by state.
func (s *Service) GetLandmarks(ctx context.Context, stateFilter string) ([]Landmark, error) {
	query := `SELECT id, name, type, ST_Y(location::geometry), ST_X(location::geometry),
	           COALESCE(state, ''), COALESCE(lga, '')
	           FROM landmarks WHERE 1=1`
	args := []interface{}{}
	if stateFilter != "" {
		query += ` AND state = $1`
		args = append(args, stateFilter)
	}
	query += ` ORDER BY name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var landmarks []Landmark
	for rows.Next() {
		var l Landmark
		if err := rows.Scan(&l.ID, &l.Name, &l.Type, &l.Location.Lat, &l.Location.Lng, &l.State, &l.LGA); err != nil {
			continue
		}
		landmarks = append(landmarks, l)
	}
	return landmarks, nil
}

// GetCrowdDensity returns recent crowd density reports.
func (s *Service) GetCrowdDensity(ctx context.Context, since time.Duration) ([]CrowdDensity, error) {
	cutoff := time.Now().Add(-since)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, ST_Y(location::geometry), ST_X(location::geometry),
		 estimated_crowd, level, source, reported_at
		 FROM crowd_density WHERE reported_at > $1
		 ORDER BY reported_at DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reports []CrowdDensity
	for rows.Next() {
		var c CrowdDensity
		if err := rows.Scan(&c.ID, &c.Location.Lat, &c.Location.Lng,
			&c.Density, &c.Level, &c.Source, &c.ReportedAt); err != nil {
			continue
		}
		reports = append(reports, c)
	}
	return reports, nil
}

// checkGeofences evaluates all active geofences for an official.
func (s *Service) checkGeofences(ctx context.Context, staffID string, loc Point) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT g.id, g.name, g.type,
		 ST_DWithin(
			ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography,
			g.center::geography,
			g.radius_meters
		 ) as inside
		 FROM geofences g WHERE g.active = TRUE`, loc.Lng, loc.Lat)
	if err != nil {
		log.Error().Err(err).Msg("Geofence check failed")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var fenceID int
		var name, fenceType string
		var inside bool
		if err := rows.Scan(&fenceID, &name, &fenceType, &inside); err != nil {
			continue
		}
		if inside {
			log.Debug().Str("staff_id", staffID).Str("geofence", name).Msg("Official inside geofence")
		}
	}
}
