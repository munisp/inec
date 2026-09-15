// Background sync manager for GOTV canvasser offline data.
// Pushes pending door knocks, pledges, and location updates to the backend.
// Uses exponential backoff for retries and conflict detection.

import NetInfo from '@react-native-community/netinfo';
import {
  getPendingDoorKnocks, markDoorKnockSynced, markDoorKnockFailed,
  getPendingPledges, markPledgeSynced, markPledgeFailed,
  getPendingLocations, markLocationsSynced,
  logConflict, setSyncMeta, getPendingCounts,
  type PendingDoorKnock, type PendingPledge, type PendingLocationUpdate,
} from './storage';
import { getMobileToken, GOTV_API } from './gotv-auth';

// R5-114: conflicts are surfaced to the UI instead of being silently
// swallowed. NOTE (HANDOFF): gotv-svc has no conflict-ingest endpoint, so
// the server still cannot learn about conflicts — the record is parked
// locally (sync_status='failed') + logged + the user is prompted.
export interface ConflictNotice {
  table: string;
  recordId: string;
  serverData: string;
}

type ConflictListener = (notice: ConflictNotice) => void;
const conflictListeners = new Set<ConflictListener>();

export function onConflict(listener: ConflictListener): () => void {
  conflictListeners.add(listener);
  return () => conflictListeners.delete(listener);
}

function notifyConflict(notice: ConflictNotice): void {
  conflictListeners.forEach((l) => {
    try { l(notice); } catch { /* listener errors must not break sync */ }
  });
}

/** Safely read a 409 response body for the conflict log. */
async function readConflictBody(res: Response): Promise<string> {
  try {
    const text = await res.text();
    return text || '(empty conflict response body)';
  } catch {
    return '(unreadable conflict response body)';
  }
}

// Use GOTV mobile backend (standalone from INEC portal). GOTV_API throws at
// startup in non-dev builds when EXPO_PUBLIC_GOTV_API_URL is unset.
const API_URL = GOTV_API;

export type SyncState = 'idle' | 'syncing' | 'offline' | 'error';

type SyncListener = (state: SyncState, pendingCount: number) => void;

class SyncManager {
  private state: SyncState = 'idle';
  private listeners: SyncListener[] = [];
  private retryCount = 0;
  private maxRetries = 5;
  private syncIntervalId: ReturnType<typeof setInterval> | null = null;
  private isOnline = true;

  constructor() {
    // Monitor network state
    NetInfo.addEventListener(state => {
      this.isOnline = state.isConnected ?? false;
      if (this.isOnline && this.state === 'offline') {
        this.syncAll();
      }
      if (!this.isOnline) {
        this.setState('offline');
      }
    });
  }

  subscribe(listener: SyncListener): () => void {
    this.listeners.push(listener);
    return () => {
      this.listeners = this.listeners.filter(l => l !== listener);
    };
  }

  private async setState(newState: SyncState) {
    this.state = newState;
    const counts = await getPendingCounts();
    const total = counts.knocks + counts.pledges + counts.locations;
    this.listeners.forEach(l => l(newState, total));
  }

  // Start periodic sync (every 30 seconds when online)
  start(intervalMs = 30000): void {
    if (this.syncIntervalId) return;
    this.syncIntervalId = setInterval(() => {
      if (this.isOnline && this.state !== 'syncing') {
        this.syncAll();
      }
    }, intervalMs);
    // Initial sync
    if (this.isOnline) this.syncAll();
  }

  stop(): void {
    if (this.syncIntervalId) {
      clearInterval(this.syncIntervalId);
      this.syncIntervalId = null;
    }
  }

  async syncAll(): Promise<void> {
    if (!this.isOnline) {
      await this.setState('offline');
      return;
    }

    await this.setState('syncing');
    try {
      await this.syncDoorKnocks();
      await this.syncPledges();
      await this.syncLocations();
      this.retryCount = 0;
      await setSyncMeta('last_sync', new Date().toISOString());
      await this.setState('idle');
    } catch (err) {
      this.retryCount++;
      if (this.retryCount >= this.maxRetries) {
        await this.setState('error');
      } else {
        // Exponential backoff
        const delay = Math.min(1000 * Math.pow(2, this.retryCount), 60000);
        setTimeout(() => this.syncAll(), delay);
      }
    }
  }

  private async syncDoorKnocks(): Promise<void> {
    const knocks = await getPendingDoorKnocks();
    for (const knock of knocks) {
      try {
        const token = await getMobileToken();
        const res = await fetch(`${API_URL}/gotv/mobile/knock`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({
            volunteer_id: knock.volunteer_id,
            contact_id: knock.contact_id,
            latitude: knock.latitude,
            longitude: knock.longitude,
            outcome: knock.outcome,
            notes: knock.notes,
            speed_kmh: knock.speed_kmh,
          }),
        });

        if (res.ok) {
          await markDoorKnockSynced(knock.id);
        } else if (res.status === 409) {
          // R5-114: conflict — the local record LOSES but must not be
          // silently discarded: park it (failed), log both versions, and
          // prompt the user. Never mark a conflicted record 'synced'.
          const serverData = await readConflictBody(res);
          await logConflict('door_knocks', String(knock.id), JSON.stringify(knock), serverData);
          await markDoorKnockFailed(knock.id);
          notifyConflict({ table: 'door_knocks', recordId: String(knock.id), serverData });
        } else {
          await markDoorKnockFailed(knock.id);
        }
      } catch {
        // Network error — leave as pending for next sync
        throw new Error('Network error during door knock sync');
      }
    }
  }

  private async syncPledges(): Promise<void> {
    const pledges = await getPendingPledges();
    for (const pledge of pledges) {
      try {
        const token = await getMobileToken();
        const res = await fetch(`${API_URL}/gotv/pledges`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({
            contact_id: pledge.contact_id,
            pledge_type: pledge.pledge_type,
          }),
        });

        if (res.ok) {
          await markPledgeSynced(pledge.id);
        } else if (res.status === 409) {
          // R5-114: read the server body before deciding; a conflicted
          // pledge is parked for review, not marked synced.
          const serverData = await readConflictBody(res);
          await logConflict('pledges', String(pledge.id), JSON.stringify(pledge), serverData);
          await markPledgeFailed(pledge.id);
          notifyConflict({ table: 'pledges', recordId: String(pledge.id), serverData });
        }
        // Other non-OK statuses: leave pending for the next sync cycle.
      } catch {
        throw new Error('Network error during pledge sync');
      }
    }
  }

  private async syncLocations(): Promise<void> {
    const locations = await getPendingLocations();
    if (locations.length === 0) return;

    // Batch location updates — send only the latest per volunteer
    const latestByVol = new Map<string, PendingLocationUpdate>();
    for (const loc of locations) {
      latestByVol.set(loc.volunteer_id, loc);
    }

    const syncedIds: number[] = [];
    for (const [, loc] of latestByVol) {
      try {
        const token = await getMobileToken();
        const res = await fetch(`${API_URL}/gotv/volunteers/${loc.volunteer_id}/location`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            ...(token ? { 'Authorization': `Bearer ${token}` } : {}),
          },
          body: JSON.stringify({
            latitude: loc.latitude,
            longitude: loc.longitude,
            battery: loc.battery,
            speed_kmh: loc.speed_kmh,
          }),
        });

        if (res.ok) {
          // Mark all locations for this volunteer as synced
          const volLocs = locations.filter(l => l.volunteer_id === loc.volunteer_id);
          syncedIds.push(...volLocs.map(l => l.id));
        }
      } catch {
        throw new Error('Network error during location sync');
      }
    }

    if (syncedIds.length > 0) {
      await markLocationsSynced(syncedIds);
    }
  }
}

// Singleton instance
export const syncManager = new SyncManager();
