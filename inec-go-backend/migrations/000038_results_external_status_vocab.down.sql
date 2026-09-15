ALTER TABLE results DROP CONSTRAINT IF EXISTS results_tigerbeetle_status_check;
ALTER TABLE results ADD CONSTRAINT results_tigerbeetle_status_check
    CHECK (tigerbeetle_status = ANY (ARRAY['PENDING','POSTED','VOIDED']));
ALTER TABLE results DROP CONSTRAINT IF EXISTS results_hyperledger_status_check;
ALTER TABLE results ADD CONSTRAINT results_hyperledger_status_check
    CHECK (hyperledger_status = ANY (ARRAY['PENDING','CONFIRMED','FAILED']));
