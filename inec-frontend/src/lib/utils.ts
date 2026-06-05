import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** Production-safe logger that suppresses debug output in prod builds. */
export const logger = {
  error(msg: unknown, ...args: unknown[]) {
    if (import.meta.env.DEV) {
      console.error('[INEC]', msg, ...args);
    }
  },
  warn(msg: unknown, ...args: unknown[]) {
    if (import.meta.env.DEV) {
      console.warn('[INEC]', msg, ...args);
    }
  },
};
