// ─── Real-Postgres scratch database helper for W11 tests ─────────────────────
// Creates a fresh database on the sandbox pgserver, applies the drizzle
// migrations in order, and returns a connection string. Tests SKIP (not fail)
// when no pgserver is reachable, matching the repo's PG-gated test convention.
import { Pool } from "pg";
import { readFileSync, readdirSync } from "fs";
import path from "path";

const ADMIN_DSN =
  process.env.W11_PG_ADMIN_DSN ??
  "host=/home/kimi/pgdata user=postgres dbname=postgres sslmode=disable";

export async function createScratchDb(tag: string): Promise<string | null> {
  const admin = new Pool({ connectionString: ADMIN_DSN });
  const name = `w11_${tag}_${Date.now().toString(36)}`;
  try {
    await admin.query(`DROP DATABASE IF EXISTS ${name}`);
    await admin.query(`CREATE DATABASE ${name}`);
  } catch {
    await admin.end().catch(() => {});
    return null; // no pgserver — caller skips
  }
  await admin.end();

  const dsn = ADMIN_DSN.replace("dbname=postgres", `dbname=${name}`);
  const pool = new Pool({ connectionString: dsn });
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
