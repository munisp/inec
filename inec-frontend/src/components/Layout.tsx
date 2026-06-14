import { useState } from 'react';
import { useAuth } from '@/lib/auth';
import { useI18n } from '@/lib/i18n';
import { useTheme } from '@/components/ThemeProvider';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Badge } from '@/components/ui/badge';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, DropdownMenuSeparator } from '@/components/ui/dropdown-menu';
import {
  LayoutDashboard, Vote, FileBarChart, Shield, AlertTriangle,
  Menu, LogOut, ChevronRight, Landmark, MapPin, Users, Map, Layers, Fingerprint,
  Brain, MessageSquare, Code2, UserPlus, GitBranch, RefreshCw, Globe, ShieldCheck, Settings,
  ScanFace, Link2, GraduationCap, Users2, BarChart3, Eye, Sun, Moon, Monitor,
  Activity, UserCheck, Crosshair, Webhook, UserCog, Copy, FileSearch, Download,
  Radio, KeyRound, TrendingUp, UserSearch, Tv, FileCheck, Flame, Megaphone
} from 'lucide-react';

const NAV_ITEMS = [
  { label: 'Dashboard', icon: LayoutDashboard, path: 'dashboard' },
  { label: 'Map View', icon: Map, path: 'map' },
  { label: 'Elections', icon: Landmark, path: 'elections' },
  { label: 'Results', icon: Vote, path: 'results' },
  { label: 'Collation', icon: FileBarChart, path: 'collation' },
  { label: 'Polling Units', icon: MapPin, path: 'polling-units' },
  { label: 'Audit Trail', icon: Shield, path: 'audit' },
  { label: 'Incidents', icon: AlertTriangle, path: 'incidents' },
  { label: 'AI Anomaly', icon: Brain, path: 'anomaly-detection' },
  { label: 'SMS/USSD', icon: MessageSquare, path: 'sms-verification' },
  { label: 'Public API', icon: Code2, path: 'public-api' },
  { label: 'Middleware', icon: Layers, path: 'middleware' },
  { label: 'BVAS', icon: Fingerprint, path: 'bvas' },
  { label: 'Voter Reg', icon: UserPlus, path: 'voter-registration', section: 'EMS' },
  { label: 'Workflow', icon: GitBranch, path: 'workflow-engine' },
  { label: 'BVAS Sync', icon: RefreshCw, path: 'bvas-sync' },
  { label: 'Portals', icon: Globe, path: 'portal-integration' },
  { label: 'Validation', icon: ShieldCheck, path: 'data-validation' },
  { label: 'Admin Console', icon: Settings, path: 'admin-console' },
  { label: 'Biometrics', icon: ScanFace, path: 'biometric', section: 'Advanced' },
  { label: 'Blockchain', icon: Link2, path: 'blockchain' },
  { label: 'Training', icon: GraduationCap, path: 'training' },
  { label: 'Stakeholders', icon: Users2, path: 'stakeholders' },
  { label: 'AI Monitoring', icon: BarChart3, path: 'ai-monitoring' },
  { label: 'Observer Monitor', icon: Eye, path: 'observer-monitoring', section: 'Monitoring' },
  { label: 'Disputes', icon: Eye, path: 'dispute-resolution', section: 'Monitoring' },
  { label: 'KYC Verification', icon: UserCheck, path: 'kyc-verification' },
  { label: 'Geofencing', icon: Crosshair, path: 'geofencing' },
  { label: 'Document AI', icon: FileSearch, path: 'document-ai' },
  { label: 'Duplicate Detection', icon: Copy, path: 'duplicate-detection' },
  { label: 'Export Center', icon: Download, path: 'export-center' },
  { label: 'Scale Health', icon: Activity, path: 'scale-health', section: 'Infrastructure' },
  { label: 'Production', icon: Shield, path: 'production', section: 'Infrastructure' },
  { label: 'Webhooks', icon: Webhook, path: 'webhooks', section: 'Admin' },
  { label: 'User Mgmt', icon: UserCog, path: 'user-management', section: 'Admin' },
  { label: 'Command Center', icon: Radio, path: 'command-center', section: 'Command' },
  { label: 'MFA Settings', icon: KeyRound, path: 'mfa' },
  { label: 'Citizen Portal', icon: UserSearch, path: 'citizen-portal' },
  { label: 'Predictive Analytics', icon: TrendingUp, path: 'predictive-analytics' },
  { label: 'Integrity Score', icon: Flame, path: 'integrity-score' },
  { label: 'TV Dashboard', icon: Tv, path: 'tv-dashboard' },
  { label: 'Compliance Report', icon: FileCheck, path: 'compliance-report' },
  { label: 'ML Dashboard', icon: Brain, path: 'ml-dashboard' },
  { label: 'GOTV Portal', icon: Megaphone, path: 'gotv-portal', section: 'Party' },
  { label: 'Party Primaries', icon: Vote, path: 'party-primaries', section: 'Party' },
];

interface LayoutProps {
  currentPage: string;
  onNavigate: (page: string) => void;
  children: React.ReactNode;
}

export default function Layout({ currentPage, onNavigate, children }: LayoutProps) {
  const { user, logout } = useAuth();
  const { lang, setLang } = useI18n();
  const { theme, setTheme, resolved } = useTheme();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const ThemeIcon = resolved === 'dark' ? Moon : Sun;

  const NavContent = () => (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-zinc-200 dark:border-zinc-700">
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-lg bg-green-700 flex items-center justify-center">
            <Vote className="w-5 h-5 text-white" />
          </div>
          <div>
            <h2 className="text-sm font-bold text-zinc-900 dark:text-zinc-100">INEC Platform</h2>
            <p className="text-xs text-zinc-500 dark:text-zinc-400">Election Results v4.0</p>
          </div>
        </div>
      </div>
      <nav className="flex-1 p-2 space-y-0.5 overflow-y-auto scrollbar-thin" aria-label="Main navigation" role="navigation">
        {NAV_ITEMS.map((item: typeof NAV_ITEMS[number]) => {
          const isActive = currentPage === item.path;
          return (
            <div key={item.path}>
              {item.section && (
                <div className="px-3 pt-4 pb-1" role="separator">
                  <span className="text-[10px] font-semibold text-zinc-400 dark:text-zinc-500 uppercase tracking-wider">{item.section}</span>
                </div>
              )}
              <button
                onClick={() => { onNavigate(item.path); setSidebarOpen(false); }}
                aria-current={isActive ? 'page' : undefined}
                aria-label={`Navigate to ${item.label}`}
                className={`w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-all duration-150 ${
                  isActive
                    ? 'bg-green-50 dark:bg-green-900/30 text-green-800 dark:text-green-300 font-medium shadow-sm'
                    : 'text-zinc-600 dark:text-zinc-400 hover:bg-zinc-100 dark:hover:bg-zinc-800 hover:text-zinc-900 dark:hover:text-zinc-200'
                }`}
              >
                <item.icon className={`w-4 h-4 flex-shrink-0 ${isActive ? 'text-green-700 dark:text-green-400' : ''}`} aria-hidden="true" />
                <span className="truncate">{item.label}</span>
                {isActive && <ChevronRight className="w-4 h-4 ml-auto flex-shrink-0" aria-hidden="true" />}
              </button>
            </div>
          );
        })}
      </nav>
      <div className="p-3 border-t border-zinc-200 dark:border-zinc-700 space-y-2">
        <div className="flex items-center gap-1 px-1">
          {([
            { value: 'light' as const, icon: Sun, label: 'Light' },
            { value: 'dark' as const, icon: Moon, label: 'Dark' },
            { value: 'system' as const, icon: Monitor, label: 'System' },
          ]).map(({ value, icon: Icon, label }) => (
            <button
              key={value}
              onClick={() => setTheme(value)}
              aria-label={`${label} theme`}
              className={`flex-1 flex items-center justify-center gap-1.5 py-1.5 rounded-md text-xs transition-colors ${
                theme === value
                  ? 'bg-zinc-200 dark:bg-zinc-700 text-zinc-900 dark:text-zinc-100 font-medium'
                  : 'text-zinc-500 dark:text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-300'
              }`}
            >
              <Icon className="w-3.5 h-3.5" />
              {label}
            </button>
          ))}
        </div>
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-lg bg-zinc-50 dark:bg-zinc-800">
          <div className="w-6 h-6 rounded-full bg-green-100 dark:bg-green-900 flex items-center justify-center">
            <Users className="w-3.5 h-3.5 text-green-700 dark:text-green-400" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-xs font-medium text-zinc-900 dark:text-zinc-100 truncate">{user?.full_name}</p>
            <p className="text-xs text-zinc-500 dark:text-zinc-400 capitalize">{user?.role?.replace('_', ' ')}</p>
          </div>
        </div>
      </div>
    </div>
  );

  return (
    <div className="min-h-screen bg-zinc-50 dark:bg-zinc-900 transition-colors duration-200">
      <aside className="hidden lg:flex lg:fixed lg:inset-y-0 lg:w-56 lg:flex-col border-r border-zinc-200 dark:border-zinc-700 bg-white dark:bg-zinc-800/50">
        <NavContent />
      </aside>

      <a href="#main-content" className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 bg-white dark:bg-zinc-800 border px-2 py-1 rounded text-sm z-[100]">Skip to content</a>
      <header className="lg:hidden sticky top-0 z-50 flex items-center justify-between px-4 h-14 bg-white/80 dark:bg-zinc-800/80 backdrop-blur-md border-b border-zinc-200 dark:border-zinc-700" role="banner">
        <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon"><Menu className="h-5 w-5" /></Button>
          </SheetTrigger>
          <SheetContent side="left" className="p-0 w-56">
            <NavContent />
          </SheetContent>
        </Sheet>
        <div className="flex items-center gap-2">
          <Vote className="w-5 h-5 text-green-700 dark:text-green-400" />
          <span className="font-bold text-sm text-zinc-900 dark:text-zinc-100">INEC Platform</span>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={() => setTheme(resolved === 'dark' ? 'light' : 'dark')} className="p-2 rounded-md text-zinc-500 hover:text-zinc-700 dark:text-zinc-400 dark:hover:text-zinc-200" aria-label="Toggle theme">
            <ThemeIcon className="w-4 h-4" />
          </button>
          <select aria-label="Language" className="text-sm border dark:border-zinc-600 rounded px-2 py-1 bg-white dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100" value={lang} onChange={(e) => setLang(e.target.value as 'en' | 'ha' | 'yo' | 'ig')}>
            <option value="en">EN</option>
            <option value="ha">HA</option>
            <option value="yo">YO</option>
            <option value="ig">IG</option>
          </select>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon">
                <Avatar className="h-8 w-8">
                <AvatarFallback className="text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300">
                  {user?.full_name?.split(' ').map(n => n[0]).join('').slice(0, 2)}
                </AvatarFallback>
              </Avatar>
            </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={logout}>
                <LogOut className="w-4 h-4 mr-2" /> Logout
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <div className="lg:pl-56">
        <header className="hidden lg:flex items-center justify-between px-6 h-14 bg-white/80 dark:bg-zinc-800/80 backdrop-blur-md border-b border-zinc-200 dark:border-zinc-700 sticky top-0 z-40" role="banner">
          <h1 className="text-lg font-semibold text-zinc-900 dark:text-zinc-100 capitalize">
            {currentPage.replace('-', ' ')}
          </h1>
          <div className="flex items-center gap-3" aria-label="User controls">
            <Badge variant="outline" className="text-green-700 dark:text-green-400 border-green-200 dark:border-green-800 bg-green-50 dark:bg-green-900/30" aria-live="polite">
              System Online
            </Badge>
            <select aria-label="Language" className="text-sm border dark:border-zinc-600 rounded px-2 py-1 bg-white dark:bg-zinc-800 text-zinc-900 dark:text-zinc-100" value={lang} onChange={(e) => setLang(e.target.value as 'en' | 'ha' | 'yo' | 'ig')}>
              <option value="en">EN</option>
              <option value="ha">HA</option>
              <option value="yo">YO</option>
              <option value="ig">IG</option>
            </select>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2" aria-label="User menu">
                  <Avatar className="h-7 w-7">
                    <AvatarFallback className="text-xs bg-green-100 dark:bg-green-900 text-green-800 dark:text-green-300">
                      {user?.full_name?.split(' ').map(n => n[0]).join('').slice(0, 2)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-sm text-zinc-900 dark:text-zinc-100">{user?.full_name}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => setTheme(resolved === 'dark' ? 'light' : 'dark')}>
                  <ThemeIcon className="w-4 h-4 mr-2" /> {resolved === 'dark' ? 'Light Mode' : 'Dark Mode'}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={logout}>
                  <LogOut className="w-4 h-4 mr-2" /> Logout
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>
        <main id="main-content" className="p-4 lg:p-6">{children}</main>
      </div>
    </div>
  );
}
