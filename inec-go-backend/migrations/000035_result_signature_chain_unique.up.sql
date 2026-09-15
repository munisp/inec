-- R5-054: prevent cross-replica forks of the result-signature chain.
-- Concurrent signers on different replicas could read the same tail
-- (chain_position N) and both insert N+1; this unique index makes the second
-- insert fail instead of silently forking the chain.
CREATE UNIQUE INDEX IF NOT EXISTS uq_result_signatures_chain_position ON result_signatures(chain_position);
