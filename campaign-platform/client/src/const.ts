export { COOKIE_NAME, ONE_YEAR_MS } from "@shared/const";

// Navigate to the local username/password login page. This app authenticates
// against the Go backend's shared `users` table (see server/_core/localAuth.ts),
// not OAuth — the Manus OAuth portal isn't configured in this deployment.
export const startLogin = () => {
  window.location.href = "/login";
};
