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

    // Run migrations. R4-39c fix: the whole file cannot go through a single
    // sqlx::query call (PostgreSQL's extended query protocol rejects
    // multi-statement strings), and the previous `.ok()` silently discarded
    // every failure — a fresh database ended up with NO tables. Execute the
    // statements one by one and fail startup on the first error.
    for statement in include_str!("../migrations/001_biometric_tables.sql").split(';') {
        let statement = statement.trim();
        if statement.is_empty() {
            continue;
        }
        sqlx::query(statement).execute(&pool).await.map_err(|e| {
            tracing::error!(error = %e, "biometric vault migration failed");
            e
        })?;
    }

    Ok(pool)
}
