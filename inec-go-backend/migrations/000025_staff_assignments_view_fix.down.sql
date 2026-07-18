-- Rollback 000025
DROP VIEW IF EXISTS staff_assignments;

CREATE TABLE IF NOT EXISTS staff_assignments (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    election_id INTEGER NOT NULL REFERENCES elections(id),
    role TEXT NOT NULL,
    state_code TEXT,
    lga_code TEXT,
    ward_code TEXT,
    polling_unit_code TEXT,
    assigned_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE election_staff_assignments ALTER COLUMN area_type DROP DEFAULT;
ALTER TABLE election_staff_assignments ALTER COLUMN area_code DROP DEFAULT;
