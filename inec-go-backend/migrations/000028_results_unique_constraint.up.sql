-- R4-22: close the TOCTOU duplicate-result race.
-- handleSubmitResult previously did SELECT COUNT(*) then INSERT with no
-- uniqueness enforcement, so two concurrent submissions for the same
-- (election, polling unit) both succeeded. A UNIQUE constraint makes the
-- database the arbiter; the handler maps the violation to 409 Conflict.
--
-- Dedup guard: if a legacy deployment somehow accumulated exact duplicates,
-- keep the earliest row per (election_id, polling_unit_code) so the
-- constraint can be created.
DELETE FROM results a
USING results b
WHERE a.election_id = b.election_id
  AND a.polling_unit_code = b.polling_unit_code
  AND a.id > b.id;

ALTER TABLE results
    ADD CONSTRAINT results_election_pu_unique UNIQUE (election_id, polling_unit_code);
