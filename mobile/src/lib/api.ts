/**
 * API client with offline-first support, retry logic, and auth headers.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';

const API_BASE = __DEV__
  ? 'http://localhost:8103'
  : 'https://api.inec.gov.ng';

const CACHE_PREFIX = 'api_cache:';
const CACHE_TTL = 5 * 60 * 1000; // 5 minutes

interface CachedResponse {
  data: any;
  timestamp: number;
}

let authToken: string | null = null;
let partyCode: string = 'APC';

export function setAuthToken(token: string) {
  authToken = token;
}

export function setPartyCode(code: string) {
  partyCode = code;
}

async function getHeaders(): Promise<Record<string, string>> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-GOTV-Party-Code': partyCode,
  };
  if (authToken) {
    headers['Authorization'] = `Bearer ${authToken}`;
  }
  return headers;
}

export async function apiGet<T = any>(endpoint: string, useCache = true): Promise<T> {
  const cacheKey = CACHE_PREFIX + endpoint;

  // Try cache first if offline or useCache=true
  if (useCache) {
    try {
      const cached = await AsyncStorage.getItem(cacheKey);
      if (cached) {
        const { data, timestamp }: CachedResponse = JSON.parse(cached);
        if (Date.now() - timestamp < CACHE_TTL) {
          return data;
        }
      }
    } catch {}
  }

  // Network request with retry
  const headers = await getHeaders();
  let lastError: Error | null = null;

  for (let attempt = 0; attempt < 3; attempt++) {
    try {
      const res = await fetch(`${API_BASE}${endpoint}`, {
        method: 'GET',
        headers,
        signal: AbortSignal.timeout(10000),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();

      // Cache successful response
      await AsyncStorage.setItem(cacheKey, JSON.stringify({
        data,
        timestamp: Date.now(),
      }));

      return data;
    } catch (err) {
      lastError = err as Error;
      if (attempt < 2) await new Promise(r => setTimeout(r, 500 * (attempt + 1)));
    }
  }

  // Fallback to stale cache
  try {
    const cached = await AsyncStorage.getItem(cacheKey);
    if (cached) {
      const { data }: CachedResponse = JSON.parse(cached);
      return data;
    }
  } catch {}

  throw lastError || new Error('Network request failed');
}

export async function apiPost<T = any>(endpoint: string, body: any): Promise<T> {
  const headers = await getHeaders();

  try {
    const res = await fetch(`${API_BASE}${endpoint}`, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(15000),
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  } catch (err) {
    // Queue for offline sync
    const queue = JSON.parse(await AsyncStorage.getItem('offline_queue') || '[]');
    queue.push({ endpoint, body, timestamp: Date.now() });
    await AsyncStorage.setItem('offline_queue', JSON.stringify(queue));
    throw err;
  }
}

export async function syncOfflineQueue(): Promise<number> {
  const queue = JSON.parse(await AsyncStorage.getItem('offline_queue') || '[]');
  let synced = 0;

  for (let i = 0; i < queue.length; i++) {
    try {
      await apiPost(queue[i].endpoint, queue[i].body);
      synced++;
    } catch {
      // Keep remaining items in queue
      await AsyncStorage.setItem('offline_queue', JSON.stringify(queue.slice(i)));
      return synced;
    }
  }

  await AsyncStorage.removeItem('offline_queue');
  return synced;
}
