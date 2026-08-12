import { afterEach, describe, expect, it } from "vitest";
import type { Request, Response } from "express";
import {
  metricsAuthGuard,
  metricsMiddleware,
  recordTrpcError,
  renderMetrics,
} from "./metrics";

const originalNodeEnv = process.env.NODE_ENV;
const originalToken = process.env.METRICS_BEARER_TOKEN;

afterEach(() => {
  if (originalNodeEnv === undefined) delete process.env.NODE_ENV;
  else process.env.NODE_ENV = originalNodeEnv;
  if (originalToken === undefined) delete process.env.METRICS_BEARER_TOKEN;
  else process.env.METRICS_BEARER_TOKEN = originalToken;
});

function createRes() {
  const listeners: Record<string, () => void> = {};
  const res = {
    statusCode: 200,
    body: undefined as unknown,
    status(code: number) {
      this.statusCode = code;
      return this;
    },
    json(payload: unknown) {
      this.body = payload;
      return this;
    },
    setHeader() {},
    on(event: string, cb: () => void) {
      listeners[event] = cb;
      return this;
    },
    emitFinish() {
      listeners["finish"]?.();
    },
  };
  return res;
}

describe("metricsAuthGuard", () => {
  it("fails closed (503) in production when METRICS_BEARER_TOKEN is unset", () => {
    process.env.NODE_ENV = "production";
    delete process.env.METRICS_BEARER_TOKEN;
    const res = createRes();
    let called = false;
    metricsAuthGuard({ headers: {} } as Request, res as never, () => {
      called = true;
    });
    expect(called).toBe(false);
    expect(res.statusCode).toBe(503);
  });

  it("rejects (401) a missing/wrong bearer token when one is configured", () => {
    process.env.NODE_ENV = "production";
    process.env.METRICS_BEARER_TOKEN = "s3cret-token";
    const res = createRes();
    let called = false;
    metricsAuthGuard({ headers: {} } as Request, res as never, () => {
      called = true;
    });
    expect(called).toBe(false);
    expect(res.statusCode).toBe(401);
  });

  it("passes with the correct bearer token", () => {
    process.env.METRICS_BEARER_TOKEN = "s3cret-token";
    const res = createRes();
    let called = false;
    metricsAuthGuard(
      { headers: { authorization: "Bearer s3cret-token" } } as unknown as Request,
      res as never,
      () => {
        called = true;
      },
    );
    expect(called).toBe(true);
  });

  it("stays open outside production when no token is configured", () => {
    process.env.NODE_ENV = "test";
    delete process.env.METRICS_BEARER_TOKEN;
    const res = createRes();
    let called = false;
    metricsAuthGuard({ headers: {} } as Request, res as never, () => {
      called = true;
    });
    expect(called).toBe(true);
  });
});

describe("renderMetrics", () => {
  it("exposes request counters, tRPC errors and the SSE gauge", () => {
    const middleware = metricsMiddleware();
    const res = createRes();
    middleware(
      { method: "GET", path: "/api/v1/campaign/health", baseUrl: "" } as unknown as Request,
      res as unknown as Response,
      () => {},
    );
    res.emitFinish();
    recordTrpcError("petitions.publicSign", "TOO_MANY_REQUESTS");

    const text = renderMetrics();
    expect(text).toContain("campaign_http_requests_total");
    expect(text).toContain('route="/api/v1/campaign/health"');
    expect(text).toContain('campaign_trpc_errors_total{path="petitions.publicSign",code="TOO_MANY_REQUESTS"}');
    expect(text).toMatch(/campaign_sse_active_clients \d+/);
    expect(text).toMatch(/campaign_process_uptime_seconds \d+/);
  });

  it("collapses numeric id path segments to bound label cardinality", () => {
    const middleware = metricsMiddleware();
    const res = createRes();
    middleware(
      { method: "GET", path: "/api/profiles/42", baseUrl: "" } as unknown as Request,
      res as unknown as Response,
      () => {},
    );
    res.emitFinish();
    expect(renderMetrics()).toContain('route="/api/profiles/:id"');
  });
});
