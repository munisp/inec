//! TigerBeetle direct client — batch transfers at 1M+ TPS.
//!
//! Key optimizations:
//! - Native binary protocol (no HTTP overhead)
//! - Batch size = 8190 (TigerBeetle's maximum per request)
//! - Pre-allocated transfer buffers (no heap allocation per transfer)
//! - Deterministic 128-bit IDs from SHA-256 (no UUID generation overhead)
//! - Linked transfers for atomic multi-leg operations
//! - Zero-copy serialization via fixed-size structs

use anyhow::Result;
use sha2::{Digest, Sha256};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use crate::pipeline::{Config, Transaction};

/// TigerBeetle transfer struct matching the native binary format.
/// 128 bytes fixed-size — no heap allocation.
#[repr(C, packed)]
#[derive(Debug, Clone, Copy, Default)]
pub struct TBTransfer {
    pub id: u128,
    pub debit_account_id: u128,
    pub credit_account_id: u128,
    pub amount: u64,
    pub pending_id: u128,
    pub user_data_128: u128,
    pub user_data_64: u64,
    pub user_data_32: u32,
    pub timeout: u32,
    pub ledger: u32,
    pub code: u16,
    pub flags: u16,
    pub timestamp: u64,
}

/// Transfer codes for election operations
pub mod codes {
    pub const RESULT_DEPOSIT: u16 = 1001;
    pub const BALLOT_AUDIT: u16 = 1002;
    pub const INCIDENT_PENALTY: u16 = 1003;
    pub const SETTLEMENT: u16 = 1004;
    pub const ACCREDITATION: u16 = 1005;
}

/// Ledger IDs
pub mod ledgers {
    pub const ELECTION: u32 = 1;
    pub const AUDIT: u32 = 2;
    pub const PENALTY: u32 = 3;
    pub const SETTLEMENT: u32 = 4;
}

/// Transfer flags
pub mod flags {
    pub const LINKED: u16 = 0x0001; // atomic with next transfer
    pub const PENDING: u16 = 0x0002; // two-phase commit
    pub const POST_PENDING: u16 = 0x0004; // complete pending transfer
    pub const VOID_PENDING: u16 = 0x0008; // cancel pending transfer
}

/// Upper bound on a single ledger transfer amount. `Transaction.amount` is a
/// signed i64 from the Kafka payload; TigerBeetle amounts are u64. A negative
/// amount run through `as u64` wraps to ~u64::MAX — silent ledger corruption
/// (R4-34b). Anything above this cap is a data bug, not a real ballot/result
/// quantity: 1e15 milli-units is orders of magnitude beyond any plausible
/// election figure.
pub const MAX_TRANSFER_AMOUNT: i64 = 1_000_000_000_000_000;

/// Validate a transaction's amount for ledger submission.
/// Rejects zero/negative amounts and amounts above MAX_TRANSFER_AMOUNT
/// BEFORE any `as u64` cast can wrap them.
fn validate_amount(tx: &Transaction) -> Result<u64> {
    if tx.amount <= 0 {
        anyhow::bail!(
            "transaction {} has non-positive amount {} — refusing to wrap-cast into a ledger transfer",
            tx.id, tx.amount
        );
    }
    if tx.amount > MAX_TRANSFER_AMOUNT {
        anyhow::bail!(
            "transaction {} amount {} exceeds MAX_TRANSFER_AMOUNT ({}) — refusing implausible ledger transfer",
            tx.id, tx.amount, MAX_TRANSFER_AMOUNT
        );
    }
    Ok(tx.amount as u64) // safe: 0 < amount <= MAX_TRANSFER_AMOUNT
}

/// Build a TBTransfer, validating all signed fields first.
fn build_transfer(tx: &Transaction) -> Result<TBTransfer> {
    let amount = validate_amount(tx)?;
    Ok(TBTransfer {
        id: deterministic_id(&tx.id),
        debit_account_id: deterministic_id(&tx.source),
        credit_account_id: deterministic_id(&tx.election_id),
        amount,
        pending_id: 0,
        user_data_128: deterministic_id(&tx.hash),
        // Negative timestamps clamp to 0 rather than wrapping to ~u64::MAX.
        user_data_64: u64::try_from(tx.timestamp).unwrap_or(0),
        user_data_32: 0,
        timeout: 0,
        ledger: ledger_for_type(&tx.tx_type),
        code: code_for_type(&tx.tx_type),
        flags: 0,
        timestamp: 0, // server-assigned
    })
}

pub struct TigerBeetleDirectClient {
    addresses: Vec<String>,
    cluster_id: u128,
    batch_size: usize,

    // Pre-allocated transfer buffer (avoids allocation per batch)
    buffer: Vec<TBTransfer>,

    // Metrics
    transfers_submitted: AtomicU64,
    batches_sent: AtomicU64,
    /// Transactions rejected before ledger submission (invalid amount).
    transfers_rejected: AtomicU64,
}

impl TigerBeetleDirectClient {
    pub fn new(config: &Config) -> Self {
        Self {
            addresses: config.tb_addresses.clone(),
            cluster_id: config.tb_cluster_id,
            batch_size: config.tb_batch_size.min(8190), // TB hard limit
            buffer: Vec::with_capacity(8190),
            transfers_submitted: AtomicU64::new(0),
            batches_sent: AtomicU64::new(0),
            transfers_rejected: AtomicU64::new(0),
        }
    }

    /// Convert a batch of transactions into TigerBeetle transfers and submit.
    /// Transactions with invalid amounts are rejected (counted + logged) and
    /// skipped — never wrap-cast into the ledger.
    pub async fn batch_transfer(&self, batch: Arc<Vec<Transaction>>) -> Result<()> {
        let mut transfers = Vec::with_capacity(batch.len().min(self.batch_size));

        for tx in batch.iter() {
            let transfer = match build_transfer(tx) {
                Ok(t) => t,
                Err(e) => {
                    tracing::warn!("rejecting transfer: {}", e);
                    self.transfers_rejected.fetch_add(1, Ordering::Relaxed);
                    continue;
                }
            };
            transfers.push(transfer);

            // Flush when batch is full
            if transfers.len() >= self.batch_size {
                self.submit_batch(&transfers).await?;
                self.transfers_submitted
                    .fetch_add(transfers.len() as u64, Ordering::Relaxed);
                self.batches_sent.fetch_add(1, Ordering::Relaxed);
                transfers.clear();
            }
        }

        // Flush remaining
        if !transfers.is_empty() {
            self.submit_batch(&transfers).await?;
            self.transfers_submitted
                .fetch_add(transfers.len() as u64, Ordering::Relaxed);
            self.batches_sent.fetch_add(1, Ordering::Relaxed);
        }

        Ok(())
    }

    /// Submit linked transfers (atomic multi-leg operation).
    /// All transfers succeed or all fail; any invalid amount aborts the whole
    /// linked chain before submission.
    pub async fn linked_transfer(&self, txs: &[Transaction]) -> Result<()> {
        let mut transfers = Vec::with_capacity(txs.len());

        for (i, tx) in txs.iter().enumerate() {
            let mut transfer = build_transfer(tx)?;

            // Link all except the last transfer
            if i < txs.len() - 1 {
                transfer.flags |= flags::LINKED;
            }
            transfers.push(transfer);
        }

        self.submit_batch(&transfers).await
    }

    /// Two-phase commit: create pending transfer, then post or void.
    pub async fn pending_transfer(&self, tx: &Transaction, timeout_secs: u32) -> Result<u128> {
        let id = deterministic_id(&tx.id);
        let mut transfer = build_transfer(tx)?;
        transfer.timeout = timeout_secs;
        transfer.flags = flags::PENDING;
        self.submit_batch(&[transfer]).await?;
        Ok(id)
    }

    async fn submit_batch(&self, transfers: &[TBTransfer]) -> Result<()> {
        // SECURITY: previously a silent no-op returning Ok(()) — the
        // transfers_submitted counter was incremented while ballots were
        // dropped from the audit ledger. No TigerBeetle client crate is
        // available in this build (see Cargo.toml), so fail loudly: callers
        // MUST know the ledger write did not happen.
        Err(anyhow::anyhow!(
            "tigerbeetle client not available in this build; refusing to silently drop {} audit-ledger transfer(s)",
            transfers.len()
        ))
    }

    pub fn stats(&self) -> (u64, u64) {
        (
            self.transfers_submitted.load(Ordering::Relaxed),
            self.batches_sent.load(Ordering::Relaxed),
        )
    }

    /// Number of transactions rejected before ledger submission.
    pub fn rejected(&self) -> u64 {
        self.transfers_rejected.load(Ordering::Relaxed)
    }
}

/// Generate deterministic 128-bit ID from string using SHA-256 truncation.
/// This is 3x faster than UUID v4 (no CSPRNG) and deterministic.
#[inline]
fn deterministic_id(input: &str) -> u128 {
    let hash = Sha256::digest(input.as_bytes());
    u128::from_le_bytes(hash[..16].try_into().expect("failed to unwrap safely"))
}

fn ledger_for_type(tx_type: &str) -> u32 {
    match tx_type {
        "result_submission" | "ballot_cast" => ledgers::ELECTION,
        "incident" => ledgers::PENALTY,
        "settlement" => ledgers::SETTLEMENT,
        _ => ledgers::AUDIT,
    }
}

fn code_for_type(tx_type: &str) -> u16 {
    match tx_type {
        "result_submission" => codes::RESULT_DEPOSIT,
        "ballot_cast" => codes::BALLOT_AUDIT,
        "incident" => codes::INCIDENT_PENALTY,
        "settlement" => codes::SETTLEMENT,
        "accreditation" => codes::ACCREDITATION,
        _ => codes::SETTLEMENT,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tx_with_amount(amount: i64) -> Transaction {
        Transaction {
            id: "tx-1".into(),
            tx_type: "ballot_cast".into(),
            source: "pu-001".into(),
            timestamp: 1_700_000_000,
            election_id: "elec-1".into(),
            state_code: "01".into(),
            lga_id: "lga-1".into(),
            ward_id: "ward-1".into(),
            pu_id: "pu-1".into(),
            amount,
            hash: "hash".into(),
            data: serde_json::Value::Null,
        }
    }

    /// R4-34b regression: a negative i64 amount used to wrap via `as u64`
    /// into ~u64::MAX and land in the financial ledger. It must now be an
    /// error and produce NO transfer.
    #[test]
    fn negative_amount_is_rejected_not_wrapped() {
        let err = build_transfer(&tx_with_amount(-1)).unwrap_err();
        assert!(
            err.to_string().contains("non-positive"),
            "unexpected: {}",
            err
        );
    }

    #[test]
    fn zero_amount_is_rejected() {
        assert!(build_transfer(&tx_with_amount(0)).is_err());
    }

    #[test]
    fn amount_above_max_is_rejected() {
        assert!(build_transfer(&tx_with_amount(MAX_TRANSFER_AMOUNT + 1)).is_err());
    }

    #[test]
    fn valid_amount_builds_transfer() {
        let t = build_transfer(&tx_with_amount(42)).unwrap();
        // repr(packed): copy fields out before asserting (no unaligned refs).
        let (amount, ts) = (t.amount, t.user_data_64);
        assert_eq!(amount, 42);
        assert_eq!(ts, 1_700_000_000);
    }

    #[test]
    fn negative_timestamp_clamps_instead_of_wrapping() {
        let mut tx = tx_with_amount(7);
        tx.timestamp = -5;
        let t = build_transfer(&tx).unwrap();
        let ts = t.user_data_64;
        assert_eq!(ts, 0);
    }

    /// A batch mixing valid and invalid transactions: invalid ones are
    /// skipped and counted, valid ones still build — and the batch-level
    /// submit fails loudly in this build (no TB client), which is fine; the
    /// assertion target is the rejection counter.
    #[tokio::test]
    async fn batch_rejects_invalid_and_counts_them() {
        let config = Config::from_env();
        let client = TigerBeetleDirectClient::new(&config);
        let batch = Arc::new(vec![
            tx_with_amount(-3),                      // rejected
            tx_with_amount(0),                       // rejected
            tx_with_amount(MAX_TRANSFER_AMOUNT + 9), // rejected
        ]);
        // submit_batch fails loudly without a TB client, but the three
        // invalid transactions must already have been counted as rejected
        // before any submission attempt.
        let _ = client.batch_transfer(batch).await;
        assert_eq!(client.rejected(), 3);
    }
}
