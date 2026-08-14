//! PostgreSQL connection pool and database operations.
//!
//! All biometric vault state is persisted to PostgreSQL — no in-memory HashMaps.

use sqlx::postgres::{PgPool, PgPoolOptions};
use std::time::Duration;

/// Initialize the PostgreSQL connection pool.
pub async fn init_pool(database_url: &str) -> Result<PgPool, sqlx::Error> {
    let pool = PgPoolOptions::new()
        .max_connections(50)
        .min_connections(5)
        .acquire_timeout(Duration::from_secs(10))
        .idle_timeout(Duration::from_secs(300))
        .connect(database_url)
        .await?;

    // Run embedded migrations. `sqlx::query` uses the extended protocol and
    // silently executes ONLY the first statement of a multi-statement file —
    // that is why 001's tables "applied" but the vault tables never existed.
    // `raw_sql` uses the simple protocol (multi-statement capable). Failures
    // are logged, not discarded: idempotent DDL failing on "already exists"
    // is fine, silence is not.
    for (name, sql) in [
        ("001_biometric_tables.sql", include_str!("../migrations/001_biometric_tables.sql")),
        ("002_vault_tables.sql", include_str!("../migrations/002_vault_tables.sql")),
    ] {
        if let Err(e) = sqlx::raw_sql(sql).execute(&pool).await {
            tracing::warn!("embedded migration {} did not apply cleanly (objects may already exist): {}", name, e);
        }
    }

    Ok(pool)
}
