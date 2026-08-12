import { describe, it, expect } from "vitest";
import { getDb } from "./db";

// SECURITY: never hardcode database credentials in the repo. The connection
// string must come from the environment; the test is skipped when no database
// is configured (e.g. CI without a Postgres service).
const connectionUrl = process.env.POSTGRES_URL || process.env.DATABASE_URL;
const describeWithDb = connectionUrl ? describe : describe.skip;

describeWithDb("PostgreSQL connection", () => {
  it("connects to the configured PostgreSQL database and can run a query", async () => {
    const db = await getDb();
    expect(db).not.toBeNull();
    // A simple query that should always succeed if the DB is up
    const result = await db!.execute("SELECT 1 AS ok");
    expect(result.rows[0]).toMatchObject({ ok: 1 });
  });
});
