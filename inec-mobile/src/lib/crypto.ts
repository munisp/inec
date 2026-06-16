/**
 * Field-level encryption for sensitive SQLite data.
 *
 * Uses expo-secure-store for key storage and a simple XOR cipher
 * with a device-specific key for field-level encryption of biometric
 * and voter data at rest.
 *
 * In production, this should be replaced with AES-256-GCM via
 * expo-crypto once native module support is confirmed for the
 * target Expo SDK version.
 */
import * as SecureStore from 'expo-secure-store';

const ENCRYPTION_KEY_ALIAS = 'inec_db_encryption_key';
const KEY_LENGTH = 32;

let cachedKey: Uint8Array | null = null;

/**
 * Get or generate the device-specific encryption key.
 * Stored in SecureStore (iOS Keychain / Android Keystore).
 */
async function getEncryptionKey(): Promise<Uint8Array> {
  if (cachedKey) return cachedKey;

  const stored = await SecureStore.getItemAsync(ENCRYPTION_KEY_ALIAS);
  if (stored) {
    cachedKey = new Uint8Array(JSON.parse(stored));
    return cachedKey;
  }

  // Generate a new random key
  const key = new Uint8Array(KEY_LENGTH);
  for (let i = 0; i < KEY_LENGTH; i++) {
    key[i] = Math.floor(Math.random() * 256);
  }

  await SecureStore.setItemAsync(
    ENCRYPTION_KEY_ALIAS,
    JSON.stringify(Array.from(key)),
    { requireAuthentication: false }
  );

  cachedKey = key;
  return key;
}

/**
 * Encrypt a string value for storage in SQLite.
 * Returns a base64-encoded encrypted string.
 */
export async function encryptField(plaintext: string): Promise<string> {
  if (!plaintext) return '';

  const key = await getEncryptionKey();
  const textBytes = new TextEncoder().encode(plaintext);
  const encrypted = new Uint8Array(textBytes.length);

  for (let i = 0; i < textBytes.length; i++) {
    encrypted[i] = textBytes[i] ^ key[i % key.length];
  }

  // Convert to base64 manually (React Native compatible)
  return btoa(String.fromCharCode(...encrypted));
}

/**
 * Decrypt a base64-encoded encrypted string from SQLite.
 */
export async function decryptField(ciphertext: string): Promise<string> {
  if (!ciphertext) return '';

  const key = await getEncryptionKey();
  const raw = atob(ciphertext);
  const encryptedBytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) {
    encryptedBytes[i] = raw.charCodeAt(i);
  }

  const decrypted = new Uint8Array(encryptedBytes.length);
  for (let i = 0; i < encryptedBytes.length; i++) {
    decrypted[i] = encryptedBytes[i] ^ key[i % key.length];
  }

  return new TextDecoder().decode(decrypted);
}

/**
 * Check if encryption is available (SecureStore accessible).
 */
export async function isEncryptionAvailable(): Promise<boolean> {
  try {
    await getEncryptionKey();
    return true;
  } catch {
    return false;
  }
}
