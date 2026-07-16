-- Rollback: Election management: lifecycle, materials, staff, EMS

DROP TABLE IF EXISTS stakeholders CASCADE;
DROP TABLE IF EXISTS stakeholder_incidents CASCADE;
DROP TABLE IF EXISTS result_signatures CASCADE;
DROP TABLE IF EXISTS result_party_scores CASCADE;
DROP TABLE IF EXISTS kiosk_sessions CASCADE;
DROP TABLE IF EXISTS dedup_resolutions CASCADE;
DROP TABLE IF EXISTS dedup_jobs CASCADE;
DROP TABLE IF EXISTS dedup_candidates CASCADE;
DROP TABLE IF EXISTS ems_workflows CASCADE;
DROP TABLE IF EXISTS ems_workflow_phases CASCADE;
DROP TABLE IF EXISTS election_templates CASCADE;
DROP TABLE IF EXISTS election_state_log CASCADE;
DROP TABLE IF EXISTS election_staff_assignments CASCADE;
DROP TABLE IF EXISTS election_materials CASCADE;
DROP TABLE IF EXISTS election_lifecycle CASCADE;
DROP TABLE IF EXISTS election_archive CASCADE;

