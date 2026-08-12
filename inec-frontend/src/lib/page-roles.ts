/**
 * Client-side role gating for pages and navigation.
 *
 * This is a UX-layer guard only — the backend remains the authoritative
 * access-control enforcer. Pages not listed here are available to every
 * authenticated user.
 *
 * Role vocabulary (from inec-go-backend JWT claims):
 *   admin, superadmin, collation_officer, presiding_officer, observer
 */

const ADMIN_ROLES = ['admin', 'superadmin'];
const STAFF_ROLES = ['admin', 'superadmin', 'collation_officer', 'presiding_officer'];

/** Platform administration — admin/superadmin only. */
const ADMIN_PAGES = [
  'admin-console',
  'user-management',
  'production',
  'command-center',
  'webhooks',
  'scale-health',
  'middleware',
  'portal-integration',
  'workflow-engine',
  'audit',
  'voter-registration',
  'kyc-verification',
  'enrollment-kiosk',
  'bvas',
  'bvas-sync',
  'biometric',
  'blockchain',
  'geofencing',
  'data-validation',
  'duplicate-detection',
  'document-ai',
  'evidence-journey',
  'export-center',
  'stakeholders',
  'stakeholder-workflows',
  'gotv-portal',
  'party-primaries',
  'training',
];

/** Election-operations tooling — staff roles, not read-only observers. */
const STAFF_PAGES = [
  'collation',
  'incidents',
  'sms-verification',
  'anomaly-detection',
  'ai-monitoring',
  'ml-dashboard',
  'predictive-analytics',
  'integrity-score',
  'compliance-report',
  'dispute-resolution',
  'observer-monitoring',
];

export const PAGE_ROLES: Record<string, string[]> = {
  ...Object.fromEntries(ADMIN_PAGES.map((p) => [p, ADMIN_ROLES])),
  ...Object.fromEntries(STAFF_PAGES.map((p) => [p, STAFF_ROLES])),
};

/** True when a user with `role` may open `page`. Unlisted pages are open. */
export function canAccessPage(page: string, role: string | null | undefined): boolean {
  const roles = PAGE_ROLES[page];
  if (!roles) return true;
  return !!role && roles.includes(role);
}
