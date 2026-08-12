import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("pg", async () => await import("../testkit/fakePg"));

process.env.POSTGRES_URL ||= "postgres://test:test@localhost:5432/test";
// The throttle uses the in-memory fallback outside production (the mode under
// test here); in production it is Postgres-backed and fails closed.
process.env.NODE_ENV = "test";

import type { Request, Response } from "express";
import { fakePgState } from "../testkit/fakePg";
import { __resetInMemoryRateLimitsForTests } from "./rateLimit";
import { registerLocalAuthRoutes } from "./localAuth";

type Handler = (req: Request, res: Response) => unknown;

function createApp() {
  const routes: Record<string, Handler> = {};
  const app = {
    post: (path: string, handler: Handler) => {
      routes[path] = handler;
    },
  };
  return { app: app as never, routes };
}

function createReq(body: Record<string, unknown>, ip = "198.51.100.7") {
  return {
    body,
    ip,
    socket: { remoteAddress: ip },
    headers: {},
    protocol: "http",
  } as unknown as Request;
}

function createRes() {
  const res = {
    statusCode: 200,
    body: undefined as unknown,
    headers: {} as Record<string, string>,
    cookies: [] as Array<{ name: string; value: string }>,
    status(code: number) {
      this.statusCode = code;
      return this;
    },
    json(payload: unknown) {
      this.body = payload;
      return this;
    },
    setHeader(name: string, value: string) {
      this.headers[name.toLowerCase()] = value;
    },
    cookie(name: string, value: string) {
      this.cookies.push({ name, value });
      return this;
    },
  };
  return res as unknown as Response & typeof res;
}

beforeEach(() => {
  fakePgState.reset();
  __resetInMemoryRateLimitsForTests();
  // Unknown user for every attempt → 401s until the throttle engages.
  fakePgState.handler = () => ({ rows: [] });
});

describe("POST /api/login throttle", () => {
  it("allows up to 5 attempts per IP+username in 5 minutes, then returns 429", async () => {
    const { app, routes } = createApp();
    registerLocalAuthRoutes(app);
    const login = routes["/api/login"];
    expect(login).toBeDefined();

    const ip = "198.51.100.7";
    const results: number[] = [];
    for (let i = 0; i < 6; i++) {
      const res = createRes();
      await login(createReq({ username: "admin", password: "wrong" }, ip), res);
      results.push(res.statusCode);
    }

    expect(results).toEqual([401, 401, 401, 401, 401, 429]);
  });

  it("throttles password spraying across usernames from one IP (30/5min)", async () => {
    const { app, routes } = createApp();
    registerLocalAuthRoutes(app);
    const login = routes["/api/login"];

    const ip = "198.51.100.99";
    let lastStatus = 0;
    for (let i = 0; i < 31; i++) {
      const res = createRes();
      await login(createReq({ username: `user-${i}`, password: "wrong" }, ip), res);
      lastStatus = res.statusCode;
    }

    expect(lastStatus).toBe(429);
  });

  it("returns 400 for a malformed body without consuming throttle budget", async () => {
    const { app, routes } = createApp();
    registerLocalAuthRoutes(app);
    const login = routes["/api/login"];

    const res = createRes();
    await login(createReq({ username: 42 }), res);
    expect(res.statusCode).toBe(400);
  });
});
