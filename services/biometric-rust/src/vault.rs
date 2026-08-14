//! Production biometric template vault with AES-256-GCM encryption.
//!
//! All state persisted to PostgreSQL — zero in-memory storage.
//! - AES-256-GCM authenticated encryption for template storage
//! - HKDF-SHA256 key derivation
//! - Key rotation with versioning
//! - Audit trail for every vault operation
//! - Zeroize on drop for all sensitive data

use aes_gcm::{
    aead::{Aead, KeyInit, OsRng},
    Aes256Gcm, Nonce,
};
use chrono::{DateTime, Utc};
use hkdf::Hkdf;
use rand::RngCore;
use serde::{Deserialize, Serialize};
use sha2::Sha256;
use sqlx::PgPool;
use uuid::Uuid;
use zeroize::Zeroize;

#[derive(Debug, thiserror::Error)]
pub enum VaultError {
    #[error("encryption failed: {0}")]
    EncryptionFailed(String),
    #[error("decryption failed: {0}")]
    DecryptionFailed(String),
    #[error("key not found: {0}")]
    KeyNotFound(String),
    #[error("key revoked: {0}")]
    KeyRevoked(String),
    #[error("template not found: {0}")]
    TemplateNotFound(String),
    #[error("integrity check failed")]
    IntegrityCheckFailed,
    #[error("database error: {0}")]
    DatabaseError(String),
    #[error("vault configuration error: {0}")]
    ConfigurationError(String),
}

impl From<sqlx::Error> for VaultError {
    fn from(e: sqlx::Error) -> Self {
        VaultError::DatabaseError(e.to_string())
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub enum KeyPurpose {
    TemplateEncryption,
    Signing,
    KeyWrapping,
}

impl KeyPurpose {
    fn as_str(&self) -> &'static str {
        match self {
            KeyPurpose::TemplateEncryption => "template_encryption",
            KeyPurpose::Signing => "integrity_hmac",
            KeyPurpose::KeyWrapping => "key_wrapping",
        }
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub struct VaultKey {
    pub key_id: String,
    pub purpose: KeyPurpose,
    pub is_active: bool,
    pub is_revoked: bool,
    pub key_version: i32,
    pub created_at: DateTime<Utc>,
    pub rotated_at: Option<DateTime<Utc>>,
    #[serde(skip)]
    key_material: Vec<u8>,
}

impl Drop for VaultKey {
    fn drop(&mut self) {
        self.key_material.zeroize();
    }
}

#[derive(Clone, Serialize, Deserialize)]
pub struct EncryptedTemplate {
    pub template_id: String,
    pub voter_vin: String,
    pub modality: String,
    pub key_id: String,
    pub ciphertext: Vec<u8>,
    pub nonce: [u8; 12],
    pub integrity_hash: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Clone, Serialize, Deserialize)]
pub struct AuditEntry {
    pub id: String,
    pub operation: String,
    pub key_id: Option<String>,
    pub voter_vin: Option<String>,
    pub modality: Option<String>,
    pub actor: String,
    pub success: bool,
    pub error_detail: Option<String>,
    pub timestamp: DateTime<Utc>,
}

/// Biometric vault — all state in PostgreSQL, no in-memory storage.
pub struct BiometricVault {
    pool: PgPool,
    master_key: [u8; 32],
}

impl BiometricVault {
    /// Create vault backed by PostgreSQL.
    ///
    /// SECURITY: previously the master key was freshly random on EVERY start,
    /// which silently made every previously wrapped key (and therefore every
    /// stored biometric template) permanently undecryptable after a restart.
    /// The master key is now loaded from the BIOMETRIC_MASTER_KEY environment
    /// variable (hex-encoded 32 bytes). A random ephemeral key is only
    /// allowed in explicit dev environments; otherwise startup is refused.
    pub async fn new(pool: PgPool) -> Result<Self, VaultError> {
        let master_key = Self::load_master_key()?;
        Self::init(pool, master_key).await
    }

    /// Load the vault master key from configuration.
    fn load_master_key() -> Result<[u8; 32], VaultError> {
        match std::env::var("BIOMETRIC_MASTER_KEY") {
            Ok(hex_key) => {
                let bytes = hex::decode(hex_key.trim()).map_err(|e| {
                    VaultError::ConfigurationError(format!(
                        "BIOMETRIC_MASTER_KEY is not valid hex: {}",
                        e
                    ))
                })?;
                let key: [u8; 32] = bytes.try_into().map_err(|b: Vec<u8>| {
                    VaultError::ConfigurationError(format!(
                        "BIOMETRIC_MASTER_KEY must decode to exactly 32 bytes, got {}",
                        b.len()
                    ))
                })?;
                Ok(key)
            }
            Err(_) => {
                let env = std::env::var("APP_ENV")
                    .or_else(|_| std::env::var("ENVIRONMENT"))
                    .unwrap_or_default()
                    .to_lowercase();
                if matches!(env.as_str(), "dev" | "development" | "local" | "test") {
                    // Dev-only convenience: ephemeral key, loudly flagged.
                    tracing::warn!(
                        "SECURITY WARNING: BIOMETRIC_MASTER_KEY is not set — generating an \
                         EPHEMERAL random master key. All keys wrapped in previous runs are now \
                         PERMANENTLY UNDECRYPTABLE. Never run production without BIOMETRIC_MASTER_KEY."
                    );
                    let mut master_key = [0u8; 32];
                    OsRng.fill_bytes(&mut master_key);
                    Ok(master_key)
                } else {
                    Err(VaultError::ConfigurationError(
                        "BIOMETRIC_MASTER_KEY is not set and APP_ENV/ENVIRONMENT is not a dev \
                         environment; refusing to start with an ephemeral master key because it \
                         would permanently destroy access to previously wrapped keys"
                            .to_string(),
                    ))
                }
            }
        }
    }

    /// Shared initialization for `new` and `from_master_key`.
    async fn init(pool: PgPool, master_key: [u8; 32]) -> Result<Self, VaultError> {
        let vault = Self { pool, master_key };

        // Generate initial encryption key if none exists
        let count: (i64,) = sqlx::query_as(
            "SELECT COUNT(*) FROM vault_keys WHERE purpose = 'template_encryption' AND is_active = TRUE AND is_revoked = FALSE"
        )
            .fetch_one(&vault.pool)
            .await?;

        if count.0 == 0 {
            vault
                .generate_key(KeyPurpose::TemplateEncryption, "system")
                .await?;
        }

        Ok(vault)
    }

    /// Create vault with a specific master key (for deterministic testing/recovery).
    pub async fn from_master_key(pool: PgPool, master_key: [u8; 32]) -> Result<Self, VaultError> {
        Self::init(pool, master_key).await
    }

    /// Generate a new encryption key and persist to PostgreSQL.
    pub async fn generate_key(
        &self,
        purpose: KeyPurpose,
        actor: &str,
    ) -> Result<String, VaultError> {
        let key_id = format!("key-{}", Uuid::new_v4());

        // Derive key material using HKDF
        let mut salt = [0u8; 32];
        OsRng.fill_bytes(&mut salt);

        let hk = Hkdf::<Sha256>::new(Some(&salt), &self.master_key);
        let mut key_material = vec![0u8; 32];
        hk.expand(key_id.as_bytes(), &mut key_material)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        // Encrypt key material with master key before persisting
        let encrypted_key = self.wrap_key_material(&key_material)?;

        sqlx::query(
            "INSERT INTO vault_keys (key_id, purpose, encrypted_key, key_version, is_active, is_revoked)
             VALUES ($1, $2, $3, 1, TRUE, FALSE)"
        )
            .bind(&key_id)
            .bind(purpose.as_str())
            .bind(&encrypted_key)
            .execute(&self.pool)
            .await?;

        key_material.zeroize();

        self.log_audit("generate_key", Some(&key_id), None, None, actor, true, None)
            .await;
        Ok(key_id)
    }

    /// Encrypt a biometric template and persist to PostgreSQL.
    pub async fn encrypt_template(
        &self,
        voter_vin: &str,
        modality: &str,
        template_data: &[u8],
        actor: &str,
    ) -> Result<EncryptedTemplate, VaultError> {
        let key = self.get_active_key(KeyPurpose::TemplateEncryption).await?;

        let cipher = Aes256Gcm::new_from_slice(&key.key_material)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        let mut nonce_bytes = [0u8; 12];
        OsRng.fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);

        // AAD includes voter VIN and modality for binding
        let aad = format!("{}:{}", voter_vin, modality).into_bytes();

        let ciphertext = cipher
            .encrypt(
                nonce,
                aes_gcm::aead::Payload {
                    msg: template_data,
                    aad: &aad,
                },
            )
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        // HMAC over ciphertext for integrity
        let integrity_hash = self.compute_hmac_hex(&key.key_material, &ciphertext);

        let template_id = format!("tmpl-{}", Uuid::new_v4());

        // Persist to PostgreSQL
        sqlx::query(
            "INSERT INTO vault_templates (template_id, voter_vin, modality, key_id, ciphertext, nonce, integrity_hash, version)
             VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
             ON CONFLICT (template_id) DO UPDATE SET
                ciphertext = EXCLUDED.ciphertext,
                nonce = EXCLUDED.nonce,
                key_id = EXCLUDED.key_id,
                integrity_hash = EXCLUDED.integrity_hash,
                version = vault_templates.version + 1,
                updated_at = NOW()"
        )
            .bind(&template_id)
            .bind(voter_vin)
            .bind(modality)
            .bind(&key.key_id)
            .bind(&ciphertext)
            .bind(&nonce_bytes[..])
            .bind(&integrity_hash)
            .execute(&self.pool)
            .await?;

        let encrypted = EncryptedTemplate {
            template_id: template_id.clone(),
            voter_vin: voter_vin.to_string(),
            modality: modality.to_string(),
            key_id: key.key_id.clone(),
            ciphertext,
            nonce: nonce_bytes,
            integrity_hash,
            created_at: Utc::now(),
        };

        self.log_audit(
            "encrypt_template",
            Some(&key.key_id),
            Some(voter_vin),
            Some(modality),
            actor,
            true,
            None,
        )
        .await;

        Ok(encrypted)
    }

    /// Decrypt a template by loading from PostgreSQL.
    pub async fn decrypt_template(
        &self,
        template_id: &str,
        actor: &str,
    ) -> Result<Vec<u8>, VaultError> {
        // Load template from PostgreSQL
        let row = sqlx::query_as::<_, (String, String, String, Vec<u8>, Vec<u8>, String)>(
            "SELECT voter_vin, modality, key_id, ciphertext, nonce, integrity_hash
             FROM vault_templates WHERE template_id = $1",
        )
        .bind(template_id)
        .fetch_optional(&self.pool)
        .await?
        .ok_or_else(|| VaultError::TemplateNotFound(template_id.to_string()))?;

        let (voter_vin, modality, key_id, ciphertext, nonce_vec, integrity_hash) = row;

        // Load key from PostgreSQL
        let key = self.load_key(&key_id).await?;

        if key.is_revoked {
            self.log_audit(
                "decrypt_template",
                Some(&key_id),
                Some(&voter_vin),
                Some(&modality),
                actor,
                false,
                Some("key_revoked"),
            )
            .await;
            return Err(VaultError::KeyRevoked(key_id));
        }

        // Verify integrity
        let expected_hmac = self.compute_hmac_hex(&key.key_material, &ciphertext);
        if expected_hmac != integrity_hash {
            self.log_audit(
                "decrypt_template",
                Some(&key_id),
                Some(&voter_vin),
                Some(&modality),
                actor,
                false,
                Some("integrity_check_failed"),
            )
            .await;
            return Err(VaultError::IntegrityCheckFailed);
        }

        let cipher = Aes256Gcm::new_from_slice(&key.key_material)
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        let nonce_arr: [u8; 12] = nonce_vec
            .try_into()
            .map_err(|_| VaultError::DecryptionFailed("invalid nonce length".to_string()))?;
        let nonce = Nonce::from_slice(&nonce_arr);
        let aad = format!("{}:{}", voter_vin, modality).into_bytes();

        let plaintext = cipher
            .decrypt(
                nonce,
                aes_gcm::aead::Payload {
                    msg: &ciphertext,
                    aad: &aad,
                },
            )
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        self.log_audit(
            "decrypt_template",
            Some(&key_id),
            Some(&voter_vin),
            Some(&modality),
            actor,
            true,
            None,
        )
        .await;

        Ok(plaintext)
    }

    /// Rotate key: generate new key, re-encrypt all templates using old key.
    ///
    /// Each template is re-encrypted inside its own transaction so a crash
    /// mid-rotation leaves every template either fully on the old key or fully
    /// on the new key — never corrupted. The scan is paged (templates still on
    /// the old key drop out of the result set as they are rotated), the vault
    /// never holds every template ID in memory at once, and re-running the
    /// rotation after a failure simply resumes with the remaining templates.
    pub async fn rotate_key(&self, key_id: &str, actor: &str) -> Result<String, VaultError> {
        let old_key = self.load_key(key_id).await?;
        if old_key.is_revoked {
            return Err(VaultError::KeyRevoked(key_id.to_string()));
        }

        let new_key_id = self
            .generate_key(KeyPurpose::TemplateEncryption, actor)
            .await?;
        let new_key = self.load_key(&new_key_id).await?;

        // Mark old key as rotated
        sqlx::query(
            "UPDATE vault_keys SET is_active = FALSE, rotated_at = NOW(), key_version = key_version + 1
             WHERE key_id = $1"
        )
            .bind(key_id)
            .execute(&self.pool)
            .await?;

        const PAGE_SIZE: i64 = 500;
        let mut processed: i64 = 0;

        loop {
            // Paged scan: rotated rows no longer match key_id, so the first
            // page always contains the next un-rotated templates.
            let page: Vec<(String, String, String, Vec<u8>, Vec<u8>, String)> = sqlx::query_as(
                "SELECT template_id, voter_vin, modality, ciphertext, nonce, integrity_hash
                 FROM vault_templates WHERE key_id = $1
                 ORDER BY template_id LIMIT $2",
            )
            .bind(key_id)
            .bind(PAGE_SIZE)
            .fetch_all(&self.pool)
            .await?;

            if page.is_empty() {
                break;
            }

            for (tid, voter_vin, modality, ciphertext, nonce_vec, integrity_hash) in &page {
                // Per-template transaction: decrypt with the old key, re-encrypt
                // with the new key, and persist atomically.
                let mut tx = self.pool.begin().await?;

                // Lock the row and skip if a concurrent rotation already moved it.
                let current: Option<(String,)> = sqlx::query_as(
                    "SELECT key_id FROM vault_templates WHERE template_id = $1 FOR UPDATE",
                )
                .bind(tid)
                .fetch_optional(&mut *tx)
                .await?;
                match current {
                    Some((current_key,)) if current_key == key_id => {}
                    _ => {
                        tx.rollback().await?;
                        continue;
                    }
                }

                let plaintext = self.decrypt_with_key(
                    &old_key,
                    voter_vin,
                    modality,
                    ciphertext,
                    nonce_vec,
                    integrity_hash,
                )?;

                let cipher = Aes256Gcm::new_from_slice(&new_key.key_material)
                    .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

                let mut nonce_bytes = [0u8; 12];
                OsRng.fill_bytes(&mut nonce_bytes);
                let nonce = Nonce::from_slice(&nonce_bytes);
                let aad = format!("{}:{}", voter_vin, modality).into_bytes();

                let new_ciphertext = cipher
                    .encrypt(
                        nonce,
                        aes_gcm::aead::Payload {
                            msg: &plaintext,
                            aad: &aad,
                        },
                    )
                    .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

                let new_integrity = self.compute_hmac_hex(&new_key.key_material, &new_ciphertext);

                sqlx::query(
                    "UPDATE vault_templates SET key_id = $1, ciphertext = $2, nonce = $3, integrity_hash = $4, version = version + 1, updated_at = NOW()
                     WHERE template_id = $5"
                )
                    .bind(&new_key_id)
                    .bind(&new_ciphertext)
                    .bind(&nonce_bytes[..])
                    .bind(&new_integrity)
                    .bind(tid)
                    .execute(&mut *tx)
                    .await?;

                tx.commit().await?;
                processed += 1;
            }

            // Audit progress after each page so long rotations are observable.
            self.log_audit(
                "rotate_key_progress",
                Some(key_id),
                None,
                None,
                actor,
                true,
                Some(&format!("templates_processed={}", processed)),
            )
            .await;
        }

        self.log_audit(
            "rotate_key",
            Some(key_id),
            None,
            None,
            actor,
            true,
            Some(&format!(
                "new_key_id={} templates_processed={}",
                new_key_id, processed
            )),
        )
        .await;
        Ok(new_key_id)
    }

    /// Verify integrity and decrypt template material with a specific key.
    /// Used by key rotation, which loads rows directly inside a transaction.
    fn decrypt_with_key(
        &self,
        key: &VaultKey,
        voter_vin: &str,
        modality: &str,
        ciphertext: &[u8],
        nonce_vec: &[u8],
        integrity_hash: &str,
    ) -> Result<Vec<u8>, VaultError> {
        let expected_hmac = self.compute_hmac_hex(&key.key_material, ciphertext);
        if expected_hmac != integrity_hash {
            return Err(VaultError::IntegrityCheckFailed);
        }

        let cipher = Aes256Gcm::new_from_slice(&key.key_material)
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        let nonce_arr: [u8; 12] = nonce_vec
            .try_into()
            .map_err(|_| VaultError::DecryptionFailed("invalid nonce length".to_string()))?;
        let nonce = Nonce::from_slice(&nonce_arr);
        let aad = format!("{}:{}", voter_vin, modality).into_bytes();

        cipher
            .decrypt(
                nonce,
                aes_gcm::aead::Payload {
                    msg: ciphertext,
                    aad: &aad,
                },
            )
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))
    }

    /// Revoke a key — templates encrypted with this key can no longer be decrypted.
    pub async fn revoke_key(&self, key_id: &str, actor: &str) -> Result<(), VaultError> {
        sqlx::query(
            "UPDATE vault_keys SET is_revoked = TRUE, is_active = FALSE, revoked_at = NOW() WHERE key_id = $1"
        )
            .bind(key_id)
            .execute(&self.pool)
            .await?;

        self.log_audit("revoke_key", Some(key_id), None, None, actor, true, None)
            .await;
        Ok(())
    }

    /// Probe PostgreSQL reachability for health/readiness checks.
    pub async fn health_check(&self) -> Result<(), VaultError> {
        sqlx::query("SELECT 1").execute(&self.pool).await?;
        Ok(())
    }

    /// Get vault statistics from PostgreSQL.
    pub async fn get_stats(&self) -> Result<serde_json::Value, VaultError> {
        let key_stats = sqlx::query_as::<_, (i64, i64, i64, i64)>(
            "SELECT
                COUNT(*),
                COUNT(*) FILTER (WHERE is_active = TRUE AND is_revoked = FALSE),
                COUNT(*) FILTER (WHERE is_active = FALSE AND is_revoked = FALSE),
                COUNT(*) FILTER (WHERE is_revoked = TRUE)
             FROM vault_keys",
        )
        .fetch_one(&self.pool)
        .await?;

        let template_count: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM vault_templates")
            .fetch_one(&self.pool)
            .await?;

        let audit_count: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM vault_audit_log")
            .fetch_one(&self.pool)
            .await?;

        Ok(serde_json::json!({
            "total_keys": key_stats.0,
            "active_keys": key_stats.1,
            "rotated_keys": key_stats.2,
            "revoked_keys": key_stats.3,
            "total_templates": template_count.0,
            "total_audit_entries": audit_count.0,
            "encryption_algorithm": "AES-256-GCM",
            "key_derivation": "HKDF-SHA256",
            "persistence": "postgresql",
        }))
    }

    /// Get recent audit entries from PostgreSQL.
    pub async fn get_audit_log(&self, limit: i64) -> Result<Vec<AuditEntry>, VaultError> {
        let rows = sqlx::query_as::<_, (String, String, Option<String>, Option<String>, Option<String>, String, bool, Option<String>, DateTime<Utc>)>(
            "SELECT id, operation, key_id, voter_vin, modality, actor, success, error_detail, created_at
             FROM vault_audit_log ORDER BY created_at DESC LIMIT $1"
        )
            .bind(limit)
            .fetch_all(&self.pool)
            .await?;

        Ok(rows
            .into_iter()
            .map(|r| AuditEntry {
                id: r.0,
                operation: r.1,
                key_id: r.2,
                voter_vin: r.3,
                modality: r.4,
                actor: r.5,
                success: r.6,
                error_detail: r.7,
                timestamp: r.8,
            })
            .collect())
    }

    // ─── Private helpers ────────────────────────────────────────

    async fn get_active_key(&self, purpose: KeyPurpose) -> Result<VaultKey, VaultError> {
        let row = sqlx::query_as::<_, (String, Vec<u8>, i32, DateTime<Utc>)>(
            "SELECT key_id, encrypted_key, key_version, created_at
             FROM vault_keys
             WHERE purpose = $1 AND is_active = TRUE AND is_revoked = FALSE
             ORDER BY created_at DESC LIMIT 1",
        )
        .bind(purpose.as_str())
        .fetch_optional(&self.pool)
        .await?
        .ok_or_else(|| {
            VaultError::KeyNotFound(format!("no active key for {}", purpose.as_str()))
        })?;

        let key_material = self.unwrap_key_material(&row.1)?;

        Ok(VaultKey {
            key_id: row.0,
            purpose,
            is_active: true,
            is_revoked: false,
            key_version: row.2,
            created_at: row.3,
            rotated_at: None,
            key_material,
        })
    }

    async fn load_key(&self, key_id: &str) -> Result<VaultKey, VaultError> {
        let row = sqlx::query_as::<_, (String, Vec<u8>, i32, bool, bool, DateTime<Utc>, Option<DateTime<Utc>>)>(
            "SELECT purpose, encrypted_key, key_version, is_active, is_revoked, created_at, rotated_at
             FROM vault_keys WHERE key_id = $1"
        )
            .bind(key_id)
            .fetch_optional(&self.pool)
            .await?
            .ok_or_else(|| VaultError::KeyNotFound(key_id.to_string()))?;

        let purpose = match row.0.as_str() {
            "template_encryption" => KeyPurpose::TemplateEncryption,
            "integrity_hmac" => KeyPurpose::Signing,
            "key_wrapping" => KeyPurpose::KeyWrapping,
            _ => KeyPurpose::TemplateEncryption,
        };

        let key_material = self.unwrap_key_material(&row.1)?;

        Ok(VaultKey {
            key_id: key_id.to_string(),
            purpose,
            is_active: row.3,
            is_revoked: row.4,
            key_version: row.2,
            created_at: row.5,
            rotated_at: row.6,
            key_material,
        })
    }

    /// Wrap key material with master key for safe storage.
    fn wrap_key_material(&self, key_material: &[u8]) -> Result<Vec<u8>, VaultError> {
        let cipher = Aes256Gcm::new_from_slice(&self.master_key)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        let mut nonce_bytes = [0u8; 12];
        OsRng.fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);

        let encrypted = cipher
            .encrypt(nonce, key_material)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        // Prepend nonce to ciphertext
        let mut result = Vec::with_capacity(12 + encrypted.len());
        result.extend_from_slice(&nonce_bytes);
        result.extend(encrypted);
        Ok(result)
    }

    /// Unwrap key material from storage.
    fn unwrap_key_material(&self, wrapped: &[u8]) -> Result<Vec<u8>, VaultError> {
        if wrapped.len() < 12 {
            return Err(VaultError::DecryptionFailed(
                "wrapped key too short".to_string(),
            ));
        }

        let (nonce_bytes, ciphertext) = wrapped.split_at(12);
        let cipher = Aes256Gcm::new_from_slice(&self.master_key)
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        let nonce = Nonce::from_slice(nonce_bytes);
        cipher
            .decrypt(nonce, ciphertext)
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))
    }

    fn compute_hmac_hex(&self, key: &[u8], data: &[u8]) -> String {
        use hmac::{Hmac, Mac};
        type HmacSha256 = Hmac<Sha256>;

        let mut mac = <HmacSha256 as Mac>::new_from_slice(key).expect("HMAC key length");
        mac.update(data);
        hex::encode(mac.finalize().into_bytes())
    }

    async fn log_audit(
        &self,
        operation: &str,
        key_id: Option<&str>,
        voter_vin: Option<&str>,
        modality: Option<&str>,
        actor: &str,
        success: bool,
        error: Option<&str>,
    ) {
        let id = Uuid::new_v4().to_string();
        let _ = sqlx::query(
            "INSERT INTO vault_audit_log (id, operation, key_id, voter_vin, modality, actor, success, error_detail)
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8)"
        )
            .bind(&id)
            .bind(operation)
            .bind(key_id)
            .bind(voter_vin)
            .bind(modality)
            .bind(actor)
            .bind(success)
            .bind(error)
            .execute(&self.pool)
            .await;
    }
}

impl Drop for BiometricVault {
    fn drop(&mut self) {
        self.master_key.zeroize();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // Tests require a running PostgreSQL instance provided via TEST_DATABASE_URL.
    // Run with: TEST_DATABASE_URL=postgresql://... cargo test -- --ignored --test-threads=1
    // (serial: the tests share one scratch DB and the single-active-key
    // invariant, so parallel runs can race on key creation/rotation).
    // No credentials are committed to the repository — tests skip when unset.
    async fn test_pool() -> Option<sqlx::PgPool> {
        let url = std::env::var("TEST_DATABASE_URL").ok()?;
        Some(crate::db::init_pool(&url).await.unwrap())
    }
    #[tokio::test]
    #[ignore]
    async fn test_encrypt_decrypt_roundtrip() {
        let Some(pool) = test_pool().await else {
            eprintln!("TEST_DATABASE_URL not set — skipping PostgreSQL integration test");
            return;
        };
        let vault = BiometricVault::new(pool).await.unwrap();
        let template_data = b"fingerprint_minutiae_data_here";

        let encrypted = vault
            .encrypt_template("VIN001", "fingerprint", template_data, "test")
            .await
            .unwrap();

        assert_ne!(encrypted.ciphertext, template_data);
        assert_eq!(encrypted.voter_vin, "VIN001");
        assert_eq!(encrypted.modality, "fingerprint");

        let decrypted = vault
            .decrypt_template(&encrypted.template_id, "test")
            .await
            .unwrap();
        assert_eq!(decrypted, template_data);
    }

    #[tokio::test]
    #[ignore]
    async fn test_key_rotation() {
        let Some(pool) = test_pool().await else {
            eprintln!("TEST_DATABASE_URL not set — skipping PostgreSQL integration test");
            return;
        };
        let vault = BiometricVault::new(pool).await.unwrap();
        let template_data = b"test_template";

        let encrypted = vault
            .encrypt_template("VIN002", "face", template_data, "test")
            .await
            .unwrap();
        let old_key_id = encrypted.key_id.clone();

        let new_key_id = vault.rotate_key(&old_key_id, "test").await.unwrap();
        assert_ne!(old_key_id, new_key_id);

        let decrypted = vault
            .decrypt_template(&encrypted.template_id, "test")
            .await
            .unwrap();
        assert_eq!(decrypted, template_data);
    }

    /// R4-10b regression: a mid-rotation failure must leave the vault
    /// consistent — every template stays either fully on the old key or fully
    /// on the new key (per-template transaction rolls back on error), and
    /// re-running the rotation resumes and completes.
    ///
    /// Failure is injected by corrupting one template's integrity hash so its
    /// decrypt-with-old-key step fails inside the per-template transaction.
    #[tokio::test]
    #[ignore]
    async fn test_rotate_key_mid_loop_failure_rolls_back_and_resumes() {
        let Some(pool) = test_pool().await else {
            eprintln!("TEST_DATABASE_URL not set — skipping PostgreSQL integration test");
            return;
        };
        let vault = BiometricVault::new(pool.clone()).await.unwrap();

        // Two templates on the current active key.
        let t1 = vault
            .encrypt_template("VIN-ROT-A", "fingerprint", b"template-a", "test")
            .await
            .unwrap();
        let t2 = vault
            .encrypt_template("VIN-ROT-B", "fingerprint", b"template-b", "test")
            .await
            .unwrap();
        let old_key = t1.key_id.clone();
        assert_eq!(
            old_key, t2.key_id,
            "both templates must start on the same key"
        );

        // Snapshot t2's integrity hash, then corrupt it to force a mid-loop
        // decrypt failure inside that template's transaction.
        let (saved_hash,): (String,) =
            sqlx::query_as("SELECT integrity_hash FROM vault_templates WHERE template_id = $1")
                .bind(&t2.template_id)
                .fetch_one(&pool)
                .await
                .unwrap();
        sqlx::query(
            "UPDATE vault_templates SET integrity_hash = 'corrupted' WHERE template_id = $1",
        )
        .bind(&t2.template_id)
        .execute(&pool)
        .await
        .unwrap();

        // Rotation must fail on the corrupted template.
        let outcome = vault.rotate_key(&old_key, "test").await;
        assert!(
            outcome.is_err(),
            "rotation must fail loudly on undecryptable template"
        );

        // The corrupted template's transaction rolled back: its row still
        // points at the old key with the (corrupted) ciphertext untouched —
        // never a half-written mixed-key record.
        let (t2_key, t2_hash): (String, String) = sqlx::query_as(
            "SELECT key_id, integrity_hash FROM vault_templates WHERE template_id = $1",
        )
        .bind(&t2.template_id)
        .fetch_one(&pool)
        .await
        .unwrap();
        assert_eq!(
            t2_key, old_key,
            "failed template must roll back to the old key"
        );
        assert_eq!(t2_hash, "corrupted");

        // The uncorrupted template is consistent regardless of processing
        // order: it decrypts to the original plaintext under whichever key
        // its (committed or never-started) transaction left it on.
        let p1 = vault
            .decrypt_template(&t1.template_id, "test")
            .await
            .unwrap();
        assert_eq!(p1, b"template-a");

        // Heal the corruption and re-run: rotation resumes and completes.
        sqlx::query("UPDATE vault_templates SET integrity_hash = $1 WHERE template_id = $2")
            .bind(&saved_hash)
            .bind(&t2.template_id)
            .execute(&pool)
            .await
            .unwrap();
        let new_key = vault.rotate_key(&old_key, "test").await.unwrap();
        assert_ne!(new_key, old_key);

        let p1 = vault
            .decrypt_template(&t1.template_id, "test")
            .await
            .unwrap();
        let p2 = vault
            .decrypt_template(&t2.template_id, "test")
            .await
            .unwrap();
        assert_eq!(p1, b"template-a");
        assert_eq!(p2, b"template-b");

        // No template remains on the old key after a successful rotation.
        let (remaining,): (i64,) =
            sqlx::query_as("SELECT COUNT(*) FROM vault_templates WHERE key_id = $1")
                .bind(&old_key)
                .fetch_one(&pool)
                .await
                .unwrap();
        assert_eq!(remaining, 0);
    }

    #[tokio::test]
    #[ignore]
    async fn test_revoked_key_blocks_decrypt() {
        let Some(pool) = test_pool().await else {
            eprintln!("TEST_DATABASE_URL not set — skipping PostgreSQL integration test");
            return;
        };
        let vault = BiometricVault::new(pool).await.unwrap();
        let template_data = b"test_template";

        let encrypted = vault
            .encrypt_template("VIN003", "iris", template_data, "test")
            .await
            .unwrap();

        vault.revoke_key(&encrypted.key_id, "test").await.unwrap();

        let result = vault.decrypt_template(&encrypted.template_id, "test").await;
        assert!(result.is_err());
    }

    #[tokio::test]
    #[ignore]
    async fn test_audit_trail() {
        let Some(pool) = test_pool().await else {
            eprintln!("TEST_DATABASE_URL not set — skipping PostgreSQL integration test");
            return;
        };
        let vault = BiometricVault::new(pool).await.unwrap();
        let _ = vault
            .encrypt_template("VIN005", "fingerprint", b"data", "officer1")
            .await;

        let audit = vault.get_audit_log(10).await.unwrap();
        assert!(!audit.is_empty());
        assert!(audit.iter().any(|e| e.operation == "encrypt_template"));
    }
}
