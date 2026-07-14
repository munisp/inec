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

    // Run migrations
    sqlx::query(include_str!("../migrations/001_biometric_tables.sql"))
        .execute(&pool)
        .await
        .ok(); // Ignore if tables already exist

    Ok(pool)
}
