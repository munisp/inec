-- Rollback: BVAS extended: heartbeats, sync, capabilities, location

DROP TABLE IF EXISTS bvas_sync_queue CASCADE;
DROP TABLE IF EXISTS bvas_location_logs CASCADE;
DROP TABLE IF EXISTS bvas_heartbeats CASCADE;
DROP TABLE IF EXISTS bvas_device_capabilities CASCADE;
DROP TABLE IF EXISTS bvas_accreditations CASCADE;

