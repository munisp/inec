// ─── Programmable fake `pg` module for unit tests ────────────────────────────
// Wire it up at the top of a test file (before importing server modules):
//
//   import { vi } from "vitest";
//   vi.mock("pg", async () => await import("./testkit/fakePg"));
//   import { fakePgState } from "./testkit/fakePg";
//
// then route SQL → rows with `fakePgState.handler`. drizzle-orm calls
// pool.query(text, params) positionally; the same mock also satisfies
// drizzle's `instanceof Pool` checks because vitest applies the mock to every
// importer of "pg".
//
// Set process.env.POSTGRES_URL before the first db call so getDb() builds the
// (fake) pool instead of returning null.

export type FakeQueryResult = { rows: Array<Record<string, unknown>> };
export type FakeQueryHandler = (text: string, params: unknown[]) => FakeQueryResult;

export const fakePgState: {
  handler: FakeQueryHandler;
  queries: Array<{ text: string; params: unknown[] }>;
  reset: () => void;
} = {
  handler: () => ({ rows: [] }),
  queries: [],
  reset() {
    this.handler = () => ({ rows: [] });
    this.queries = [];
  },
};

// ─── Row shape adaptation ────────────────────────────────────────────────────
// drizzle-orm issues queries with rowMode:"array" and maps result rows
// POSITIONALLY (row[columnIndex] in mapResultRow), so the fake must return
// arrays in SELECT/RETURNING column order. Handlers may return either:
//   - arrays (already positional — passed through untouched), or
//   - objects keyed by column name (converted using the column list parsed
//     from the SQL text).

function splitTopLevel(list: string): string[] {
  // Split on commas that are not inside parentheses (expressions like
  // count(*)::int stay intact).
  const parts: string[] = [];
  let depth = 0;
  let current = "";
  for (const ch of list) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === "," && depth === 0) {
      parts.push(current);
      current = "";
    } else {
      current += ch;
    }
  }
  if (current.trim()) parts.push(current);
  return parts.map(p => p.trim());
}

function columnName(item: string): string {
  const alias = / as "([^"]+)"/i.exec(item);
  if (alias) return alias[1];
  const quotedRe = /"([^"]+)"/g;
  let quoted: string | null = null;
  let m: RegExpExecArray | null;
  while ((m = quotedRe.exec(item)) !== null) quoted = m[1];
  if (quoted !== null) return quoted;
  return item.trim(); // expression column (e.g. count(*)::int)
}

function extractColumns(text: string): string[] | null {
  const lower = text.toLowerCase();
  if (lower.startsWith("select")) {
    const fromIdx = lower.indexOf(" from ");
    if (fromIdx === -1) return null;
    return splitTopLevel(text.slice("select".length, fromIdx)).map(columnName);
  }
  const retIdx = lower.indexOf(" returning ");
  if (retIdx !== -1) {
    return splitTopLevel(text.slice(retIdx + " returning ".length)).map(columnName);
  }
  return null;
}

function toArrayRows(rows: Array<Record<string, unknown>>, text: string) {
  if (rows.length === 0 || Array.isArray(rows[0])) return rows;
  const columns = extractColumns(text);
  if (!columns) return rows;
  return rows.map(row => columns.map(col => row[col]));
}

export class Pool {
  constructor(_config?: unknown) {}
  // drizzle-orm calls client.query(queryConfig, params) where queryConfig is
  // a pg QueryConfig object ({ name?, text, rowMode?, ... }); accept a plain
  // string too.
  query(config: string | { text: string }, params: unknown[] = []) {
    const text = typeof config === "string" ? config : config.text;
    fakePgState.queries.push({ text, params });
    const result = fakePgState.handler(text, params);
    return Promise.resolve({ ...result, rows: toArrayRows(result.rows, text) });
  }
  // Transaction support: drizzle calls pool.connect() and runs
  // BEGIN/COMMIT/ROLLBACK on the client. The fake routes those statements
  // through the same programmable handler (they record as queries so tests
  // can assert on transactional boundaries) — isolation semantics are out of
  // scope for a test double.
  connect() {
    const client = {
      query: (config: string | { text: string }, params: unknown[] = []) => {
        const text = typeof config === "string" ? config : config.text;
        fakePgState.queries.push({ text, params });
        const result = fakePgState.handler(text, params);
        return Promise.resolve({ ...result, rows: toArrayRows(result.rows, text) });
      },
      release: () => {},
    };
    return Promise.resolve(client);
  }
  on() {
    return this;
  }
  end() {
    return Promise.resolve();
  }
}

export default { Pool };
