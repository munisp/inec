import * as SQLite from 'expo-sqlite';
import NetInfo from '@react-native-community/netinfo';
import { api, getToken } from './api';
import { encryptField, decryptField } from './crypto';

let db: SQLite.SQLiteDatabase | null = null;

export async function getDb(): Promise<SQLite.SQLiteDatabase> {
  if (!db) {
    db = await SQLite.openDatabaseAsync('inec_observer.db');
    await db.execAsync(`
      CREATE TABLE IF NOT EXISTS pending_reports (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        polling_unit_code TEXT NOT NULL,
        election_id INTEGER NOT NULL,
        report_type TEXT NOT NULL,
        photo_uri TEXT,
        description TEXT,
        latitude REAL,
        longitude REAL,
        created_at TEXT DEFAULT (datetime('now')),
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS pending_checkins (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        polling_unit_code TEXT NOT NULL,
        latitude REAL NOT NULL,
        longitude REAL NOT NULL,
        created_at TEXT DEFAULT (datetime('now')),
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS cached_results (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        party_code TEXT,
        state_code TEXT,
        data TEXT,
        cached_at TEXT DEFAULT (datetime('now'))
      );
      CREATE TABLE IF NOT EXISTS cached_polling_units (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        pu_code TEXT UNIQUE NOT NULL,
        name TEXT,
        state TEXT,
        lga TEXT,
        ward TEXT,
        latitude REAL,
        longitude REAL,
        data TEXT,
        cached_at TEXT DEFAULT (datetime('now'))
      );
      CREATE TABLE IF NOT EXISTS pending_biometric_verifications (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        voter_id TEXT NOT NULL,
        device_serial TEXT,
        verification_type TEXT DEFAULT 'fingerprint',
        result TEXT DEFAULT 'pending',
        latitude REAL,
        longitude REAL,
        created_at TEXT DEFAULT (datetime('now')),
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS cached_election_data (
        key TEXT PRIMARY KEY,
        data TEXT NOT NULL,
        cached_at TEXT DEFAULT (datetime('now'))
      );
    `);
  }
  return db;
}

export async function queueReport(report: {
  polling_unit_code: string;
  election_id: number;
  report_type: string;
  photo_uri: string | null;
  description: string;
  latitude: number;
  longitude: number;
}): Promise<number> {
  const database = await getDb();
  const result = await database.runAsync(
    `INSERT INTO pending_reports (polling_unit_code, election_id, report_type, photo_uri, description, latitude, longitude)
     VALUES (?, ?, ?, ?, ?, ?, ?)`,
    [report.polling_unit_code, report.election_id, report.report_type, report.photo_uri, report.description, report.latitude, report.longitude]
  );
  return result.lastInsertRowId;
}

export async function queueCheckIn(checkIn: {
  polling_unit_code: string;
  latitude: number;
  longitude: number;
}): Promise<number> {
  const database = await getDb();
  const result = await database.runAsync(
    `INSERT INTO pending_checkins (polling_unit_code, latitude, longitude) VALUES (?, ?, ?)`,
    [checkIn.polling_unit_code, checkIn.latitude, checkIn.longitude]
  );
  return result.lastInsertRowId;
}

export interface PendingReport {
  id: number;
  polling_unit_code: string;
  description: string;
  photo_uri: string | null;
  created_at: string;
}

export async function getPendingReports(): Promise<PendingReport[]> {
  const database = await getDb();
  return database.getAllAsync<PendingReport>('SELECT * FROM pending_reports WHERE synced = 0 ORDER BY created_at DESC');
}

export async function savePendingReport(pollingUnitCode: string, description: string, photoUri: string): Promise<number> {
  const database = await getDb();
  const result = await database.runAsync(
    `INSERT INTO pending_reports (polling_unit_code, election_id, report_type, photo_uri, description, latitude, longitude)
     VALUES (?, 0, 'observer', ?, ?, 0, 0)`,
    [pollingUnitCode, photoUri || null, description]
  );
  return result.lastInsertRowId;
}

export async function getPendingReportCount(): Promise<number> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ count: number }>('SELECT COUNT(*) as count FROM pending_reports WHERE synced = 0');
  return row?.count ?? 0;
}

export async function syncPendingData(): Promise<{ reports: number; checkins: number }> {
  const state = await NetInfo.fetch();
  if (!state.isConnected) {
    return { reports: 0, checkins: 0 };
  }

  const token = await getToken();
  if (!token) return { reports: 0, checkins: 0 };

  const database = await getDb();
  let reportsSynced = 0;
  let checkinsSynced = 0;

  // Sync pending check-ins
  const pendingCheckins = await database.getAllAsync<{
    id: number; polling_unit_code: string; latitude: number; longitude: number;
  }>('SELECT * FROM pending_checkins WHERE synced = 0 ORDER BY created_at ASC');

  for (const checkin of pendingCheckins) {
    try {
      await api('/observer/check-in', {
        method: 'POST',
        body: JSON.stringify({
          polling_unit_code: checkin.polling_unit_code,
          latitude: checkin.latitude,
          longitude: checkin.longitude,
        }),
      });
      await database.runAsync('UPDATE pending_checkins SET synced = 1 WHERE id = ?', [checkin.id]);
      checkinsSynced++;
    } catch {
      break; // Stop on first failure, retry later
    }
  }

  // Sync pending reports
  const pendingReports = await database.getAllAsync<{
    id: number; polling_unit_code: string; election_id: number; report_type: string;
    photo_uri: string | null; description: string;
  }>('SELECT * FROM pending_reports WHERE synced = 0 ORDER BY created_at ASC');

  for (const report of pendingReports) {
    try {
      const form = new FormData();
      form.append('polling_unit_code', report.polling_unit_code);
      form.append('election_id', String(report.election_id));
      form.append('notes', report.description || '');
      if (report.photo_uri) {
        const filename = report.photo_uri.split('/').pop() || 'photo.jpg';
        form.append('photo', {
          uri: report.photo_uri,
          name: filename,
          type: 'image/jpeg',
        } as unknown as Blob);
      }
      await api('/observer/reports', { method: 'POST', body: form });
      await database.runAsync('UPDATE pending_reports SET synced = 1 WHERE id = ?', [report.id]);
      reportsSynced++;
    } catch {
      break;
    }
  }

  return { reports: reportsSynced, checkins: checkinsSynced };
}

// Cache polling unit data for offline use
export async function cachePollingUnits(units: Array<{
  pu_code: string; name: string; state: string; lga: string; ward: string;
  latitude: number; longitude: number;
}>): Promise<number> {
  const database = await getDb();
  let cached = 0;
  for (const pu of units) {
    await database.runAsync(
      `INSERT OR REPLACE INTO cached_polling_units (pu_code, name, state, lga, ward, latitude, longitude, data, cached_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
      [pu.pu_code, pu.name, pu.state, pu.lga, pu.ward, pu.latitude, pu.longitude, JSON.stringify(pu)]
    );
    cached++;
  }
  return cached;
}

export async function getCachedPollingUnits(state?: string): Promise<Array<{
  pu_code: string; name: string; state: string; lga: string; ward: string;
  latitude: number; longitude: number;
}>> {
  const database = await getDb();
  const query = state
    ? 'SELECT data FROM cached_polling_units WHERE state = ? ORDER BY pu_code'
    : 'SELECT data FROM cached_polling_units ORDER BY pu_code';
  const rows = await database.getAllAsync<{ data: string }>(query, state ? [state] : []);
  return rows.map(r => JSON.parse(r.data));
}

// Cache election data (results, parties, etc.)
export async function cacheElectionData(key: string, data: unknown): Promise<void> {
  const database = await getDb();
  await database.runAsync(
    `INSERT OR REPLACE INTO cached_election_data (key, data, cached_at) VALUES (?, ?, datetime('now'))`,
    [key, JSON.stringify(data)]
  );
}

export async function getCachedElectionData<T = unknown>(key: string): Promise<T | null> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ data: string }>('SELECT data FROM cached_election_data WHERE key = ?', [key]);
  return row ? JSON.parse(row.data) as T : null;
}

// Queue biometric verification for offline sync (encrypts sensitive voter_id at rest)
export async function queueBiometricVerification(verification: {
  voter_id: string; device_serial: string; verification_type: string;
  latitude: number; longitude: number;
}): Promise<number> {
  const database = await getDb();
  // Encrypt voter_id and device_serial — these are PII/sensitive identifiers
  const encryptedVoterId = await encryptField(verification.voter_id);
  const encryptedDeviceSerial = await encryptField(verification.device_serial);
  const result = await database.runAsync(
    `INSERT INTO pending_biometric_verifications (voter_id, device_serial, verification_type, latitude, longitude)
     VALUES (?, ?, ?, ?, ?)`,
    [encryptedVoterId, encryptedDeviceSerial, verification.verification_type, verification.latitude, verification.longitude]
  );
  return result.lastInsertRowId;
}

// Decrypt biometric verification records for sync
export async function getDecryptedBiometricVerifications(): Promise<Array<{
  id: number; voter_id: string; device_serial: string; verification_type: string;
  latitude: number; longitude: number;
}>> {
  const database = await getDb();
  const rows = await database.getAllAsync<{
    id: number; voter_id: string; device_serial: string; verification_type: string;
    latitude: number; longitude: number;
  }>('SELECT * FROM pending_biometric_verifications WHERE synced = 0 ORDER BY created_at ASC');

  return Promise.all(rows.map(async (row) => ({
    ...row,
    voter_id: await decryptField(row.voter_id),
    device_serial: await decryptField(row.device_serial),
  })));
}

export async function getPendingBiometricCount(): Promise<number> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ count: number }>('SELECT COUNT(*) as count FROM pending_biometric_verifications WHERE synced = 0');
  return row?.count ?? 0;
}

// ── Offline Conflict Resolution ──
// Detects and resolves conflicts when server has newer data than locally queued changes.
// Uses timestamp-based comparison: server version wins if newer, local version prompts merge.

export interface ConflictRecord {
  table: string;
  localId: number;
  localTimestamp: string;
  serverTimestamp: string;
  resolution: 'server_wins' | 'local_wins' | 'merged';
  details: string;
}

export type ConflictStrategy = 'server_wins' | 'local_wins' | 'latest_wins';

/**
 * Sync pending reports with conflict detection.
 * Compares local created_at with server's last_modified to detect conflicts.
 */
export async function syncWithConflictResolution(
  strategy: ConflictStrategy = 'latest_wins'
): Promise<{ synced: number; conflicts: ConflictRecord[]; skipped: number }> {
  const state = await NetInfo.fetch();
  if (!state.isConnected) {
    return { synced: 0, conflicts: [], skipped: 0 };
  }

  const token = await getToken();
  if (!token) return { synced: 0, conflicts: [], skipped: 0 };

  const database = await getDb();

  // Ensure conflict log table exists
  await database.execAsync(`
    CREATE TABLE IF NOT EXISTS conflict_log (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      table_name TEXT NOT NULL,
      local_id INTEGER NOT NULL,
      local_timestamp TEXT,
      server_timestamp TEXT,
      resolution TEXT NOT NULL,
      details TEXT,
      resolved_at TEXT DEFAULT (datetime('now'))
    )
  `);

  let synced = 0;
  let skipped = 0;
  const conflicts: ConflictRecord[] = [];

  // Sync pending reports with conflict checking
  const pendingReports = await database.getAllAsync<{
    id: number; polling_unit_code: string; election_id: number; report_type: string;
    description: string; created_at: string;
  }>('SELECT * FROM pending_reports WHERE synced = 0 ORDER BY created_at ASC');

  for (const report of pendingReports) {
    try {
      // Check if server already has a report for this PU + election
      const serverCheck = await api(`/observer/reports/check?pu_code=${report.polling_unit_code}&election_id=${report.election_id}`);
      const serverData = serverCheck as { exists: boolean; last_modified?: string };

      if (serverData.exists && serverData.last_modified) {
        const localTime = new Date(report.created_at).getTime();
        const serverTime = new Date(serverData.last_modified).getTime();

        if (serverTime > localTime) {
          // Server has newer version — apply conflict strategy
          let resolution: ConflictRecord['resolution'];
          if (strategy === 'server_wins' || (strategy === 'latest_wins' && serverTime > localTime)) {
            resolution = 'server_wins';
            await database.runAsync('UPDATE pending_reports SET synced = 2 WHERE id = ?', [report.id]);
            skipped++;
          } else {
            resolution = 'local_wins';
            // Force-push local version
            await api('/observer/reports', {
              method: 'POST',
              body: JSON.stringify({
                polling_unit_code: report.polling_unit_code,
                election_id: report.election_id,
                notes: report.description,
                force: true,
              }),
            });
            await database.runAsync('UPDATE pending_reports SET synced = 1 WHERE id = ?', [report.id]);
            synced++;
          }

          const conflict: ConflictRecord = {
            table: 'pending_reports',
            localId: report.id,
            localTimestamp: report.created_at,
            serverTimestamp: serverData.last_modified,
            resolution,
            details: `PU ${report.polling_unit_code}, election ${report.election_id}`,
          };
          conflicts.push(conflict);

          // Log conflict to local DB
          await database.runAsync(
            `INSERT INTO conflict_log (table_name, local_id, local_timestamp, server_timestamp, resolution, details) VALUES (?, ?, ?, ?, ?, ?)`,
            [conflict.table, conflict.localId, conflict.localTimestamp, conflict.serverTimestamp, conflict.resolution, conflict.details]
          );
          continue;
        }
      }

      // No conflict — normal sync
      await api('/observer/reports', {
        method: 'POST',
        body: JSON.stringify({
          polling_unit_code: report.polling_unit_code,
          election_id: report.election_id,
          notes: report.description,
        }),
      });
      await database.runAsync('UPDATE pending_reports SET synced = 1 WHERE id = ?', [report.id]);
      synced++;
    } catch {
      break;
    }
  }

  return { synced, conflicts, skipped };
}

/**
 * Get conflict history from local database.
 */
export async function getConflictHistory(): Promise<ConflictRecord[]> {
  const database = await getDb();
  try {
    const rows = await database.getAllAsync<{
      table_name: string; local_id: number; local_timestamp: string;
      server_timestamp: string; resolution: string; details: string;
    }>('SELECT * FROM conflict_log ORDER BY resolved_at DESC LIMIT 100');
    return rows.map(r => ({
      table: r.table_name,
      localId: r.local_id,
      localTimestamp: r.local_timestamp,
      serverTimestamp: r.server_timestamp,
      resolution: r.resolution as ConflictRecord['resolution'],
      details: r.details,
    }));
  } catch {
    return [];
  }
}

// Get total offline queue size across all tables
export async function getTotalPendingCount(): Promise<{ reports: number; checkins: number; biometrics: number; total: number }> {
  const database = await getDb();
  const reports = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_reports WHERE synced = 0'))?.c ?? 0;
  const checkins = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_checkins WHERE synced = 0'))?.c ?? 0;
  const biometrics = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_biometric_verifications WHERE synced = 0'))?.c ?? 0;
  return { reports, checkins, biometrics, total: reports + checkins + biometrics };
}
