-- Rollback: Geospatial: landmarks, tracking, geofences, crowd

DROP TABLE IF EXISTS pu_photos CASCADE;
DROP TABLE IF EXISTS incident_locations CASCADE;
DROP TABLE IF EXISTS geofenced_submissions CASCADE;
DROP TABLE IF EXISTS geofence_zones CASCADE;
DROP TABLE IF EXISTS geofence_attestations CASCADE;
DROP TABLE IF EXISTS geo_events CASCADE;
DROP TABLE IF EXISTS geo_analytics_cache CASCADE;
DROP TABLE IF EXISTS crowd_alerts CASCADE;
DROP TABLE IF EXISTS crowd_density CASCADE;
DROP TABLE IF EXISTS official_tracking_history CASCADE;
DROP TABLE IF EXISTS official_tracking CASCADE;
DROP TABLE IF EXISTS landmarks CASCADE;

