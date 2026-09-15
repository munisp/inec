-- W2/R5-016: the result pipeline writes tigerbeetle_status='NOT_APPLICABLE'
-- (electoral results never touch the settlement ledger) and
-- hyperledger_status='NOT_CONFIGURED' on finalize, but the CHECK constraints
-- predated those values, so submit/finalize/correction INSERTs failed with
-- 23514. Align the constraint with the actual vocabulary.
ALTER TABLE results DROP CONSTRAINT IF EXISTS results_tigerbeetle_status_check;
ALTER TABLE results ADD CONSTRAINT results_tigerbeetle_status_check
    CHECK (tigerbeetle_status = ANY (ARRAY['PENDING','POSTED','VOIDED','NOT_APPLICABLE']));

ALTER TABLE results DROP CONSTRAINT IF EXISTS results_hyperledger_status_check;
ALTER TABLE results ADD CONSTRAINT results_hyperledger_status_check
    CHECK (hyperledger_status = ANY (ARRAY['PENDING','CONFIRMED','FAILED','NOT_CONFIGURED']));
