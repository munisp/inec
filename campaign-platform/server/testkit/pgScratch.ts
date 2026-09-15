// ─── Real-Postgres scratch database helper for W11 tests ─────────────────────
// Creates a fresh database on the sandbox pgserver, applies the drizzle
// migrations in order, and returns a connection string. Tests SKIP (not fail)
// when no pgserver is reachable, matching the repo's PG-gated test convention.
import { Pool } from "pg";
import { readFileSync, readdirSync } from "fs";
import path from "path";

// pgserver socket directory. A leading '/' host is treated as a unix-socket
// directory by node-postgres — no TCP needed.
const PG_HOST = process.env.W11_PG_HOST ?? "/home/kimi/pgdata";
const PG_USER = process.env.W11_PG_USER ?? "postgres";

function cfg(database: string) {
  return { host: PG_HOST, user: PG_USER, database };
}

export async function createScratchDb(tag: string): Promise<string | null> {
  const admin = new Pool(cfg("postgres"));
  const name = `w11_${tag}_${Date.now().toString(36)}`;
  try {
    await admin.query(`DROP DATABASE IF EXISTS ${name}`);
    await admin.query(`CREATE DATABASE ${name}`);
  } catch {
    await admin.end().catch(() => {});
    return null; // no pgserver — caller skips
  }
  await admin.end();

  // URI form with percent-encoded socket dir as host — parseable by
  // pg-connection-string, so it works both for Pool({connectionString}) and
  // for server/db.ts getDb() via POSTGRES_URL.
  const dsn = `postgresql://${PG_USER}@${encodeURIComponent(PG_HOST)}/${name}`;
  const pool = new Pool(cfg(name));
  try {
    const dir = path.resolve(import.meta.dirname, "..", "..", "drizzle");
    const files = readdirSync(dir)
      .filter((f) => /^\d{4}_.*\.sql$/.test(f))
      .sort();
    for (const f of files) {
      const sql = readFileSync(path.join(dir, f), "utf8");
      await pool.query(sql);
    }
  } finally {
    await pool.end();
  }
  return dsn;
}



/** Seed a user + candidate profile + campaign_membership; returns ids. */
export async function seedProfile(
  dsn: string,
  opts: { username?: string; memberRole?: "owner" | "manager" | "viewer"; office?: string } = {},
): Promise<{ userId: number; profileId: number }> {
  const pool = new Pool({ connectionString: dsn });
  try {
    const username = opts.username ?? "w11-user";
    const u = await pool.query(
      `INSERT INTO users (username, password_hash, full_name, role) VALUES ($1, 'x', 'W11 User', 'user')
       ON CONFLICT (username) DO UPDATE SET full_name=excluded.full_name RETURNING id`,
      [username],
    );
    const userId: number = u.rows[0].id;
    const p = await pool.query(
      `INSERT INTO candidate_profiles (user_id, candidate_name, office) VALUES ($1, 'W11 Candidate', $2) RETURNING id`,
      [userId, opts.office ?? "Governor"],
    );
    const profileId: number = p.rows[0].id;
    await pool.query(
      `INSERT INTO campaign_members (profile_id, user_id, name, email, role) VALUES ($1, $2, 'W11 User', $3, $4)`,
      [profileId, userId, `${username}@example.test`, opts.memberRole ?? "owner"],
    );
    return { userId, profileId };
  } finally {
    await pool.end();
  }
}
