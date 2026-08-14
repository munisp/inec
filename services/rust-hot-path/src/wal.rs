//! Durable retry log for failed sink batches (R4-25).
//!
//! Previously a batch that failed on any sink was counted and **dropped** —
//! permanent per-sink divergence with no recovery path. This module appends
//! failed batches to an append-only JSONL file and replays them on startup.
//!
//! DELIVERY SEMANTICS (honest): replay is **at-least-once**, not
//! exactly-once. A sink may have applied part of a batch before the failure
//! was observed (e.g. TigerBeetle wrote a sub-batch, then the connection
//! dropped), in which case replay re-delivers those records. Callers must
//! rely on idempotency keys: TigerBeetle transfer IDs are deterministic
//! (`deterministic_id(tx.id)`), so ledger retries collapse; OpenSearch/Redis
//! writers must dedup on `Transaction.id`. A cross-sink transaction
//! coordinator is the real fix for exactly-once; this WAL only guarantees
//! failed batches are never silently lost.

use std::path::{Path, PathBuf};
use std::sync::Mutex;

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

use crate::pipeline::Transaction;

/// One failed batch: which sinks failed and the full payload to retry.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WalRecord {
    /// Unix seconds when the failure was recorded.
    pub ts: i64,
    /// Names of the sinks whose writes failed ("redis", "tigerbeetle", ...).
    pub failed_sinks: Vec<String>,
    pub transactions: Vec<Transaction>,
}

/// Append-only JSONL write-ahead log. Sync file I/O is acceptable here:
/// appends happen only on the failure path (rare, small) and the lock
/// serializes writers from the worker tasks.
pub struct WriteAheadLog {
    path: PathBuf,
    write_lock: Mutex<()>,
}

impl WriteAheadLog {
    /// Open (creating if needed) the WAL file `failed-batches.jsonl` in `dir`.
    pub fn open(dir: &Path) -> Result<Self> {
        std::fs::create_dir_all(dir)
            .with_context(|| format!("failed to create WAL dir {}", dir.display()))?;
        Ok(Self {
            path: dir.join("failed-batches.jsonl"),
            write_lock: Mutex::new(()),
        })
    }

    pub fn path(&self) -> &Path {
        &self.path
    }

    /// Append one failed batch. Returns Err on I/O failure — the caller logs
    /// it loudly; there is nowhere further to fall back to.
    pub fn append(&self, failed_sinks: &[&str], batch: &[Transaction]) -> Result<()> {
        use std::io::Write;
        let record = WalRecord {
            ts: std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0),
            failed_sinks: failed_sinks.iter().map(|s| s.to_string()).collect(),
            transactions: batch.to_vec(),
        };
        let line = serde_json::to_string(&record).context("WAL record serialize")?;
        let _guard = self.write_lock.lock().unwrap_or_else(|e| e.into_inner());
        let mut f = std::fs::OpenOptions::new()
            .create(true)
            .append(true)
            .open(&self.path)
            .with_context(|| format!("failed to open WAL {}", self.path.display()))?;
        f.write_all(line.as_bytes())?;
        f.write_all(b"\n")?;
        f.sync_all()?;
        Ok(())
    }

    /// Read every intact record. Malformed trailing lines (crash mid-append)
    /// are skipped with a warning rather than aborting replay.
    pub fn read_all(&self) -> Result<Vec<WalRecord>> {
        match std::fs::read_to_string(&self.path) {
            Ok(contents) => {
                let mut out = Vec::new();
                for (i, line) in contents.lines().enumerate() {
                    if line.trim().is_empty() {
                        continue;
                    }
                    match serde_json::from_str(line) {
                        Ok(rec) => out.push(rec),
                        Err(e) => {
                            tracing::warn!(
                                "skipping malformed WAL line {} in {}: {}",
                                i + 1,
                                self.path.display(),
                                e
                            );
                        }
                    }
                }
                Ok(out)
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(Vec::new()),
            Err(e) => Err(e).with_context(|| format!("failed to read WAL {}", self.path.display())),
        }
    }

    /// Atomically replace the WAL with only `records` (the batches that
    /// failed again during replay). Write-tmp-then-rename so a crash during
    /// rewrite never destroys un-replayed records.
    pub fn rewrite(&self, records: &[WalRecord]) -> Result<()> {
        use std::io::Write;
        let _guard = self.write_lock.lock().unwrap_or_else(|e| e.into_inner());
        let tmp = self.path.with_extension("jsonl.tmp");
        {
            let mut f = std::fs::File::create(&tmp)
                .with_context(|| format!("failed to create {}", tmp.display()))?;
            for rec in records {
                let line = serde_json::to_string(rec).context("WAL record serialize")?;
                f.write_all(line.as_bytes())?;
                f.write_all(b"\n")?;
            }
            f.sync_all()?;
        }
        std::fs::rename(&tmp, &self.path)
            .with_context(|| format!("failed to rotate WAL {}", self.path.display()))?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write as _;

    fn tx(id: &str) -> Transaction {
        Transaction {
            id: id.into(),
            tx_type: "ballot_cast".into(),
            source: "pu-001".into(),
            timestamp: 1_700_000_000,
            election_id: "elec-1".into(),
            state_code: "01".into(),
            lga_id: "lga-1".into(),
            ward_id: "ward-1".into(),
            pu_id: "pu-1".into(),
            amount: 5,
            hash: "h".into(),
            data: serde_json::Value::Null,
        }
    }

    fn scratch_dir(tag: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!(
            "wal-test-{}-{}-{}",
            tag,
            std::process::id(),
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap()
                .as_nanos()
        ));
        let _ = std::fs::remove_dir_all(&dir);
        dir
    }

    #[test]
    fn append_and_read_roundtrip() {
        let dir = scratch_dir("roundtrip");
        let wal = WriteAheadLog::open(&dir).unwrap();
        assert!(wal.read_all().unwrap().is_empty());

        wal.append(&["tigerbeetle"], &[tx("a"), tx("b")]).unwrap();
        wal.append(&["redis", "opensearch"], &[tx("c")]).unwrap();

        let records = wal.read_all().unwrap();
        assert_eq!(records.len(), 2);
        assert_eq!(records[0].failed_sinks, vec!["tigerbeetle".to_string()]);
        assert_eq!(records[0].transactions.len(), 2);
        assert_eq!(records[0].transactions[0].id, "a");
        assert_eq!(records[1].failed_sinks.len(), 2);
        assert_eq!(records[1].transactions[0].id, "c");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn rewrite_keeps_only_still_failing_records() {
        let dir = scratch_dir("rewrite");
        let wal = WriteAheadLog::open(&dir).unwrap();
        wal.append(&["redis"], &[tx("a")]).unwrap();
        wal.append(&["redis"], &[tx("b")]).unwrap();

        // Simulate replay: "a" succeeded on retry, "b" failed again.
        let mut again = wal.read_all().unwrap();
        again.retain(|r| r.transactions[0].id == "b");
        wal.rewrite(&again).unwrap();

        let remaining = wal.read_all().unwrap();
        assert_eq!(remaining.len(), 1);
        assert_eq!(remaining[0].transactions[0].id, "b");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn rewrite_to_empty_truncates_log() {
        let dir = scratch_dir("empty");
        let wal = WriteAheadLog::open(&dir).unwrap();
        wal.append(&["fluvio"], &[tx("a")]).unwrap();
        wal.rewrite(&[]).unwrap();
        assert!(wal.read_all().unwrap().is_empty());
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn malformed_line_is_skipped_not_fatal() {
        let dir = scratch_dir("malformed");
        let wal = WriteAheadLog::open(&dir).unwrap();
        wal.append(&["redis"], &[tx("a")]).unwrap();
        // Simulate a crash mid-append: partial JSON at the tail.
        std::fs::OpenOptions::new()
            .append(true)
            .open(wal.path())
            .unwrap()
            .write_all(b"{\"ts\": 12")
            .unwrap();
        let records = wal.read_all().unwrap();
        assert_eq!(records.len(), 1);
        assert_eq!(records[0].transactions[0].id, "a");
        let _ = std::fs::remove_dir_all(&dir);
    }
}
