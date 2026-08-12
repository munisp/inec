import { NOT_ADMIN_ERR_MSG, UNAUTHED_ERR_MSG } from '@shared/const';
import { initTRPC, TRPCError } from "@trpc/server";
import superjson from "superjson";
import type { TrpcContext } from "./context";

const t = initTRPC.context<TrpcContext>().create({
  transformer: superjson,
});

export const router = t.router;
export const publicProcedure = t.procedure;

const requireUser = t.middleware(async opts => {
  const { ctx, next } = opts;

  if (!ctx.user) {
    throw new TRPCError({ code: "UNAUTHORIZED", message: UNAUTHED_ERR_MSG });
  }

  return next({
    ctx: {
      ...ctx,
      user: ctx.user,
    },
  });
});

export const protectedProcedure = t.procedure.use(requireUser);

export const adminProcedure = t.procedure.use(
  t.middleware(async opts => {
    const { ctx, next } = opts;

    if (!ctx.user || ctx.user.role !== 'admin') {
      throw new TRPCError({ code: "FORBIDDEN", message: NOT_ADMIN_ERR_MSG });
    }

    return next({
      ctx: {
        ...ctx,
        user: ctx.user,
      },
    });
  }),
);

// ─── Profile tenancy enforcement ─────────────────────────────────────────────
// SECURITY: every campaign entity row belongs to a candidate profile. Before
// these fixes, routers accepted a caller-supplied `profileId` with no ownership
// or membership verification, so any authenticated user could read/write any
// other campaign's data (IDOR). The helpers below close that hole.
//
// NOTE on db.getMyRoleForProfile (server/db.ts): it returns "viewer" when the
// user has NO membership row at all (fail-open default), so it cannot by itself
// enforce tenancy for read access. resolveProfileRole below performs a strict
// resolution instead: owner via candidate_profiles.user_id, else an explicit
// campaign_members row, else null (no access).
export type ProfileRole = "owner" | "manager" | "viewer";

const PROFILE_ROLE_RANK: Record<ProfileRole, number> = {
  viewer: 1,
  manager: 2,
  owner: 3,
};

const NO_PROFILE_ACCESS_ERR_MSG =
  "You do not have access to this campaign profile";

/**
 * SECURITY: strict role resolution for (profileId, userId).
 * Returns null when the user neither owns the profile nor holds a
 * campaign_members row. Fails closed when the database is unavailable.
 */
export async function resolveProfileRole(
  profileId: number,
  userId: number,
): Promise<ProfileRole | null> {
  // Dynamic imports avoid any module-load-order coupling between _core and db.
  const { getDb } = await import("../db");
  const conn = getDb();
  if (!conn) return null; // SECURITY: fail closed — no DB, no tenancy check possible
  const schema = await import("../../drizzle/schema");
  const { eq, and } = await import("drizzle-orm");

  const owned = await conn
    .select({ id: schema.candidateProfiles.id })
    .from(schema.candidateProfiles)
    .where(
      and(
        eq(schema.candidateProfiles.id, profileId),
        eq(schema.candidateProfiles.userId, userId),
      ),
    )
    .limit(1);
  if (owned.length > 0) return "owner";

  const member = await conn
    .select({ role: schema.campaignMembers.role })
    .from(schema.campaignMembers)
    .where(
      and(
        eq(schema.campaignMembers.profileId, profileId),
        eq(schema.campaignMembers.userId, userId),
      ),
    )
    .limit(1);
  if (member.length > 0) return member[0].role as ProfileRole;
  return null;
}

/**
 * SECURITY: imperative variant for procedures whose input does not carry
 * `profileId` directly (row-id deletes, petition-id lookups, team member ids).
 * Resolve the owning profileId first, then call this.
 */
export async function assertProfileRole(
  user: TrpcContext["user"],
  profileId: number,
  minRole: ProfileRole,
): Promise<ProfileRole> {
  if (!user) {
    throw new TRPCError({ code: "UNAUTHORIZED", message: UNAUTHED_ERR_MSG });
  }
  const role = await resolveProfileRole(profileId, user.id);
  if (!role || PROFILE_ROLE_RANK[role] < PROFILE_ROLE_RANK[minRole]) {
    throw new TRPCError({
      code: "FORBIDDEN",
      message: `${NO_PROFILE_ACCESS_ERR_MSG} (requires ${minRole})`,
    });
  }
  return role;
}

/**
 * SECURITY: tRPC middleware factory. Reads the raw input's `profileId`
 * (middleware runs before input parsing, so this is the caller-supplied value)
 * and rejects anyone without at least `minRole` on that profile.
 * Attach the resolved role to ctx as `profileRole`.
 */
export function requireProfileRole(minRole: ProfileRole) {
  return t.middleware(async ({ ctx, next, input }) => {
    if (!ctx.user) {
      throw new TRPCError({ code: "UNAUTHORIZED", message: UNAUTHED_ERR_MSG });
    }
    const profileId = (input as { profileId?: unknown } | null | undefined)
      ?.profileId;
    if (typeof profileId !== "number" || !Number.isInteger(profileId)) {
      throw new TRPCError({
        code: "BAD_REQUEST",
        message: "A numeric profileId is required for this procedure",
      });
    }
    const role = await resolveProfileRole(profileId, ctx.user.id);
    if (!role || PROFILE_ROLE_RANK[role] < PROFILE_ROLE_RANK[minRole]) {
      throw new TRPCError({
        code: "FORBIDDEN",
        message: `${NO_PROFILE_ACCESS_ERR_MSG} (requires ${minRole})`,
      });
    }
    return next({
      ctx: {
        ...ctx,
        user: ctx.user,
        profileRole: role,
      },
    });
  });
}

/**
 * SECURITY: drop-in replacement for protectedProcedure on any procedure whose
 * input includes `profileId`.
 *  - "viewer"  → read-only procedures (lists, KPIs, AI drafts)
 *  - "manager" → writes (upsert/add/save/create/import)
 *  - "owner"   → destructive operations (delete, seeding)
 */
export function profileScopedProcedure(minRole: ProfileRole = "viewer") {
  return t.procedure.use(requireProfileRole(minRole));
}
