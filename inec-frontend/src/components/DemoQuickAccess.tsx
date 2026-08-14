import { Shield, Vote, Eye } from 'lucide-react';

interface DemoQuickAccessProps {
  /** Must be import.meta.env.DEV at the call site — demo credentials must
      never render in production builds (R4-41). */
  enabled: boolean;
  onQuickLogin: (username: string, password: string) => void;
}

const DEMO_ACCOUNTS = [
  { username: 'admin', password: 'admin123', label: 'Administrator', hint: 'Full system access', Icon: Shield, color: 'green' },
  { username: 'officer1', password: 'officer123', label: 'Presiding Officer', hint: 'Upload & manage results', Icon: Vote, color: 'blue' },
  { username: 'observer', password: 'observer123', label: 'Election Observer', hint: 'View & verify results', Icon: Eye, color: 'amber' },
] as const;

const COLOR_CLASSES: Record<string, { box: string; icon: string }> = {
  green: { box: 'bg-green-100', icon: 'text-green-700' },
  blue: { box: 'bg-blue-100', icon: 'text-blue-700' },
  amber: { box: 'bg-amber-100', icon: 'text-amber-700' },
};

export default function DemoQuickAccess({ enabled, onQuickLogin }: DemoQuickAccessProps) {
  if (!enabled) return null;
  return (
    <div className="mt-6 pt-4 border-t border-zinc-200">
      <p className="text-xs text-zinc-500 mb-3">Quick access (demo accounts, dev only):</p>
      <div className="space-y-2">
        {DEMO_ACCOUNTS.map(({ username, password, label, hint, Icon, color }) => (
          <button key={username} onClick={() => onQuickLogin(username, password)}
            className="w-full flex items-center gap-3 p-2.5 rounded-lg border border-zinc-200 hover:bg-zinc-50 transition-colors text-left">
            <div className={`w-8 h-8 rounded-lg ${COLOR_CLASSES[color].box} flex items-center justify-center`}>
              <Icon className={`w-4 h-4 ${COLOR_CLASSES[color].icon}`} />
            </div>
            <div>
              <p className="text-sm font-medium text-zinc-900">{label}</p>
              <p className="text-xs text-zinc-500">{hint}</p>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
