/**
 * R4-40 regression tests: stored-XSS payloads in stakeholder/candidate fields
 * must render as inert text in generated HTML, never as markup.
 */
import { describe, it, expect } from "vitest";
import { escapeHtml, safeImageUrl, safeColor } from "./html";
import { generateBriefHTML } from "../components/StakeholderBriefPDF";
import { generatePrintHTML, type CalendarEvent } from "../components/EngagementCalendar";
import type { Stakeholder } from "../components/StakeholderTypes";

const XSS_NAME = `<img src=x onerror="alert('xss')">`;
const XSS_SCRIPT = `<script>alert('xss')</script>`;

function hostileStakeholder(overrides: Partial<Stakeholder> = {}): Stakeholder {
  return {
    id: "s1",
    name: XSS_NAME,
    category: "Youth",
    subcategory: "",
    priority: 1,
    reach_pct: 10,
    key_ask: XSS_SCRIPT,
    cultural_protocol: "greet",
    engagement_method: ["rally"],
    best_engagement_time: "morning",
    talking_points: [`<svg onload=alert(1)>`],
    office_relevance: {},
    ...overrides,
  };
}

describe("escapeHtml", () => {
  it("escapes an img/onerror payload so it cannot parse as markup", () => {
    const out = escapeHtml(XSS_NAME);
    expect(out).not.toContain("<img");
    expect(out).toContain("&lt;img");
    expect(out).toContain("&quot;");
  });

  it("escapes all five HTML-significant characters", () => {
    expect(escapeHtml(`&<>"'`)).toBe("&amp;&lt;&gt;&quot;&#39;");
  });
});

describe("safeImageUrl", () => {
  it("accepts https and data:image URLs", () => {
    expect(safeImageUrl("https://example.com/logo.png")).toBe("https://example.com/logo.png");
    expect(safeImageUrl("data:image/png;base64,iVBORw0KGgo=")).toContain("data:image/png");
  });

  it("rejects javascript: and attribute-breakout payloads", () => {
    expect(safeImageUrl("javascript:alert(1)")).toBe("");
    expect(safeImageUrl(`x" onerror="alert(1)`)).toBe("");
  });
});

describe("safeColor", () => {
  it("accepts hex colors and rejects style-breakout payloads", () => {
    expect(safeColor("#006400")).toBe("#006400");
    expect(safeColor(`red;}.x{background:url(javascript:alert(1))`)).toBe("#111827");
  });
});

describe("generateBriefHTML (R4-40)", () => {
  it("renders a hostile stakeholder name as escaped text, not markup", () => {
    const html = generateBriefHTML(
      [hostileStakeholder()], XSS_NAME, "Governor", "Lagos", "APC", "en", "", "#006400"
    );
    expect(html).not.toContain(XSS_NAME); // raw payload must not appear
    expect(html).not.toContain(XSS_SCRIPT);
    expect(html).not.toContain("<svg onload");
    expect(html).toContain("&lt;img src=x onerror="); // escaped text form present
  });

  it("omits a hostile partyLogo URL instead of writing an <img>", () => {
    const html = generateBriefHTML(
      [hostileStakeholder({ name: "Ok" })], "Ok", "Gov", "Lagos", "APC", "en",
      `x" onerror="alert(1)`, "#006400"
    );
    expect(html).not.toContain("onerror");
  });
});

describe("generatePrintHTML / EngagementCalendar (R4-40)", () => {
  it("escapes hostile stakeholder names and candidate names", () => {
    const evt: CalendarEvent = {
      id: "e1",
      day: 1,
      date: new Date("2026-01-01"),
      week: 1,
      stakeholder: hostileStakeholder(),
      eventType: "Rally",
      duration: "2h",
      location: "Kano",
      notes: "",
      phase: "Mobilisation",
    };
    const html = generatePrintHTML([evt], XSS_NAME, "Lagos", "Governor");
    expect(html).not.toContain(XSS_NAME);
    expect(html).not.toContain(XSS_SCRIPT);
    expect(html).toContain("&lt;img src=x onerror=");
  });
});
