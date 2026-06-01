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
