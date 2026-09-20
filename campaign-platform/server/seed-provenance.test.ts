// Audit SEC-15: seeded demo data must be labelled at the data layer.
// Every seeded personal-data row gets a consent record (consentGranted=false,
// purpose=demo) + a provenance ledger entry (source=demo_seed), and each
// seeded table is audit-logged. Uses the programmable fake `pg` module.
import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { fakePgState } from "./testkit/fakePg";
import { seedProfileData } from "./db";

const originalFlag = process.env.CAMPAIGN_ALLOW_FIXTURE_SEED;
const originalNodeEnv = process.env.NODE_ENV;

beforeEach(() => {
  fakePgState.reset();
  process.env.CAMPAIGN_ALLOW_FIXTURE_SEED = "true";
  process.env.NODE_ENV = "test";
  // All inserts/updates/deletes succeed; returning() calls get synthetic ids
  // matching the real seeded row counts (8 voters, 6 volunteers, 3 petition
  // signatures, 4 diaspora contacts, 7 stakeholder contacts, 1 petition) so
  // the trail loops iterate over every seeded row.
  let nextId = 100;
  const ROW_COUNTS: Array<[string, number]> = [
    ['"voter_registrations"', 8],
    ['"volunteers"', 6],
    ['"petition_signatures"', 3],
    ['"diaspora_contacts"', 4],
    ['"stakeholder_contacts"', 7],
  ];
  fakePgState.handler = (text) => {
    const lower = text.toLowerCase();
    if (lower.includes("insert into") && lower.includes("returning")) {
      const [, count] = ROW_COUNTS.find(([t]) => lower.includes(`insert into ${t}`)) ?? ["", 1];
      return { rows: Array.from({ length: count }, () => ({ id: nextId++ })) };
    }
    return { rows: [] };
  };
});

afterEach(() => {
  if (originalFlag === undefined) delete process.env.CAMPAIGN_ALLOW_FIXTURE_SEED;
  else process.env.CAMPAIGN_ALLOW_FIXTURE_SEED = originalFlag;
  if (originalNodeEnv === undefined) delete process.env.NODE_ENV;
  else process.env.NODE_ENV = originalNodeEnv;
});

describe("seed demo-data labelling (SEC-15)", () => {
  it("labels every seeded PII row with demo consent + provenance trails", async () => {
    await seedProfileData(1);
    const inserts = fakePgState.queries.filter((q) => q.text.toLowerCase().includes("insert into"));

    const consentInserts = inserts.filter((q) => q.text.includes("consent_records"));
    const provenanceInserts = inserts.filter((q) => q.text.includes("data_provenance_ledger"));

    // Seeded PII rows: 8 voters + 6 volunteers + 3 petition signatures +
    // 4 diaspora contacts + 7 stakeholder contacts = 28 trails each.
    expect(consentInserts.length).toBe(28);
    expect(provenanceInserts.length).toBe(28);

    // Every trail must be honestly labelled as demo data.
    for (const q of provenanceInserts) {
      expect(q.params).toContain("demo_seed");
    }
    for (const q of consentInserts) {
      expect(q.params).toContain(false); // consentGranted = false for demo rows
    }
  });

  it("writes one data_access_audit seed_demo entry per seeded PII table", async () => {
    await seedProfileData(1);
    const auditInserts = fakePgState.queries.filter(
      (q) => q.text.toLowerCase().includes("insert into") && q.text.includes("data_access_audit"),
    );
    expect(auditInserts.length).toBe(5);
    for (const q of auditInserts) {
      expect(q.params).toContain("seed_demo");
      expect(q.params).toContain("seed_demo_data");
    }
    const auditedTables = auditInserts.map((q) => (q.params ?? [])[2]);
    for (const t of ["voter_registrations", "volunteers", "petition_signatures", "diaspora_contacts", "stakeholder_contacts"]) {
      expect(auditedTables).toContain(t);
    }
  });
});
