/**
 * Field-level obfuscation for sensitive SQLite data.
 *
 * SECURITY — HONEST DEGRADATION: this is NOT encryption. It is a keyed XOR
 * obfuscation that only defeats casual inspection of a dumped database file.
 * It provides NO confidentiality against an attacker with the device key
 * (stored alongside the data in SecureStore on the same device).
 *
 * Real at-rest encryption requires either:
 *  - SQLCipher via a dev-client build (expo-sqlite with SQLCipher native
 *    module), or
 *  - AES-256-GCM via a vetted native binding.
 * Neither is available in Expo Go / the current managed build, so sensitive
 * fields are obfuscated (never "encrypted") and callers should prefer storing
 * only non-sensitive fields or masked values wherever possible.
 *
 * The one hard requirement this module DOES meet: the obfuscation key is
 * generated with a cryptographically secure RNG (expo-crypto
 * getRandomBytesAsync), never Math.random.
 */
import * as SecureStore from 'expo-secure-store';
import { getRandomBytesAsync } from 'expo-crypto';

const OBFUSCATION_KEY_ALIAS = 'inec_db_encryption_key';
const KEY_LENGTH = 32;

let cachedKey: Uint8Array | null = null;

/**
 * Get or generate the device-specific obfuscation key.
 * Stored in SecureStore (iOS Keychain / Android Keystore).
 */
async function getObfuscationKey(): Promise<Uint8Array> {
  if (cachedKey) return cachedKey;

  const stored = await SecureStore.getItemAsync(OBFUSCATION_KEY_ALIAS);
  if (stored) {
    cachedKey = new Uint8Array(JSON.parse(stored));
    return cachedKey;
  }

  // Generate a new key with a cryptographically secure RNG.
  const key = await getRandomBytesAsync(KEY_LENGTH);

  await SecureStore.setItemAsync(
    OBFUSCATION_KEY_ALIAS,
    JSON.stringify(Array.from(key)),
    { requireAuthentication: false }
  );

  cachedKey = key;
  return key;
}

/**
 * Obfuscate a string value for storage in SQLite.
 * Returns a base64-encoded obfuscated string.
 *
 * NOTE: keyed XOR — obfuscation only, NOT encryption. See module header.
 */
export async function obfuscateField(plaintext: string): Promise<string> {
  if (!plaintext) return '';

  const key = await getObfuscationKey();
  const textBytes = new TextEncoder().encode(plaintext);
  const obfuscated = new Uint8Array(textBytes.length);

  for (let i = 0; i < textBytes.length; i++) {
    obfuscated[i] = textBytes[i] ^ key[i % key.length];
  }

  // Convert to base64 manually (React Native compatible)
  return btoa(String.fromCharCode(...obfuscated));
}

/**
 * Reverse obfuscateField for a base64-encoded value from SQLite.
 */
export async function deobfuscateField(obfuscated: string): Promise<string> {
  if (!obfuscated) return '';

  const key = await getObfuscationKey();
  const raw = atob(obfuscated);
  const obfuscatedBytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) {
    obfuscatedBytes[i] = raw.charCodeAt(i);
  }

  const restored = new Uint8Array(obfuscatedBytes.length);
  for (let i = 0; i < obfuscatedBytes.length; i++) {
    restored[i] = obfuscatedBytes[i] ^ key[i % key.length];
  }

  return new TextDecoder().decode(restored);
}

/**
 * Check if obfuscation is available (SecureStore accessible).
 */
export async function isObfuscationAvailable(): Promise<boolean> {
  try {
    await getObfuscationKey();
    return true;
  } catch {
    return false;
  }
}
