// DEV-ONLY demo credentials (R4-41). This module must ONLY be reachable behind
// `__DEV__ ? require(...) : null` guards so Metro's production pipeline
// (constant-folding + dead-code elimination) removes it — and these strings —
// from release bundles entirely. Never import it statically.

export interface DemoAccount {
  username: string;
  password: string;
  label: string;
  icon: 'eye-outline' | 'shield-outline' | 'person-outline';
  bg: string;
  color: string;
}

export const DEMO_ACCOUNTS: DemoAccount[] = [
  { username: 'observer', password: 'observer123', label: 'Observer', icon: 'eye-outline', bg: '#dbeafe', color: '#2563eb' },
  { username: 'admin', password: 'admin123', label: 'Admin', icon: 'shield-outline', bg: '#dcfce7', color: '#166534' },
  { username: 'officer1', password: 'officer123', label: 'Officer', icon: 'person-outline', bg: '#fef3c7', color: '#d97706' },
];
