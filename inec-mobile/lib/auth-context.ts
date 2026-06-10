// Auth context — determines which user type is active (GOTV vs INEC).
// GOTV users (party volunteers/canvassers) and INEC users (observers/admins)
// are completely separate populations with different auth flows.

import * as SecureStore from 'expo-secure-store';

export type AuthMode = 'none' | 'gotv' | 'inec';

const AUTH_MODE_KEY = 'app_auth_mode';

// GOTV screens — only accessible to GOTV-authenticated users
export const GOTV_ROUTES = new Set([
  'gotv-login',
  'gotv-canvasser',
  'gotv',
]);

// Screens that are always accessible (entry points, shared)
export const PUBLIC_ROUTES = new Set([
  'index',
]);

export async function getAuthMode(): Promise<AuthMode> {
  const mode = await SecureStore.getItemAsync(AUTH_MODE_KEY);
  if (mode === 'gotv' || mode === 'inec') return mode;
  return 'none';
}

export async function setAuthMode(mode: AuthMode): Promise<void> {
  if (mode === 'none') {
    await SecureStore.deleteItemAsync(AUTH_MODE_KEY);
  } else {
    await SecureStore.setItemAsync(AUTH_MODE_KEY, mode);
  }
}

export function isGOTVRoute(routeName: string): boolean {
  return routeName.startsWith('gotv');
}

export function isINECRoute(routeName: string): boolean {
  return !isGOTVRoute(routeName) && !PUBLIC_ROUTES.has(routeName);
}

// Check if a given route is allowed for the current auth mode
export function isRouteAllowed(routeName: string, mode: AuthMode): boolean {
  if (PUBLIC_ROUTES.has(routeName)) return true;
  if (mode === 'gotv') return isGOTVRoute(routeName);
  if (mode === 'inec') return isINECRoute(routeName);
  return false;
}
