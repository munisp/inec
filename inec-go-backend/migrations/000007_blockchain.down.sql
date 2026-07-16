-- Rollback: Blockchain: Fabric, IPFS, merkle trees, smart contracts

DROP TABLE IF EXISTS smart_contracts CASCADE;
DROP TABLE IF EXISTS merkle_trees CASCADE;
DROP TABLE IF EXISTS ipfs_pins CASCADE;
DROP TABLE IF EXISTS ipfs_objects CASCADE;
DROP TABLE IF EXISTS ipfs_dag_nodes CASCADE;
DROP TABLE IF EXISTS fabric_transactions CASCADE;
DROP TABLE IF EXISTS fabric_state_db CASCADE;
DROP TABLE IF EXISTS fabric_signing_keys CASCADE;
DROP TABLE IF EXISTS fabric_peers CASCADE;
DROP TABLE IF EXISTS fabric_orderers CASCADE;
DROP TABLE IF EXISTS fabric_endorsement_log CASCADE;
DROP TABLE IF EXISTS fabric_chaincode CASCADE;
DROP TABLE IF EXISTS fabric_blocks CASCADE;
DROP TABLE IF EXISTS chaincode_events CASCADE;
DROP TABLE IF EXISTS blockchain_results CASCADE;
DROP TABLE IF EXISTS blockchain_audit_trail CASCADE;

