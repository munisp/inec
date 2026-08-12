import type { CreateExpressContextOptions } from "@trpc/server/adapters/express";
import type { User } from "../../drizzle/schema";
import { sdk } from "./sdk";

// SECURITY: the request-scoped user never carries passwordHash — auth.me
// returns ctx.user verbatim, and leaking the bcrypt hash to the client would
// hand attackers offline-cracking material.
export type SessionUser = Omit<User, "passwordHash">;

export type TrpcContext = {
  req: CreateExpressContextOptions["req"];
  res: CreateExpressContextOptions["res"];
  user: SessionUser | null;
};

export async function createContext(
  opts: CreateExpressContextOptions
): Promise<TrpcContext> {
  let user: SessionUser | null = null;

  try {
    const authed = await sdk.authenticateRequest(opts.req);
    // Strip the credential material before the user object reaches resolvers.
    const { passwordHash: _passwordHash, ...safeUser } = authed;
    user = safeUser;
  } catch (error) {
    // Authentication is optional for public procedures.
    user = null;
  }

  return {
    req: opts.req,
    res: opts.res,
    user,
  };
}
