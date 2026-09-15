/**
 * Offline outbox for the INEC observer/presiding-officer app.
 *
 * Design notes (gap-audit fixes):
 *  - R5-107: observer reports are ALWAYS queued with the resolved
 *    election_id — the `election_id = 0` sentinel is gone, and the sync
 *    payload uses the backend's real field names (`description`, not
 *    `notes`).
 *  - R5-007: the drain is skip-and-continue. A failed item accrues retry
 *    state (attempts/last_error) and never blocks the rest of the queue;
 *    exhausted items stay parked for manual review instead of wedging the
 *    outbox head-of-line or being silently dropped.
 *  - R5-109: the old `syncWithConflictResolution` (which called the
 *    non-existent GET /observer/reports/check and POSTed a `force` flag no
 *    handler reads) is removed. Conflict handling consumes the REAL
 *    per-item outcome protocol returned by the W1 sync endpoint
 *    (items[]: {index,status,job_id,idempotency_key,error}).
 *  - R5-110: photos are copied out of the volatile camera cache into
 *    app-owned storage at capture time and SHA-256 hashed; the hash is
 *    re-verified before upload so a swapped/evicted photo fails loudly.
 *  - R5-111: the write-only biometric queue (no enqueue caller, no drain)
 *    is removed.
 *  - R5-115: queue timestamps use the server-corrected clock
 *    (clock-sync.ts), not the raw device wall-clock.
 */
import * as SQLite from 'expo-sqlite';
import NetInfo from '@react-native-community/netinfo';
// Legacy API surface (documentDirectory/readAsStringAsync) — the new
// File/Directory API in expo-file-system@56 lacks base64 read helpers used
// for SHA-256 pinning.
import * as FileSystem from 'expo-file-system/legacy';
import * as Crypto from 'expo-crypto';
import { api, getToken } from './api';
import { correctedNowIso } from './clock-sync';
import { mapItemOutcomes, nextRetryState, shouldAttempt, type SyncItemOutcome } from './outbox';

const MAX_ATTEMPTS = 10;

let db: SQLite.SQLiteDatabase | null = null;

/** Add a column to an existing table if it is missing (idempotent migration). */
async function ensureColumn(
  database: SQLite.SQLiteDatabase,
  table: string,
  column: string,
  ddl: string,
): Promise<void> {
  const cols = await database.getAllAsync<{ name: string }>(`PRAGMA table_info(${table})`);
  if (!cols.some((c) => c.name === column)) {
    await database.execAsync(`ALTER TABLE ${table} ADD COLUMN ${ddl}`);
  }
}

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
        photo_sha256 TEXT,
        description TEXT,
        latitude REAL,
        longitude REAL,
        created_at TEXT DEFAULT (datetime('now')),
        attempts INTEGER DEFAULT 0,
        last_error TEXT,
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS pending_checkins (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        polling_unit_code TEXT NOT NULL,
        latitude REAL NOT NULL,
        longitude REAL NOT NULL,
        created_at TEXT DEFAULT (datetime('now')),
        attempts INTEGER DEFAULT 0,
        last_error TEXT,
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS pending_results (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        election_id INTEGER NOT NULL,
        polling_unit_code TEXT NOT NULL,
        party_scores TEXT NOT NULL,
        accredited_voters INTEGER DEFAULT 0,
        rejected_votes INTEGER DEFAULT 0,
        latitude REAL,
        longitude REAL,
        idempotency_key TEXT UNIQUE NOT NULL,
        created_at TEXT NOT NULL,
        attempts INTEGER DEFAULT 0,
        last_error TEXT,
        synced INTEGER DEFAULT 0
      );
      CREATE TABLE IF NOT EXISTS conflict_log (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        table_name TEXT NOT NULL,
        local_id INTEGER NOT NULL,
        local_timestamp TEXT,
        server_timestamp TEXT,
        resolution TEXT NOT NULL,
        details TEXT,
        resolved_at TEXT DEFAULT (datetime('now'))
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
      CREATE TABLE IF NOT EXISTS cached_election_data (
        key TEXT PRIMARY KEY,
        data TEXT NOT NULL,
        cached_at TEXT DEFAULT (datetime('now'))
      );
    `);
    // Migrate pre-fix databases forward.
    await ensureColumn(db, 'pending_reports', 'photo_sha256', 'photo_sha256 TEXT');
    await ensureColumn(db, 'pending_reports', 'attempts', 'attempts INTEGER DEFAULT 0');
    await ensureColumn(db, 'pending_reports', 'last_error', 'last_error TEXT');
    await ensureColumn(db, 'pending_checkins', 'attempts', 'attempts INTEGER DEFAULT 0');
    await ensureColumn(db, 'pending_checkins', 'last_error', 'last_error TEXT');
    // R5-107: reports queued by the old sentinel (election_id = 0) can never
    // sync — park them (synced = 2) so they stop poisoning every drain.
    await db.runAsync('UPDATE pending_reports SET synced = 2, last_error = ? WHERE election_id <= 0 AND synced = 0',
      ['parked: queued without election_id by pre-fix build']);
    // R5-111: the biometric queue was write-only dead code (no enqueuer, no
    // drainer). Drop the table so its (obfuscated) PII does not linger.
    await db.execAsync('DROP TABLE IF EXISTS pending_biometric_verifications');
  }
  return db;
}

// ── Photo evidence persistence + integrity (R5-110) ──

const PHOTO_DIR = `${FileSystem.documentDirectory}ec8a/`;

/**
 * Move a freshly captured photo out of the volatile camera cache into
 * app-owned storage and compute its SHA-256. Returns null if the source
 * file is unreadable (caller must not queue a report without evidence).
 */
export async function persistCapturedPhoto(
  cacheUri: string,
): Promise<{ uri: string; sha256: string } | null> {
  try {
    const base64 = await FileSystem.readAsStringAsync(cacheUri, {
      encoding: FileSystem.EncodingType.Base64,
    });
    const sha256 = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, base64);
    await FileSystem.makeDirectoryAsync(PHOTO_DIR, { intermediates: true }).catch(() => {});
    const dest = `${PHOTO_DIR}${sha256}.jpg`;
    await FileSystem.writeAsStringAsync(dest, base64, {
      encoding: FileSystem.EncodingType.Base64,
    });
    return { uri: dest, sha256 };
  } catch {
    return null;
  }
}

/**
 * Re-verify a queued photo before upload: the file must still exist and its
 * SHA-256 must match the value recorded at capture. Returns an error string
 * on failure (item is skipped, never uploaded silently).
 */
export async function verifyQueuedPhoto(uri: string, expectedSha256: string | null): Promise<string | null> {
  const info = await FileSystem.getInfoAsync(uri);
  if (!info.exists) return 'photo file missing (evicted from device storage)';
  if (!expectedSha256) return null; // queued by an older build without a hash
  try {
    const base64 = await FileSystem.readAsStringAsync(uri, {
      encoding: FileSystem.EncodingType.Base64,
    });
    const actual = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, base64);
    if (actual !== expectedSha256) return 'photo hash mismatch (file changed after capture)';
    return null;
  } catch {
    return 'photo unreadable';
  }
}

// ── Observer report queue ──

export async function queueReport(report: {
  polling_unit_code: string;
  election_id: number;
  report_type?: string;
  photo_uri: string | null;
  photo_sha256?: string | null;
  description: string;
  latitude: number;
  longitude: number;
}): Promise<number> {
  // R5-107: refuse to queue without a real election id (no sentinel).
  if (!Number.isInteger(report.election_id) || report.election_id <= 0) {
    throw new Error('queueReport: a resolved election_id (> 0) is required');
  }
  const database = await getDb();
  const result = await database.runAsync(
    `INSERT INTO pending_reports
       (polling_unit_code, election_id, report_type, photo_uri, photo_sha256, description, latitude, longitude, created_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    [
      report.polling_unit_code,
      report.election_id,
      report.report_type || 'result_photo',
      report.photo_uri,
      report.photo_sha256 ?? null,
      report.description,
      report.latitude,
      report.longitude,
      correctedNowIso(),
    ]
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
    `INSERT INTO pending_checkins (polling_unit_code, latitude, longitude, created_at) VALUES (?, ?, ?, ?)`,
    [checkIn.polling_unit_code, checkIn.latitude, checkIn.longitude, correctedNowIso()]
  );
  return result.lastInsertRowId;
}

export interface PendingReport {
  id: number;
  polling_unit_code: string;
  election_id: number;
  report_type: string;
  description: string;
  photo_uri: string | null;
  photo_sha256: string | null;
  attempts: number;
  last_error: string | null;
  created_at: string;
}

export async function getPendingReports(): Promise<PendingReport[]> {
  const database = await getDb();
  return database.getAllAsync<PendingReport>(
    'SELECT * FROM pending_reports WHERE synced = 0 ORDER BY created_at DESC'
  );
}

export async function getPendingReportCount(): Promise<number> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ count: number }>(
    'SELECT COUNT(*) as count FROM pending_reports WHERE synced = 0'
  );
  return row?.count ?? 0;
}

async function recordFailure(
  database: SQLite.SQLiteDatabase,
  table: 'pending_reports' | 'pending_checkins' | 'pending_results',
  id: number,
  currentAttempts: number,
  message: string,
): Promise<void> {
  const retry = nextRetryState(currentAttempts, MAX_ATTEMPTS);
  await database.runAsync(
    `UPDATE ${table} SET attempts = ?, last_error = ? WHERE id = ?`,
    [retry.attempts, retry.exhausted ? `EXHAUSTED: ${message}` : message, id]
  );
}

// ── Drain: check-ins + observer reports (skip-and-continue, R5-007) ──

export interface SyncSummary {
  reports: number;
  checkins: number;
  reportFailures: number;
  checkinFailures: number;
}

export async function syncPendingData(): Promise<SyncSummary> {
  const zero: SyncSummary = { reports: 0, checkins: 0, reportFailures: 0, checkinFailures: 0 };
  const state = await NetInfo.fetch();
  if (!state.isConnected) return zero;

  const token = await getToken();
  if (!token) return zero;

  const database = await getDb();
  let reportsSynced = 0;
  let checkinsSynced = 0;
  let reportFailures = 0;
  let checkinFailures = 0;

  // Pending check-ins
  const pendingCheckins = await database.getAllAsync<{
    id: number; polling_unit_code: string; latitude: number; longitude: number; attempts: number;
  }>('SELECT * FROM pending_checkins WHERE synced = 0 ORDER BY created_at ASC');

  for (const checkin of pendingCheckins) {
    if (!shouldAttempt(checkin.attempts, MAX_ATTEMPTS)) continue;
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
    } catch (e) {
      // Skip-and-continue: a failed item must not block the rest of the queue.
      checkinFailures++;
      await recordFailure(database, 'pending_checkins', checkin.id, checkin.attempts, String(e));
    }
  }

  // Pending observer reports
  const pendingReports = await database.getAllAsync<{
    id: number; polling_unit_code: string; election_id: number; report_type: string;
    photo_uri: string | null; photo_sha256: string | null; description: string; attempts: number;
  }>('SELECT * FROM pending_reports WHERE synced = 0 ORDER BY created_at ASC');

  for (const report of pendingReports) {
    if (!shouldAttempt(report.attempts, MAX_ATTEMPTS)) continue;
    if (report.election_id <= 0) {
      // Defensive: never submit a sentinel election id.
      await database.runAsync(
        'UPDATE pending_reports SET synced = 2, last_error = ? WHERE id = ?',
        ['parked: missing election_id', report.id]
      );
      continue;
    }
    try {
      if (report.photo_uri) {
        const photoError = await verifyQueuedPhoto(report.photo_uri, report.photo_sha256);
        if (photoError) {
          reportFailures++;
          await recordFailure(database, 'pending_reports', report.id, report.attempts, photoError);
          continue;
        }
      }
      const form = new FormData();
      form.append('polling_unit_code', report.polling_unit_code);
      form.append('election_id', String(report.election_id));
      form.append('report_type', report.report_type || 'result_photo');
      // R5-107: the backend reads `description`, not `notes`.
      form.append('description', report.description || '');
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
    } catch (e) {
      reportFailures++;
      await recordFailure(database, 'pending_reports', report.id, report.attempts, String(e));
    }
  }

  return { reports: reportsSynced, checkins: checkinsSynced, reportFailures, checkinFailures };
}

// ── Result-capture outbox (R5-106) ──

export interface PendingResult {
  id: number;
  election_id: number;
  polling_unit_code: string;
  party_scores: string; // JSON: Array<{ party_code, votes }>
  accredited_voters: number;
  rejected_votes: number;
  latitude: number | null;
  longitude: number | null;
  idempotency_key: string;
  created_at: string;
  attempts: number;
  last_error: string | null;
  synced: number;
}

export async function queueResult(result: {
  election_id: number;
  polling_unit_code: string;
  party_scores: Array<{ party_code: string; votes: number }>;
  accredited_voters: number;
  rejected_votes: number;
  latitude: number | null;
  longitude: number | null;
}): Promise<number> {
  if (!Number.isInteger(result.election_id) || result.election_id <= 0) {
    throw new Error('queueResult: a resolved election_id (> 0) is required');
  }
  if (!result.party_scores.length) {
    throw new Error('queueResult: at least one party score is required');
  }
  const database = await getDb();
  const inserted = await database.runAsync(
    `INSERT INTO pending_results
       (election_id, polling_unit_code, party_scores, accredited_voters, rejected_votes,
        latitude, longitude, idempotency_key, created_at)
     VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
    [
      result.election_id,
      result.polling_unit_code,
      JSON.stringify(result.party_scores),
      result.accredited_voters,
      result.rejected_votes,
      result.latitude,
      result.longitude,
      Crypto.randomUUID(),
      correctedNowIso(),
    ]
  );
  return inserted.lastInsertRowId;
}

export async function getPendingResults(): Promise<PendingResult[]> {
  const database = await getDb();
  return database.getAllAsync<PendingResult>(
    'SELECT * FROM pending_results WHERE synced = 0 ORDER BY created_at ASC'
  );
}

export async function getPendingResultCount(): Promise<number> {
  const database = await getDb();
  const row = await database.getFirstAsync<{ count: number }>(
    'SELECT COUNT(*) as count FROM pending_results WHERE synced = 0'
  );
  return row?.count ?? 0;
}

export interface ResultSyncSummary {
  synced: number;
  duplicates: number;
  conflicts: number;
  failed: number;
}

interface OfflineSyncResponse {
  items?: SyncItemOutcome[];
}

/**
 * Resolve a registered BVAS device id for the PU — the W1 offline-sync
 * endpoint rejects unregistered device ids. Returns null when no device is
 * registered (caller falls back to the per-item /results/submit path).
 */
async function resolveSyncDeviceId(electionId: number, puCode: string): Promise<string | null> {
  try {
    const devices = await api<Array<{ id: number | string }>>(
      `/bvas/devices?election_id=${electionId}&polling_unit_code=${encodeURIComponent(puCode)}&limit=1`
    );
    if (Array.isArray(devices) && devices.length > 0 && devices[0].id != null) {
      return String(devices[0].id);
    }
  } catch { /* fall back to per-item submission */ }
  return null;
}

async function parkConflict(
  database: SQLite.SQLiteDatabase,
  row: PendingResult,
  error?: string,
): Promise<void> {
  await database.runAsync('UPDATE pending_results SET synced = 2, last_error = ? WHERE id = ?',
    [`conflict: ${error ?? 'server has a newer version'}`, row.id]);
  await database.runAsync(
    `INSERT INTO conflict_log (table_name, local_id, local_timestamp, resolution, details)
     VALUES ('pending_results', ?, ?, 'pending_review', ?)`,
    [row.id, row.created_at,
     `PU ${row.polling_unit_code}, election ${row.election_id}${error ? ` — ${error}` : ''}`]
  );
}

/**
 * Drain the result-capture outbox.
 *
 * Preferred path: batch through the W1 offline-sync endpoint and consume the
 * per-item outcome protocol (queued/duplicate/rejected/conflict). When no
 * registered BVAS device exists for the PU, fall back to per-item
 * /results/submit with the stored idempotency key (server returns
 * duplicate:true on replay — R5-025).
 */
export async function syncPendingResults(): Promise<ResultSyncSummary> {
  const summary: ResultSyncSummary = { synced: 0, duplicates: 0, conflicts: 0, failed: 0 };
  const state = await NetInfo.fetch();
  if (!state.isConnected) return summary;
  const token = await getToken();
  if (!token) return summary;

  const database = await getDb();
  const rows = (await getPendingResults()).filter((r) => shouldAttempt(r.attempts, MAX_ATTEMPTS));
  if (!rows.length) return summary;

  // Group by PU + election: one offline-sync batch per registered device.
  const groups = new Map<string, PendingResult[]>();
  for (const row of rows) {
    const key = `${row.election_id}:${row.polling_unit_code}`;
    const group = groups.get(key) ?? [];
    group.push(row);
    groups.set(key, group);
  }

  for (const group of groups.values()) {
    const first = group[0];
    const deviceId = await resolveSyncDeviceId(first.election_id, first.polling_unit_code);

    if (deviceId) {
      try {
        const items = group.map((row) => ({
          payload: {
            election_id: row.election_id,
            polling_unit_code: row.polling_unit_code,
            party_scores: JSON.parse(row.party_scores),
            accredited_voters: row.accredited_voters,
            rejected_votes: row.rejected_votes,
            device_lat: row.latitude,
            device_lng: row.longitude,
          },
          timestamp: row.created_at,
          idempotency_key: row.idempotency_key,
        }));
        const resp = await api<OfflineSyncResponse>('/ingestion/offline-sync', {
          method: 'POST',
          body: JSON.stringify({ device_id: deviceId, sync_type: 'result', items }),
        });
        for (const action of mapItemOutcomes(resp.items, group.length)) {
          const row = group[action.index];
          switch (action.action) {
            case 'mark_synced':
              await database.runAsync('UPDATE pending_results SET synced = 1 WHERE id = ?', [row.id]);
              summary.synced++;
              break;
            case 'mark_duplicate':
              await database.runAsync('UPDATE pending_results SET synced = 1, last_error = ? WHERE id = ?',
                ['duplicate (already on server)', row.id]);
              summary.duplicates++;
              break;
            case 'mark_conflict':
              await parkConflict(database, row, action.error);
              summary.conflicts++;
              break;
            default:
              summary.failed++;
              await recordFailure(database, 'pending_results', row.id, row.attempts, action.error ?? 'rejected');
          }
        }
        continue;
      } catch (e) {
        // Batch transport failed — fall through to per-item submission.
        if (String(e).startsWith('Error: 401') || String(e).startsWith('Error: 403')) {
          summary.failed += group.length;
          for (const row of group) {
            await recordFailure(database, 'pending_results', row.id, row.attempts, String(e));
          }
          continue;
        }
      }
    }

    // Per-item fallback: /results/submit (idempotent via stored key).
    for (const row of group) {
      try {
        const resp = await api<{ id?: number; duplicate?: boolean }>('/results/submit', {
          method: 'POST',
          body: JSON.stringify({
            election_id: row.election_id,
            polling_unit_code: row.polling_unit_code,
            party_scores: JSON.parse(row.party_scores),
            accredited_voters: row.accredited_voters,
            rejected_votes: row.rejected_votes,
            device_lat: row.latitude,
            device_lng: row.longitude,
            idempotency_key: row.idempotency_key,
          }),
        });
        if (resp.duplicate) {
          await database.runAsync('UPDATE pending_results SET synced = 1, last_error = ? WHERE id = ?',
            ['duplicate (already on server)', row.id]);
          summary.duplicates++;
        } else {
          await database.runAsync('UPDATE pending_results SET synced = 1 WHERE id = ?', [row.id]);
          summary.synced++;
        }
      } catch (e) {
        const message = String(e);
        if (message.startsWith('Error: 409')) {
          await parkConflict(database, row, message);
          summary.conflicts++;
        } else {
          summary.failed++;
          await recordFailure(database, 'pending_results', row.id, row.attempts, message);
        }
      }
    }
  }

  return summary;
}

// ── Caches ──

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

// ── Conflict history (populated by the outbox drains above) ──

export interface ConflictRecord {
  table: string;
  localId: number;
  localTimestamp: string;
  serverTimestamp: string;
  resolution: 'server_wins' | 'local_wins' | 'merged' | 'pending_review';
  details: string;
}

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
export async function getTotalPendingCount(): Promise<{ reports: number; checkins: number; results: number; total: number }> {
  const database = await getDb();
  const reports = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_reports WHERE synced = 0'))?.c ?? 0;
  const checkins = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_checkins WHERE synced = 0'))?.c ?? 0;
  const results = (await database.getFirstAsync<{ c: number }>('SELECT COUNT(*) as c FROM pending_results WHERE synced = 0'))?.c ?? 0;
  return { reports, checkins, results, total: reports + checkins + results };
}
