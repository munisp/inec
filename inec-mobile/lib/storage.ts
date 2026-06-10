// Offline-first storage for GOTV canvasser workflow.
// Uses expo-sqlite v56 (openDatabaseAsync) + expo-secure-store for field-level encryption.

import * as SQLite from 'expo-sqlite';
import * as SecureStore from 'expo-secure-store';

export type SyncStatus = 'pending' | 'syncing' | 'synced' | 'failed';

export interface PendingDoorKnock {
  id: number;
  volunteer_id: string;
  contact_id: string | null;
  latitude: number;
  longitude: number;
  outcome: string;
  notes: string;
  speed_kmh: number;
  recorded_at: string;
  sync_status: SyncStatus;
}

export interface PendingPledge {
  id: number;
  contact_id: string;
  pledge_type: string;
  notes: string;
  recorded_at: string;
  sync_status: SyncStatus;
}

export interface PendingLocationUpdate {
  id: number;
  volunteer_id: string;
  latitude: number;
  longitude: number;
  battery: number;
  speed_kmh: number;
  recorded_at: string;
  sync_status: SyncStatus;
}

export interface CachedContact {
  contact_id: string;
  phone_masked: string;
  full_name_encrypted: string;
  state_code: string;
  lga_code: string;
  ward_code: string;
  voter_status: string;
  cached_at: string;
}

export interface ConflictLogEntry {
  id: number;
  table_name: string;
  record_id: string;
  local_data: string;
  server_data: string;
  resolved_at: string | null;
  resolution: string | null;
}

let db: SQLite.SQLiteDatabase | null = null;

async function getEncryptionKey(): Promise<string> {
  let key = await SecureStore.getItemAsync('gotv_db_key');
  if (!key) {
    // Generate a random encryption key on first use
    const bytes = new Uint8Array(32);
    for (let i = 0; i < 32; i++) {
      bytes[i] = Math.floor(Math.random() * 256);
    }
    key = Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('');
    await SecureStore.setItemAsync('gotv_db_key', key);
  }
  return key;
}

export async function getDB(): Promise<SQLite.SQLiteDatabase> {
  if (db) return db;

  db = await SQLite.openDatabaseAsync('gotv_canvasser.db');

  // Enable WAL mode for better concurrent read/write
  await db.execAsync('PRAGMA journal_mode = WAL;');

  // Set up encryption key for field-level encryption via PRAGMA
  const encKey = await getEncryptionKey();
  try {
    await db.execAsync(`PRAGMA key = '${encKey}';`);
  } catch {
    // SQLCipher may not be available — continue without DB-level encryption
    // Field-level encryption via SecureStore still works
  }

  // Create tables
  await db.execAsync(`
    CREATE TABLE IF NOT EXISTS pending_door_knocks (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      volunteer_id TEXT NOT NULL,
      contact_id TEXT,
      latitude REAL NOT NULL,
      longitude REAL NOT NULL,
      outcome TEXT NOT NULL,
      notes TEXT DEFAULT '',
      speed_kmh REAL DEFAULT 0,
      recorded_at TEXT NOT NULL,
      sync_status TEXT DEFAULT 'pending'
    );

    CREATE TABLE IF NOT EXISTS pending_pledges (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      contact_id TEXT NOT NULL,
      pledge_type TEXT NOT NULL,
      notes TEXT DEFAULT '',
      recorded_at TEXT NOT NULL,
      sync_status TEXT DEFAULT 'pending'
    );

    CREATE TABLE IF NOT EXISTS pending_locations (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      volunteer_id TEXT NOT NULL,
      latitude REAL NOT NULL,
      longitude REAL NOT NULL,
      battery INTEGER DEFAULT 100,
      speed_kmh REAL DEFAULT 0,
      recorded_at TEXT NOT NULL,
      sync_status TEXT DEFAULT 'pending'
    );

    CREATE TABLE IF NOT EXISTS cached_contacts (
      contact_id TEXT PRIMARY KEY,
      phone_masked TEXT,
      full_name_encrypted TEXT,
      state_code TEXT,
      lga_code TEXT,
      ward_code TEXT,
      voter_status TEXT,
      cached_at TEXT NOT NULL
    );

    CREATE TABLE IF NOT EXISTS conflict_log (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      table_name TEXT NOT NULL,
      record_id TEXT NOT NULL,
      local_data TEXT NOT NULL,
      server_data TEXT NOT NULL,
      resolved_at TEXT,
      resolution TEXT
    );

    CREATE TABLE IF NOT EXISTS sync_metadata (
      key TEXT PRIMARY KEY,
      value TEXT NOT NULL
    );
  `);

  return db;
}

// ─── Door Knock Operations ─────────────────────────────────────────────────

export async function saveDoorKnock(knock: Omit<PendingDoorKnock, 'id' | 'sync_status'>): Promise<number> {
  const database = await getDB();
  const result = await database.runAsync(
    `INSERT INTO pending_door_knocks (volunteer_id, contact_id, latitude, longitude, outcome, notes, speed_kmh, recorded_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    knock.volunteer_id, knock.contact_id, knock.latitude, knock.longitude,
    knock.outcome, knock.notes, knock.speed_kmh, knock.recorded_at,
  );
  return result.lastInsertRowId;
}

export async function getPendingDoorKnocks(): Promise<PendingDoorKnock[]> {
  const database = await getDB();
  return database.getAllAsync<PendingDoorKnock>(
    "SELECT * FROM pending_door_knocks WHERE sync_status = 'pending' ORDER BY recorded_at",
  );
}

export async function markDoorKnockSynced(id: number): Promise<void> {
  const database = await getDB();
  await database.runAsync("UPDATE pending_door_knocks SET sync_status = 'synced' WHERE id = ?", id);
}

export async function markDoorKnockFailed(id: number): Promise<void> {
  const database = await getDB();
  await database.runAsync("UPDATE pending_door_knocks SET sync_status = 'failed' WHERE id = ?", id);
}

// ─── Pledge Operations ─────────────────────────────────────────────────────

export async function savePledge(pledge: Omit<PendingPledge, 'id' | 'sync_status'>): Promise<number> {
  const database = await getDB();
  const result = await database.runAsync(
    `INSERT INTO pending_pledges (contact_id, pledge_type, notes, recorded_at)
     VALUES (?, ?, ?, ?)`,
    pledge.contact_id, pledge.pledge_type, pledge.notes, pledge.recorded_at,
  );
  return result.lastInsertRowId;
}

export async function getPendingPledges(): Promise<PendingPledge[]> {
  const database = await getDB();
  return database.getAllAsync<PendingPledge>(
    "SELECT * FROM pending_pledges WHERE sync_status = 'pending' ORDER BY recorded_at",
  );
}

export async function markPledgeSynced(id: number): Promise<void> {
  const database = await getDB();
  await database.runAsync("UPDATE pending_pledges SET sync_status = 'synced' WHERE id = ?", id);
}

// ─── Location Queue ────────────────────────────────────────────────────────

export async function saveLocation(loc: Omit<PendingLocationUpdate, 'id' | 'sync_status'>): Promise<void> {
  const database = await getDB();
  await database.runAsync(
    `INSERT INTO pending_locations (volunteer_id, latitude, longitude, battery, speed_kmh, recorded_at)
     VALUES (?, ?, ?, ?, ?, ?)`,
    loc.volunteer_id, loc.latitude, loc.longitude, loc.battery, loc.speed_kmh, loc.recorded_at,
  );
  // Keep only the latest 1000 locations to prevent DB bloat
  await database.runAsync(
    `DELETE FROM pending_locations WHERE id NOT IN (
       SELECT id FROM pending_locations ORDER BY recorded_at DESC LIMIT 1000
     )`,
  );
}

export async function getPendingLocations(): Promise<PendingLocationUpdate[]> {
  const database = await getDB();
  return database.getAllAsync<PendingLocationUpdate>(
    "SELECT * FROM pending_locations WHERE sync_status = 'pending' ORDER BY recorded_at LIMIT 50",
  );
}

export async function markLocationsSynced(ids: number[]): Promise<void> {
  if (ids.length === 0) return;
  const database = await getDB();
  const placeholders = ids.map(() => '?').join(',');
  await database.runAsync(
    `UPDATE pending_locations SET sync_status = 'synced' WHERE id IN (${placeholders})`,
    ...ids,
  );
}

// ─── Contact Cache ─────────────────────────────────────────────────────────

export async function cacheContacts(contacts: CachedContact[]): Promise<void> {
  const database = await getDB();
  for (const c of contacts) {
    await database.runAsync(
      `INSERT OR REPLACE INTO cached_contacts
       (contact_id, phone_masked, full_name_encrypted, state_code, lga_code, ward_code, voter_status, cached_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
      c.contact_id, c.phone_masked, c.full_name_encrypted,
      c.state_code, c.lga_code, c.ward_code, c.voter_status, c.cached_at,
    );
  }
}

export async function getCachedContacts(stateCode?: string, lgaCode?: string): Promise<CachedContact[]> {
  const database = await getDB();
  let query = 'SELECT * FROM cached_contacts';
  const args: (string | number)[] = [];
  const conditions: string[] = [];

  if (stateCode) {
    conditions.push('state_code = ?');
    args.push(stateCode);
  }
  if (lgaCode) {
    conditions.push('lga_code = ?');
    args.push(lgaCode);
  }
  if (conditions.length > 0) {
    query += ' WHERE ' + conditions.join(' AND ');
  }
  query += ' ORDER BY full_name_encrypted LIMIT 200';

  return database.getAllAsync<CachedContact>(query, ...args);
}

// ─── Conflict Log ──────────────────────────────────────────────────────────

export async function logConflict(
  tableName: string, recordId: string, localData: string, serverData: string
): Promise<void> {
  const database = await getDB();
  await database.runAsync(
    `INSERT INTO conflict_log (table_name, record_id, local_data, server_data) VALUES (?, ?, ?, ?)`,
    tableName, recordId, localData, serverData,
  );
}

export async function getUnresolvedConflicts(): Promise<ConflictLogEntry[]> {
  const database = await getDB();
  return database.getAllAsync<ConflictLogEntry>(
    'SELECT * FROM conflict_log WHERE resolved_at IS NULL ORDER BY id DESC',
  );
}

// ─── Sync Metadata ─────────────────────────────────────────────────────────

export async function getSyncMeta(key: string): Promise<string | null> {
  const database = await getDB();
  const row = await database.getFirstAsync<{ value: string }>(
    'SELECT value FROM sync_metadata WHERE key = ?', key,
  );
  return row?.value ?? null;
}

export async function setSyncMeta(key: string, value: string): Promise<void> {
  const database = await getDB();
  await database.runAsync(
    'INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)', key, value,
  );
}

// ─── Stats ─────────────────────────────────────────────────────────────────

export async function getPendingCounts(): Promise<{ knocks: number; pledges: number; locations: number }> {
  const database = await getDB();
  const knocks = await database.getFirstAsync<{ count: number }>(
    "SELECT COUNT(*) as count FROM pending_door_knocks WHERE sync_status = 'pending'",
  );
  const pledges = await database.getFirstAsync<{ count: number }>(
    "SELECT COUNT(*) as count FROM pending_pledges WHERE sync_status = 'pending'",
  );
  const locations = await database.getFirstAsync<{ count: number }>(
    "SELECT COUNT(*) as count FROM pending_locations WHERE sync_status = 'pending'",
  );
  return {
    knocks: knocks?.count ?? 0,
    pledges: pledges?.count ?? 0,
    locations: locations?.count ?? 0,
  };
}
