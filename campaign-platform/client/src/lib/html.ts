/**
 * Minimal HTML escaping helpers (R4-40 remediation).
 *
 * Stakeholder / candidate fields are API- and CRM-controlled, so any value
 * interpolated into an HTML template (print briefs, calendars, tooltips) MUST
 * pass through `escapeHtml` — otherwise a stored payload such as
 * `<img src=x onerror=alert(1)>` in a stakeholder name executes as markup.
 *
 * Keep this dependency-free on purpose: the bundle does not need a full
 * sanitizer because templates here only ever inject *text*, never markup.
 */

const HTML_ENTITIES: Record<string, string> = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
};

/** Escape a string for safe interpolation into HTML text or attribute context. */
export function escapeHtml(value: unknown): string {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => HTML_ENTITIES[ch] ?? ch);
}

/**
 * Validate a URL for use in an `<img src>` attribute. Only http(s) and
 * data:image/* URLs are allowed; anything else (notably `javascript:` or
 * quote-breakout payloads) yields an empty string so the <img> is omitted.
 */
export function safeImageUrl(url: unknown): string {
  const u = String(url ?? "").trim();
  if (/^https:\/\//i.test(u)) return escapeHtml(u);
  if (typeof window !== "undefined" && window.location?.protocol === "http:" && /^http:\/\//i.test(u)) {
    return escapeHtml(u); // dev/http deployments
  }
  if (/^data:image\/(png|jpe?g|gif|webp|svg\+xml);base64,[a-z0-9+/=\s]+$/i.test(u)) return u;
  return "";
}

/**
 * Validate a CSS color used inside style attributes. Accepts #hex, rgb(a),
 * hsl(a) and named letters only; anything else falls back to the default so a
 * hostile value cannot break out of the style attribute.
 */
export function safeColor(value: unknown, fallback = "#111827"): string {
  const v = String(value ?? "").trim();
  if (/^(#[0-9a-f]{3,8}|rgba?\(\s*[\d\s.,%]+\)|hsla?\(\s*[\d\s.,%]+\)|[a-z]{3,20})$/i.test(v)) {
    return v;
  }
  return fallback;
}
