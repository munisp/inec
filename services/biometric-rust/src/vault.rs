//! Production biometric template vault with AES-256-GCM encryption.
//!
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
use std::collections::HashMap;
use std::sync::Arc;
use parking_lot::RwLock;
use uuid::Uuid;
use zeroize::{Zeroize, ZeroizeOnDrop};

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
}

#[derive(Clone, Serialize, Deserialize)]
pub enum KeyPurpose {
    TemplateEncryption,
    Signing,
    KeyWrapping,
}

#[derive(Clone, Serialize, Deserialize)]
pub enum KeyStatus {
    Active,
    Rotated,
    Revoked,
}

#[derive(Clone, Serialize, Deserialize)]
pub struct VaultKey {
    pub key_id: String,
    pub purpose: KeyPurpose,
    pub status: KeyStatus,
    pub rotation_count: u32,
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
    pub aad: Vec<u8>,
    pub hmac_tag: Vec<u8>,
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

pub struct BiometricVault {
    keys: Arc<RwLock<HashMap<String, VaultKey>>>,
    templates: Arc<RwLock<HashMap<String, EncryptedTemplate>>>,
    audit_log: Arc<RwLock<Vec<AuditEntry>>>,
    master_key: [u8; 32],
}

impl BiometricVault {
    pub fn new() -> Self {
        let mut master_key = [0u8; 32];
        OsRng.fill_bytes(&mut master_key);

        let vault = Self {
            keys: Arc::new(RwLock::new(HashMap::new())),
            templates: Arc::new(RwLock::new(HashMap::new())),
            audit_log: Arc::new(RwLock::new(Vec::new())),
            master_key,
        };

        // Generate initial encryption key
        let _ = vault.generate_key(KeyPurpose::TemplateEncryption, "system");
        vault
    }

    pub fn from_master_key(master_key: [u8; 32]) -> Self {
        let vault = Self {
            keys: Arc::new(RwLock::new(HashMap::new())),
            templates: Arc::new(RwLock::new(HashMap::new())),
            audit_log: Arc::new(RwLock::new(Vec::new())),
            master_key,
        };
        let _ = vault.generate_key(KeyPurpose::TemplateEncryption, "system");
        vault
    }

    pub fn generate_key(&self, purpose: KeyPurpose, actor: &str) -> Result<String, VaultError> {
        let key_id = format!("key-{}", Uuid::new_v4());

        // Derive key material using HKDF
        let mut salt = [0u8; 32];
        OsRng.fill_bytes(&mut salt);

        let hk = Hkdf::<Sha256>::new(Some(&salt), &self.master_key);
        let mut key_material = vec![0u8; 32];
        hk.expand(key_id.as_bytes(), &mut key_material)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        let vault_key = VaultKey {
            key_id: key_id.clone(),
            purpose,
            status: KeyStatus::Active,
            rotation_count: 0,
            created_at: Utc::now(),
            rotated_at: None,
            key_material,
        };

        self.keys.write().insert(key_id.clone(), vault_key);
        self.log_audit("generate_key", Some(&key_id), None, None, actor, true, None);

        Ok(key_id)
    }

    pub fn encrypt_template(
        &self,
        voter_vin: &str,
        modality: &str,
        template_data: &[u8],
        actor: &str,
    ) -> Result<EncryptedTemplate, VaultError> {
        let key = self.get_active_key(KeyPurpose::TemplateEncryption)?;

        let cipher = Aes256Gcm::new_from_slice(&key.key_material)
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        let mut nonce_bytes = [0u8; 12];
        OsRng.fill_bytes(&mut nonce_bytes);
        let nonce = Nonce::from_slice(&nonce_bytes);

        // AAD includes voter VIN and modality for binding
        let aad = format!("{}:{}", voter_vin, modality).into_bytes();

        let ciphertext = cipher
            .encrypt(nonce, aes_gcm::aead::Payload {
                msg: template_data,
                aad: &aad,
            })
            .map_err(|e| VaultError::EncryptionFailed(e.to_string()))?;

        // HMAC over ciphertext for integrity
        let hmac_tag = self.compute_hmac(&key.key_material, &ciphertext);

        let template_id = format!("tmpl-{}", Uuid::new_v4());

        let encrypted = EncryptedTemplate {
            template_id: template_id.clone(),
            voter_vin: voter_vin.to_string(),
            modality: modality.to_string(),
            key_id: key.key_id.clone(),
            ciphertext,
            nonce: nonce_bytes,
            aad,
            hmac_tag,
            created_at: Utc::now(),
        };

        self.templates.write().insert(template_id, encrypted.clone());
        self.log_audit(
            "encrypt_template",
            Some(&key.key_id),
            Some(voter_vin),
            Some(modality),
            actor,
            true,
            None,
        );

        Ok(encrypted)
    }

    pub fn decrypt_template(
        &self,
        template_id: &str,
        actor: &str,
    ) -> Result<Vec<u8>, VaultError> {
        let templates = self.templates.read();
        let encrypted = templates
            .get(template_id)
            .ok_or_else(|| VaultError::TemplateNotFound(template_id.to_string()))?;

        let keys = self.keys.read();
        let key = keys
            .get(&encrypted.key_id)
            .ok_or_else(|| VaultError::KeyNotFound(encrypted.key_id.clone()))?;

        if matches!(key.status, KeyStatus::Revoked) {
            self.log_audit(
                "decrypt_template",
                Some(&key.key_id),
                Some(&encrypted.voter_vin),
                Some(&encrypted.modality),
                actor,
                false,
                Some("key_revoked"),
            );
            return Err(VaultError::KeyRevoked(key.key_id.clone()));
        }

        // Verify HMAC
        let expected_hmac = self.compute_hmac(&key.key_material, &encrypted.ciphertext);
        if expected_hmac != encrypted.hmac_tag {
            self.log_audit(
                "decrypt_template",
                Some(&key.key_id),
                Some(&encrypted.voter_vin),
                Some(&encrypted.modality),
                actor,
                false,
                Some("integrity_check_failed"),
            );
            return Err(VaultError::IntegrityCheckFailed);
        }

        let cipher = Aes256Gcm::new_from_slice(&key.key_material)
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        let nonce = Nonce::from_slice(&encrypted.nonce);

        let plaintext = cipher
            .decrypt(nonce, aes_gcm::aead::Payload {
                msg: &encrypted.ciphertext,
                aad: &encrypted.aad,
            })
            .map_err(|e| VaultError::DecryptionFailed(e.to_string()))?;

        self.log_audit(
            "decrypt_template",
            Some(&key.key_id),
            Some(&encrypted.voter_vin),
            Some(&encrypted.modality),
            actor,
            true,
            None,
        );

        Ok(plaintext)
    }

    pub fn rotate_key(&self, key_id: &str, actor: &str) -> Result<String, VaultError> {
        let new_key_id = self.generate_key(KeyPurpose::TemplateEncryption, actor)?;

        {
            let mut keys = self.keys.write();
            if let Some(old_key) = keys.get_mut(key_id) {
                old_key.status = KeyStatus::Rotated;
                old_key.rotated_at = Some(Utc::now());
                old_key.rotation_count += 1;
            }
        }

        // Re-encrypt all templates that used the old key
        let template_ids: Vec<String> = {
            let templates = self.templates.read();
            templates
                .iter()
                .filter(|(_, t)| t.key_id == key_id)
                .map(|(id, _)| id.clone())
                .collect()
        };

        for tid in &template_ids {
            let plaintext = self.decrypt_template(tid, actor)?;
            let encrypted = {
                let templates = self.templates.read();
                let t = templates.get(tid).unwrap();
                (t.voter_vin.clone(), t.modality.clone())
            };
            let new_encrypted = self.encrypt_template(
                &encrypted.0,
                &encrypted.1,
                &plaintext,
                actor,
            )?;
            self.templates.write().insert(tid.clone(), EncryptedTemplate {
                template_id: tid.clone(),
                ..new_encrypted
            });
        }

        self.log_audit(
            "rotate_key",
            Some(key_id),
            None,
            None,
            actor,
            true,
            None,
        );

        Ok(new_key_id)
    }

    pub fn revoke_key(&self, key_id: &str, actor: &str) -> Result<(), VaultError> {
        let mut keys = self.keys.write();
        let key = keys
            .get_mut(key_id)
            .ok_or_else(|| VaultError::KeyNotFound(key_id.to_string()))?;
        key.status = KeyStatus::Revoked;
        key.key_material.zeroize();

        self.log_audit("revoke_key", Some(key_id), None, None, actor, true, None);
        Ok(())
    }

    pub fn get_stats(&self) -> serde_json::Value {
        let keys = self.keys.read();
        let templates = self.templates.read();
        let audit = self.audit_log.read();

        let active_keys = keys.values().filter(|k| matches!(k.status, KeyStatus::Active)).count();
        let rotated_keys = keys.values().filter(|k| matches!(k.status, KeyStatus::Rotated)).count();
        let revoked_keys = keys.values().filter(|k| matches!(k.status, KeyStatus::Revoked)).count();

        serde_json::json!({
            "total_keys": keys.len(),
            "active_keys": active_keys,
            "rotated_keys": rotated_keys,
            "revoked_keys": revoked_keys,
            "total_templates": templates.len(),
            "total_audit_entries": audit.len(),
            "encryption_algorithm": "AES-256-GCM",
            "key_derivation": "HKDF-SHA256",
        })
    }

    pub fn get_audit_log(&self, limit: usize) -> Vec<AuditEntry> {
        let audit = self.audit_log.read();
        audit.iter().rev().take(limit).cloned().collect()
    }

    fn get_active_key(&self, purpose: KeyPurpose) -> Result<VaultKey, VaultError> {
        let keys = self.keys.read();
        let purpose_str = match purpose {
            KeyPurpose::TemplateEncryption => "TemplateEncryption",
            KeyPurpose::Signing => "Signing",
            KeyPurpose::KeyWrapping => "KeyWrapping",
        };

        keys.values()
            .find(|k| {
                matches!(k.status, KeyStatus::Active)
                    && matches!((&k.purpose, &purpose), (KeyPurpose::TemplateEncryption, KeyPurpose::TemplateEncryption)
                        | (KeyPurpose::Signing, KeyPurpose::Signing)
                        | (KeyPurpose::KeyWrapping, KeyPurpose::KeyWrapping))
            })
            .cloned()
            .ok_or_else(|| VaultError::KeyNotFound(format!("no active key for {:?}", purpose_str)))
    }

    fn compute_hmac(&self, key: &[u8], data: &[u8]) -> Vec<u8> {
        use hmac::{Hmac, Mac};
        type HmacSha256 = Hmac<Sha256>;

        let mut mac = <HmacSha256 as Mac>::new_from_slice(key).expect("HMAC key length");
        mac.update(data);
        mac.finalize().into_bytes().to_vec()
    }

    fn log_audit(
        &self,
        operation: &str,
        key_id: Option<&str>,
        voter_vin: Option<&str>,
        modality: Option<&str>,
        actor: &str,
        success: bool,
        error: Option<&str>,
    ) {
        let entry = AuditEntry {
            id: Uuid::new_v4().to_string(),
            operation: operation.to_string(),
            key_id: key_id.map(String::from),
            voter_vin: voter_vin.map(String::from),
            modality: modality.map(String::from),
            actor: actor.to_string(),
            success,
            error_detail: error.map(String::from),
            timestamp: Utc::now(),
        };
        self.audit_log.write().push(entry);
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

    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let vault = BiometricVault::new();
        let template_data = b"fingerprint_minutiae_data_here";

        let encrypted = vault
            .encrypt_template("VIN001", "fingerprint", template_data, "test")
            .unwrap();

        assert_ne!(encrypted.ciphertext, template_data);
        assert_eq!(encrypted.voter_vin, "VIN001");
        assert_eq!(encrypted.modality, "fingerprint");

        let decrypted = vault.decrypt_template(&encrypted.template_id, "test").unwrap();
        assert_eq!(decrypted, template_data);
    }

    #[test]
    fn test_key_rotation() {
        let vault = BiometricVault::new();
        let template_data = b"test_template";

        let encrypted = vault
            .encrypt_template("VIN002", "facial", template_data, "test")
            .unwrap();
        let old_key_id = encrypted.key_id.clone();

        let new_key_id = vault.rotate_key(&old_key_id, "test").unwrap();
        assert_ne!(old_key_id, new_key_id);

        let decrypted = vault.decrypt_template(&encrypted.template_id, "test").unwrap();
        assert_eq!(decrypted, template_data);
    }

    #[test]
    fn test_revoked_key_blocks_decrypt() {
        let vault = BiometricVault::new();
        let template_data = b"test_template";

        let encrypted = vault
            .encrypt_template("VIN003", "iris", template_data, "test")
            .unwrap();

        vault.revoke_key(&encrypted.key_id, "test").unwrap();

        let result = vault.decrypt_template(&encrypted.template_id, "test");
        assert!(result.is_err());
    }

    #[test]
    fn test_tampered_ciphertext_detected() {
        let vault = BiometricVault::new();
        let template_data = b"test_template";

        let mut encrypted = vault
            .encrypt_template("VIN004", "fingerprint", template_data, "test")
            .unwrap();

        // Tamper with ciphertext
        if !encrypted.ciphertext.is_empty() {
            encrypted.ciphertext[0] ^= 0xFF;
        }
        vault.templates.write().insert(encrypted.template_id.clone(), encrypted.clone());

        let result = vault.decrypt_template(&encrypted.template_id, "test");
        assert!(result.is_err());
    }

    #[test]
    fn test_audit_trail() {
        let vault = BiometricVault::new();
        let _ = vault.encrypt_template("VIN005", "fingerprint", b"data", "officer1");

        let audit = vault.get_audit_log(10);
        assert!(!audit.is_empty());
        assert!(audit.iter().any(|e| e.operation == "encrypt_template"));
    }
}
