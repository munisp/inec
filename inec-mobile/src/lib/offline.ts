import * as SQLite from 'expo-sqlite';
import NetInfo from '@react-native-community/netinfo';
import { api, getToken } from './api';

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

// Queue biometric verification for offline sync
export async function queueBiometricVerification(verification: {
  voter_id: string; device_serial: string; verification_type: string;
  latitude: number; longitude: number;
}): Promise<number> {
  const database = await getDb();
  const result = await database.runAsync(
    `INSERT INTO pending_biometric_verifications (voter_id, device_serial, verification_type, latitude, longitude)
     VALUES (?, ?, ?, ?, ?)`,
    [verification.voter_id, verification.device_serial, verification.verification_type, verification.latitude, verification.longitude]
  );
  return result.lastInsertRowId;
}

export async function getPendingBiometricCount(): Promise<number> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ count: number }>('SELECT COUNT(*) as count FROM pending_biometric_verifications WHERE synced = 0');
  return row?.count ?? 0;
}

// Get total offline queue size across all tables
export async function getTotalPendingCount(): Promise<{ reports: number; checkins: number; biometrics: number; total: number }> {
  const database = await getDb();
  const reports = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_reports WHERE synced = 0'))?.c ?? 0;
  const checkins = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_checkins WHERE synced = 0'))?.c ?? 0;
  const biometrics = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_biometric_verifications WHERE synced = 0'))?.c ?? 0;
  return { reports, checkins, biometrics, total: reports + checkins + biometrics };
}
