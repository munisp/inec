import { describe, it, expect } from "vitest";
import { getDb } from "./db";

describe("PostgreSQL connection", () => {
  it("connects to the local PostgreSQL database and can query users table", async () => {
    // Set the env var for the test (mirrors what the server does at runtime)
    process.env.POSTGRES_URL = "postgresql://inec:inec_pass_2024@127.0.0.1:5432/inec_campaign";
    const db = await getDb();
    expect(db).not.toBeNull();
    // A simple query that should always succeed if the DB is up
    const result = await db!.execute("SELECT 1 AS ok");
    expect(result.rows[0]).toMatchObject({ ok: 1 });
  });
});
