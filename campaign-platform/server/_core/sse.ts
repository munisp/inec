// ─── War Room SSE registry ───────────────────────────────────────────────────
// Extracted from _core/index.ts so routers.ts can broadcast without importing
// the server entrypoint — importing _core/index boots the HTTP listener as a
// module side effect, which created a circular import (routers ↔ index) and
// started a server in unit tests. Both _core/index.ts (the SSE endpoint) and
// routers.ts (broadcasts) now import this module instead.
import type { Response } from "express";

// In-memory SSE client registry keyed by profileId.
const sseClients = new Map<number, Set<Response>>();

// SECURITY: bound total SSE connections so a client flood cannot exhaust
// sockets/memory on the process.
export const SSE_MAX_CLIENTS = 500;
// SECURITY: per-user concurrent stream cap — a single account must not be
// able to hold the global connection budget hostage.
export const SSE_MAX_STREAMS_PER_USER = 5;

const sseStreamsPerUser = new Map<number, number>();

export function sseClientCount(): number {
  let total = 0;
  sseClients.forEach(set => { total += set.size; });
  return total;
}

export function sseStreamCountForUser(userId: number): number {
  return sseStreamsPerUser.get(userId) ?? 0;
}

export function broadcastWarRoomUpdate(profileId: number) {
  const clients = sseClients.get(profileId);
  if (!clients) return;
  const payload = `data: ${JSON.stringify({ type: "update", profileId })}\n\n`;
  clients.forEach(res => {
    try {
      res.write(payload);
    } catch { /* client vanished mid-write; the close handler will clean up */ }
  });
}

/**
 * Register an SSE response for a profile/user. Returns an idempotent
 * unregister function — call it from the request's "close" handler.
 */
export function registerSseClient(
  profileId: number,
  userId: number,
  res: Response,
): () => void {
  if (!sseClients.has(profileId)) sseClients.set(profileId, new Set());
  sseClients.get(profileId)!.add(res);
  sseStreamsPerUser.set(userId, sseStreamCountForUser(userId) + 1);

  let cleaned = false;
  return () => {
    if (cleaned) return;
    cleaned = true;
    const set = sseClients.get(profileId);
    set?.delete(res);
    if (set && set.size === 0) sseClients.delete(profileId);
    const remaining = sseStreamCountForUser(userId);
    if (remaining <= 1) sseStreamsPerUser.delete(userId);
    else sseStreamsPerUser.set(userId, remaining - 1);
  };
}

/** End every open SSE response (used by graceful shutdown). */
export function destroyAllSseClients() {
  sseClients.forEach(set => {
    set.forEach(res => {
      try {
        res.end();
      } catch { /* already closed */ }
    });
  });
  sseClients.clear();
  sseStreamsPerUser.clear();
}
