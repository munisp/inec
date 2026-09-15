-- R5-015: persisted collation rollups are upserted per (election, level, area).
-- The PG schema never got the UNIQUE that the dev schema had, so the rollup
-- write path could not use ON CONFLICT. Dedupe defensively, then enforce.
DELETE FROM collation_results a
USING collation_results b
WHERE a.election_id = b.election_id AND a.level = b.level AND a.area_code = b.area_code
  AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS collation_results_area_unique
    ON collation_results (election_id, level, area_code);
