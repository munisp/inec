// ─── Structured logger ───────────────────────────────────────────────────────
// In production every line is a single JSON object (level/msg/ts + metadata)
// on stdout/stderr so log shippers can parse it without regexes. Outside
// production the output stays human-readable for local development.
// Intentionally dependency-free — a tiny wrapper over console.

export type LogMeta = Record<string, unknown>;

type Level = "debug" | "info" | "warn" | "error";

const IS_PRODUCTION = process.env.NODE_ENV === "production";

function emit(level: Level, msg: string, meta?: LogMeta) {
  const sink =
    level === "error" ? console.error : level === "warn" ? console.warn : console.log;
  if (IS_PRODUCTION) {
    sink(JSON.stringify({ level, msg, ts: new Date().toISOString(), ...(meta ?? {}) }));
    return;
  }
  const suffix = meta && Object.keys(meta).length > 0 ? ` ${JSON.stringify(meta)}` : "";
  sink(`[${msg}]${suffix}`);
}

export const logger = {
  debug: (msg: string, meta?: LogMeta) => emit("debug", msg, meta),
  info: (msg: string, meta?: LogMeta) => emit("info", msg, meta),
  warn: (msg: string, meta?: LogMeta) => emit("warn", msg, meta),
  // Errors are serialised as { name, message, stack } — Error objects do not
  // survive JSON.stringify on their own.
  error: (msg: string, meta?: LogMeta & { err?: unknown }) => {
    const { err, ...rest } = meta ?? {};
    const errorMeta =
      err instanceof Error
        ? { error: { name: err.name, message: err.message, stack: err.stack } }
        : err !== undefined
          ? { error: String(err) }
          : {};
    emit("error", msg, { ...rest, ...errorMeta });
  },
};
