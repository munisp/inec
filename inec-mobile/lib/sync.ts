// Background sync manager for GOTV canvasser offline data.
// Pushes pending door knocks, pledges, and location updates to the backend.
// Uses exponential backoff for retries and conflict detection.

import NetInfo from '@react-native-community/netinfo';
import {
  getPendingDoorKnocks, markDoorKnockSynced, markDoorKnockFailed,
  getPendingPledges, markPledgeSynced,
  getPendingLocations, markLocationsSynced,
  logConflict, setSyncMeta, getPendingCounts,
  type PendingDoorKnock, type PendingPledge, type PendingLocationUpdate,
} from './storage';

const API_URL = process.env.EXPO_PUBLIC_API_URL ?? 'http://localhost:8088';

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
        const res = await fetch(`${API_URL}/gotv/canvass/knock`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer token',
            'X-Party-ID': '1',
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
          // Conflict — server has newer data
          const serverData = await res.json();
          await logConflict('door_knocks', String(knock.id), JSON.stringify(knock), JSON.stringify(serverData));
          await markDoorKnockSynced(knock.id); // Mark synced to avoid re-trying
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
        const res = await fetch(`${API_URL}/gotv/pledges`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer token',
            'X-Party-ID': '1',
          },
          body: JSON.stringify({
            contact_id: pledge.contact_id,
            pledge_type: pledge.pledge_type,
          }),
        });

        if (res.ok || res.status === 409) {
          await markPledgeSynced(pledge.id);
        }
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
        const res = await fetch(`${API_URL}/gotv/volunteers/${loc.volunteer_id}/location`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
            'Authorization': 'Bearer token',
            'X-Party-ID': '1',
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
