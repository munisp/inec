import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Production-safe logger.
 *
 * DEV: plain console output.
 * PROD: console is silent; logger.error additionally ships a sanitized,
 * PII-free record to the frontend error inbox (best-effort, batched).
 */

interface FrontendErrorRecord {
  message: string;
  page: string;
  timestamp: string;
  user_agent: string;
}

const errorQueue: FrontendErrorRecord[] = [];
let flushTimer: ReturnType<typeof setTimeout> | null = null;
const ERROR_INGEST_URL = `${import.meta.env.VITE_API_URL ?? ''}/api/v1/errors/frontend`;

/** Strip anything that looks like PII (emails, long digit runs, bearer tokens). */
function sanitize(value: unknown): string {
  let text = typeof value === 'string' ? value : value instanceof Error ? value.message : JSON.stringify(value);
  try {
    text = String(text);
  } catch {
    text = 'unserializable error';
  }
  return text
    .replace(/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g, '[redacted-email]')
    .replace(/Bearer\s+\S+/gi, 'Bearer [redacted]')
    .replace(/\b\d{6,}\b/g, '[redacted-num]')
    .slice(0, 500);
}

function flushErrors() {
  flushTimer = null;
  if (errorQueue.length === 0) return;
  const batch = errorQueue.splice(0, errorQueue.length);
  const body = JSON.stringify({ source: 'inec-frontend', errors: batch });
  try {
    if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
      const ok = navigator.sendBeacon(ERROR_INGEST_URL, new Blob([body], { type: 'application/json' }));
      if (ok) return;
    }
    fetch(ERROR_INGEST_URL, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body,
      credentials: 'include',
      keepalive: true,
    }).catch(() => { /* best-effort telemetry — never throw from the logger */ });
  } catch { /* best-effort */ }
}

function queueError(record: FrontendErrorRecord) {
  errorQueue.push(record);
  if (errorQueue.length >= 10) {
    flushErrors();
    return;
  }
  if (!flushTimer) flushTimer = setTimeout(flushErrors, 10_000);
}

if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', flushErrors);
}

export const logger = {
  error(msg: unknown, ...args: unknown[]) {
    if (import.meta.env.DEV) {
      console.error('[INEC]', msg, ...args);
      return;
    }
    queueError({
      message: [msg, ...args].map(sanitize).join(' '),
      page: window.location.hash.replace(/^#\/?/, '').split(/[/?]/, 1)[0] || 'dashboard',
      timestamp: new Date().toISOString(),
      user_agent: navigator.userAgent.slice(0, 200),
    });
  },
  warn(msg: unknown, ...args: unknown[]) {
    if (import.meta.env.DEV) {
      console.warn('[INEC]', msg, ...args);
    }
  },
};
