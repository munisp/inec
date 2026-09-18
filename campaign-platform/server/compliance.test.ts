// W12 compliance substrate tests — CA lessons → NDPA 2023 features.
// Uses the programmable fake `pg` module (same pattern as voters.bulk.test.ts).
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("./testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";

import { TRPCError } from "@trpc/server";
import { fakePgState } from "./testkit/fakePg";
import { createCtx, createTestUser } from "./testkit/trpcCtx";
import { appRouter } from "./routers";

function routeAsManager() {
  fakePgState.handler = (text) => {
    const sql = text.toLowerCase();
    if (sql.includes('from "candidate_profiles"')) return { rows: [] };
    if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
    return { rows: [] };
  };
}

const VOTER = {
  profileId: 1,
  fullName: "Test Voter",
  dataSource: "door_to_door" as const,
  consentBasis: "consent" as const,
  consentMethod: "written" as const,
  purpose: "campaign outreach",
};

beforeEach(() => fakePgState.reset());

describe("W12 ethics gate on voter writes", () => {
  it("rejects voter.add without dataSource/consentBasis/purpose (zod)", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.voters
      // @ts-expect-error — intentionally missing required compliance fields
      .add({ profileId: 1, fullName: "No Consent" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
    // Nothing written.
    expect(fakePgState.queries.filter(q => q.text.toLowerCase().includes("insert"))).toHaveLength(0);
  });

  it("rejects consent basis without consentMethod (demonstrable-consent rule)", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const { consentMethod, ...noMethod } = VOTER;
    const err = await caller.voters.add(noMethod as any).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).message).toContain("consentMethod");
  });

  it("writes voter + consent + provenance together when compliant", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('insert into "voter_registrations"')) {
        return { rows: [{ id: 42, profile_id: 1, full_name: "Test Voter" }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const result = await caller.voters.add(VOTER);
    expect(result).toBeTruthy();
    const inserts = fakePgState.queries.map(q => q.text.toLowerCase());
    expect(inserts.some(t => t.includes('insert into "voter_registrations"'))).toBe(true);
    expect(inserts.some(t => t.includes('insert into "consent_records"'))).toBe(true);
    expect(inserts.some(t => t.includes('insert into "data_provenance_ledger"'))).toBe(true);
    // Provenance carries the declared source.
    const prov = fakePgState.queries.find(q => q.text.toLowerCase().includes('insert into "data_provenance_ledger"'))!;
    expect(prov.params).toContain("door_to_door");
  });

  it("rejects bulk import without compliance declaration", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const err = await caller.voters
      // @ts-expect-error — intentionally missing required compliance fields
      .bulkImport({ profileId: 1, rows: [{ fullName: "A" }] })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).code).toBe("BAD_REQUEST");
  });
});

describe("W12 access audit", () => {
  it("voters.list appends a data_access_audit entry with row count", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('from "voter_registrations"')) {
        return { rows: [{ id: 1 }, { id: 2 }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const rows = await caller.voters.list({ profileId: 1, purpose: "weekly review" });
    expect(rows).toHaveLength(2);
    const audit = fakePgState.queries.find(q => q.text.toLowerCase().includes('insert into "data_access_audit"'));
    expect(audit).toBeTruthy();
    expect(audit!.params).toContain("weekly review");
    expect(audit!.params).toContain(2);
  });
});

describe("W12 consent lifecycle", () => {
  it("withdrawConsent sets withdrawn_at (state transition, no deletion)", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('update "consent_records"')) {
        return { rows: [{ id: 9, consent_granted: false, retention_until: null, consented_at: null, withdrawn_at: "2026-09-18T00:00:00Z", created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-18T00:00:00Z" }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const rec = await caller.dataProtection.withdrawConsent({ profileId: 1, consentId: 9 });
    expect(rec).toBeTruthy();
    const update = fakePgState.queries.find(q => q.text.toLowerCase().includes('update "consent_records"'));
    expect(update!.text.toLowerCase()).toContain("withdrawn_at");
    // No DELETE against consent_records — history is preserved.
    expect(fakePgState.queries.some(q =>
      q.text.toLowerCase().includes('delete from "consent_records"'))).toBe(false);
  });
});

describe("W12 DSAR workflow", () => {
  it("fileDsar assigns a 30-day due date", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('insert into "data_subject_requests"')) {
        return { rows: [{ id: 5, status: "open", request_type: "erasure", due_at: "2026-10-18", received_at: "2026-09-18T00:00:00Z", fulfilled_at: null, created_at: "2026-09-18T00:00:00Z", updated_at: "2026-09-18T00:00:00Z" }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const dsar = await caller.dataProtection.fileDsar({
      profileId: 1, requestType: "erasure", subjectName: "Adaeze Okafor",
      subjectTable: "voter_registrations", subjectId: 42,
    });
    expect(dsar).toBeTruthy();
    const ins = fakePgState.queries.find(q => q.text.toLowerCase().includes('insert into "data_subject_requests"'))!;
    const dueParam = ins.params.find(p => typeof p === "string" && /^\d{4}-\d{2}-\d{2}$/.test(p));
    expect(dueParam).toBeTruthy();
    const days = (new Date(dueParam as string).getTime() - Date.now()) / 86400_000;
    expect(days).toBeGreaterThan(28);
    expect(days).toBeLessThan(31);
  });

  it("erasure fulfillment performs a REAL delete of the subject row", async () => {
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('from "data_subject_requests"') && sql.startsWith("select")) {
        return { rows: [{
          id: 5, profile_id: 1, request_type: "erasure", status: "open",
          subject_table: "voter_registrations", subject_id: 42,
          subject_name: "Adaeze Okafor", due_at: "2026-10-18",
          received_at: "2026-09-18T00:00:00Z", fulfilled_at: null,
          created_at: "2026-09-18T00:00:00Z", updated_at: "2026-09-18T00:00:00Z",
        }] };
      }
      if (sql.includes('update "data_subject_requests"')) {
        return { rows: [{ id: 5, status: "fulfilled", due_at: "2026-10-18", received_at: "2026-09-18T00:00:00Z", fulfilled_at: "2026-09-18T01:00:00Z", created_at: "2026-09-18T00:00:00Z", updated_at: "2026-09-18T01:00:00Z" }] };
      }
      return { rows: [] };
    };
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const res = await caller.dataProtection.resolveDsar({ profileId: 1, dsarId: 5, action: "fulfill" });
    expect(res).toBeTruthy();
    const del = fakePgState.queries.find(q => q.text.toLowerCase().includes('delete from "voter_registrations"'));
    expect(del).toBeTruthy();
    const tombstone = fakePgState.queries.find(q => q.text.toLowerCase().includes('insert into "data_access_audit"'));
    expect(tombstone).toBeTruthy();
  });

  it("rejection requires a reason", async () => {
    routeAsManager();
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    // DSAR exists but rejection reason missing → BAD_REQUEST.
    fakePgState.handler = (text) => {
      const sql = text.toLowerCase();
      if (sql.includes('from "candidate_profiles"')) return { rows: [] };
      if (sql.includes('from "campaign_members"')) return { rows: [{ role: "manager" }] };
      if (sql.includes('from "data_subject_requests"')) {
        return { rows: [{ id: 5, profile_id: 1, request_type: "access", status: "open", subject_name: "X", due_at: "2026-10-18", received_at: "2026-09-18T00:00:00Z", fulfilled_at: null, created_at: "2026-09-18T00:00:00Z", updated_at: "2026-09-18T00:00:00Z" }] };
      }
      return { rows: [] };
    };
    const err = await caller.dataProtection
      .resolveDsar({ profileId: 1, dsarId: 5, action: "reject" })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TRPCError);
    expect((err as TRPCError).message).toContain("rejectionReason");
  });
});

describe("W12 transparency report", () => {
  it("computes live aggregates; empty tables yield honest zeros", async () => {
    routeAsManager(); // all aggregate selects → zero rows handled by defaults
    const caller = appRouter.createCaller(createCtx(createTestUser()));
    const report = await caller.dataProtection.transparencyReport({ profileId: 1 });
    expect(report).toBeTruthy();
    expect(report!.records.voterRegistrations).toBe(0);
    expect(report!.consent.active).toBe(0);
    expect(report!.consent.voterConsentCoveragePct).toBeNull(); // no voters → null, never 0% fiction
    expect(report!.dsar.overdueOpen).toBe(0);
    expect(report!.note).toContain("never estimates");
  });
});
