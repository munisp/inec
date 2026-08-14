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

    // Run the embedded migration. `sqlx::query` uses the extended protocol and
    // silently executes ONLY the first statement of a multi-statement file —
    // that is why the tables previously "applied" but never actually existed.
    // `raw_sql` uses the simple protocol (multi-statement capable). 001 is the
    // single source of truth: it defines every table the code queries, all
    // with IF NOT EXISTS. Any failure aborts startup — a biometric service
    // without its vault tables must fail loudly, never serve 500s at runtime.
    sqlx::raw_sql(include_str!("../migrations/001_biometric_tables.sql"))
        .execute(&pool)
        .await
        .map_err(|e| {
            tracing::error!(error = %e, "biometric vault migration failed");
            e
        })?;

    Ok(pool)
}
