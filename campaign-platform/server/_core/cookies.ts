import type { CookieOptions, Request } from "express";

const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "::1"]);

function isIpAddress(host: string) {
  // Basic IPv4 check and IPv6 presence detection.
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(host)) return true;
  return host.includes(":");
}

function isSecureRequest(req: Request) {
  if (req.protocol === "https") return true;

  const forwardedProto = req.headers["x-forwarded-proto"];
  if (!forwardedProto) return false;

  const protoList = Array.isArray(forwardedProto)
    ? forwardedProto
    : forwardedProto.split(",");

  return protoList.some(proto => proto.trim().toLowerCase() === "https");
}

type SameSite = "lax" | "strict" | "none";

function resolveSameSite(): SameSite {
  // Default "lax" blocks CSRF-prone cross-site sends of the session cookie.
  // Cross-site iframe/WebView embeds that genuinely need SameSite=None must opt
  // in explicitly via COOKIE_SAMESITE=none (which also forces Secure).
  const value = (process.env.COOKIE_SAMESITE ?? "").trim().toLowerCase();
  if (value === "none" || value === "strict" || value === "lax") return value;
  return "lax";
}

function resolveSecureFlag(req: Request, sameSite: SameSite): boolean {
  // Explicit override always wins.
  const override = (process.env.COOKIE_SECURE ?? "").trim().toLowerCase();
  if (override === "true") return true;
  if (override === "false") return sameSite === "none" ? true : false;

  // Sane default: secure in production. We deliberately do NOT let a spoofable
  // client-controlled x-forwarded-proto header downgrade the cookie to
  // non-secure in production.
  if (process.env.NODE_ENV === "production") return true;
  // Browsers reject SameSite=None without Secure.
  if (sameSite === "none") return true;
  // Development: best-effort detection behind a local proxy.
  return isSecureRequest(req);
}

export function getSessionCookieOptions(
  req: Request
): Pick<CookieOptions, "domain" | "httpOnly" | "path" | "sameSite" | "secure"> {
  // const hostname = req.hostname;
  // const shouldSetDomain =
  //   hostname &&
  //   !LOCAL_HOSTS.has(hostname) &&
  //   !isIpAddress(hostname) &&
  //   hostname !== "127.0.0.1" &&
  //   hostname !== "::1";

  // const domain =
  //   shouldSetDomain && !hostname.startsWith(".")
  //     ? `.${hostname}`
  //     : shouldSetDomain
  //       ? hostname
  //       : undefined;

  const sameSite = resolveSameSite();
  return {
    httpOnly: true,
    path: "/",
    sameSite,
    secure: resolveSecureFlag(req, sameSite),
  };
}
