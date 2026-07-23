import { COOKIE_NAME, ONE_YEAR_MS } from "@shared/const";
import bcrypt from "bcryptjs";
import type { Express, Request, Response } from "express";
import * as db from "../db";
import { getSessionCookieOptions } from "./cookies";
import { sdk } from "./sdk";
import { ENV } from "./env";

// Local username/password login against the Go backend's shared `users`
// table (bcrypt password hashes) — used instead of OAuth, which requires an
// external OAuth server that isn't configured in this deployment.
export function registerLocalAuthRoutes(app: Express) {
  app.post("/api/login", async (req: Request, res: Response) => {
    const { username, password } = req.body ?? {};
    if (typeof username !== "string" || typeof password !== "string" || !username || !password) {
      res.status(400).json({ error: "username and password are required" });
      return;
    }

    const user = await db.getUserByUsername(username);
    if (!user || !(await bcrypt.compare(password, user.passwordHash))) {
      res.status(401).json({ error: "invalid username or password" });
      return;
    }
    if (user.isActive === 0) {
      res.status(403).json({ error: "account is inactive" });
      return;
    }

    const sessionToken = await sdk.signSession(
      { openId: user.username, appId: ENV.appId || "campaign-platform", name: user.fullName },
      { expiresInMs: ONE_YEAR_MS }
    );

    const cookieOptions = getSessionCookieOptions(req);
    res.cookie(COOKIE_NAME, sessionToken, { ...cookieOptions, maxAge: ONE_YEAR_MS });
    res.json({ ok: true, user: { username: user.username, fullName: user.fullName, role: user.role } });
  });
}
