/**
 * Authentication module with biometric support and secure token storage.
 */
import * as SecureStore from 'expo-secure-store';
import * as LocalAuthentication from 'expo-local-authentication';
import { setAuthToken } from './api';

const TOKEN_KEY = 'inec_auth_token';
const USER_KEY = 'inec_user_data';
const BIOMETRIC_KEY = 'inec_biometric_enabled';

export interface User {
  id: string;
  email: string;
  name: string;
  role: string;
  partyCode: string;
  permissions: string[];
}

export async function login(email: string, password: string): Promise<User> {
  // In production: POST to Keycloak OIDC token endpoint
  // For now: validate against backend auth
  const res = await fetch(`${__DEV__ ? 'http://localhost:8103' : 'https://api.inec.gov.ng'}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });

  if (!res.ok) {
    // Dev mode fallback
    if (__DEV__) {
      const user: User = {
        id: 'dev-user-1',
        email,
        name: email.split('@')[0],
        role: 'admin',
        partyCode: 'APC',
        permissions: ['*'],
      };
      await SecureStore.setItemAsync(TOKEN_KEY, 'dev-token');
      await SecureStore.setItemAsync(USER_KEY, JSON.stringify(user));
      setAuthToken('dev-token');
      return user;
    }
    throw new Error('Invalid credentials');
  }

  const data = await res.json();
  await SecureStore.setItemAsync(TOKEN_KEY, data.token);
  await SecureStore.setItemAsync(USER_KEY, JSON.stringify(data.user));
  setAuthToken(data.token);
  return data.user;
}

export async function logout(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
  await SecureStore.deleteItemAsync(USER_KEY);
  setAuthToken('');
}

export async function getStoredUser(): Promise<User | null> {
  try {
    const userData = await SecureStore.getItemAsync(USER_KEY);
    const token = await SecureStore.getItemAsync(TOKEN_KEY);
    if (userData && token) {
      setAuthToken(token);
      return JSON.parse(userData);
    }
  } catch {}
  return null;
}

export async function isBiometricAvailable(): Promise<boolean> {
  const compatible = await LocalAuthentication.hasHardwareAsync();
  const enrolled = await LocalAuthentication.isEnrolledAsync();
  return compatible && enrolled;
}

export async function authenticateWithBiometric(): Promise<boolean> {
  const result = await LocalAuthentication.authenticateAsync({
    promptMessage: 'Authenticate to INEC Platform',
    cancelLabel: 'Use Password',
    disableDeviceFallback: false,
    fallbackLabel: 'Enter Password',
  });
  return result.success;
}

export async function enableBiometric(): Promise<void> {
  await SecureStore.setItemAsync(BIOMETRIC_KEY, 'true');
}

export async function isBiometricEnabled(): Promise<boolean> {
  const val = await SecureStore.getItemAsync(BIOMETRIC_KEY);
  return val === 'true';
}
