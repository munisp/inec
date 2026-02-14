import { useState } from 'react';
import { useAuth } from '@/lib/auth';
import { useI18n } from '@/lib/i18n';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetTrigger } from '@/components/ui/sheet';
import { Avatar, AvatarFallback } from '@/components/ui/avatar';
import { Badge } from '@/components/ui/badge';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu';
import {
  LayoutDashboard, Vote, FileBarChart, Shield, AlertTriangle,
  Menu, LogOut, ChevronRight, Landmark, MapPin, Users, Map, Layers, Fingerprint,
  Brain, MessageSquare, Code2, UserPlus, GitBranch, RefreshCw, Globe, ShieldCheck, Settings,
  ScanFace, Link2, GraduationCap, Users2, BarChart3
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
];

interface LayoutProps {
  currentPage: string;
  onNavigate: (page: string) => void;
  children: React.ReactNode;
}

export default function Layout({ currentPage, onNavigate, children }: LayoutProps) {
  const { user, logout } = useAuth();
  const { lang, setLang } = useI18n();
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const NavContent = () => (
    <div className="flex flex-col h-full">
      <div className="p-4 border-b border-zinc-200">
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-lg bg-green-700 flex items-center justify-center">
            <Vote className="w-5 h-5 text-white" />
          </div>
          <div>
            <h2 className="text-sm font-bold text-zinc-900">INEC Platform</h2>
            <p className="text-xs text-zinc-500">Election Results v4.0</p>
          </div>
        </div>
      </div>
      <nav className="flex-1 p-2 space-y-1 overflow-y-auto">
        {NAV_ITEMS.map((item: any) => {
          const isActive = currentPage === item.path;
          return (
            <div key={item.path}>
              {item.section && (
                <div className="px-3 pt-3 pb-1">
                  <span className="text-[10px] font-semibold text-zinc-400 uppercase tracking-wider">{item.section}</span>
                </div>
              )}
              <button
                onClick={() => { onNavigate(item.path); setSidebarOpen(false); }}
                className={`w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm transition-colors ${
                  isActive
                    ? 'bg-green-50 text-green-800 font-medium'
                    : 'text-zinc-600 hover:bg-zinc-100 hover:text-zinc-900'
                }`}
              >
                <item.icon className={`w-4 h-4 ${isActive ? 'text-green-700' : ''}`} />
                {item.label}
                {isActive && <ChevronRight className="w-4 h-4 ml-auto" />}
              </button>
            </div>
          );
        })}
      </nav>
      <div className="p-3 border-t border-zinc-200">
        <div className="flex items-center gap-2 px-2 py-1.5 rounded-lg bg-zinc-50">
          <div className="w-6 h-6 rounded-full bg-green-100 flex items-center justify-center">
            <Users className="w-3.5 h-3.5 text-green-700" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-xs font-medium text-zinc-900 truncate">{user?.full_name}</p>
            <p className="text-xs text-zinc-500 capitalize">{user?.role?.replace('_', ' ')}</p>
          </div>
        </div>
      </div>
    </div>
  );

  return (
    <div className="min-h-screen bg-zinc-50">
      <aside className="hidden lg:flex lg:fixed lg:inset-y-0 lg:w-56 lg:flex-col border-r border-zinc-200 bg-white">
        <NavContent />
      </aside>

      <a href="#main-content" className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 bg-white border px-2 py-1 rounded text-sm">Skip to content</a>
      <header className="lg:hidden sticky top-0 z-50 flex items-center justify-between px-4 h-14 bg-white border-b border-zinc-200" role="banner">
        <Sheet open={sidebarOpen} onOpenChange={setSidebarOpen}>
          <SheetTrigger asChild>
            <Button variant="ghost" size="icon"><Menu className="h-5 w-5" /></Button>
          </SheetTrigger>
          <SheetContent side="left" className="p-0 w-56">
            <NavContent />
          </SheetContent>
        </Sheet>
        <div className="flex items-center gap-2">
          <Vote className="w-5 h-5 text-green-700" />
          <span className="font-bold text-sm">INEC Platform</span>
        </div>
        <div className="flex items-center gap-2">
          <select aria-label="Language" className="text-sm border rounded px-2 py-1" value={lang} onChange={(e)=> setLang(e.target.value as any)}>
            <option value="en">EN</option>
            <option value="ha">HA</option>
            <option value="yo">YO</option>
            <option value="ig">IG</option>
          </select>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon">
                <Avatar className="h-8 w-8">
                <AvatarFallback className="text-xs bg-green-100 text-green-800">
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
        <header className="hidden lg:flex items-center justify-between px-6 h-14 bg-white border-b border-zinc-200" role="banner">
          <h1 className="text-lg font-semibold text-zinc-900 capitalize">
            {currentPage.replace('-', ' ')}
          </h1>
          <div className="flex items-center gap-3" aria-label="User controls">
            <Badge variant="outline" className="text-green-700 border-green-200 bg-green-50" aria-live="polite">
              System Online
            </Badge>
            <select aria-label="Language" className="text-sm border rounded px-2 py-1" value={lang} onChange={(e)=> setLang(e.target.value as any)}>
              <option value="en">EN</option>
              <option value="ha">HA</option>
              <option value="yo">YO</option>
              <option value="ig">IG</option>
            </select>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="sm" className="gap-2" aria-label="User menu">
                  <Avatar className="h-7 w-7">
                    <AvatarFallback className="text-xs bg-green-100 text-green-800">
                      {user?.full_name?.split(' ').map(n => n[0]).join('').slice(0, 2)}
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-sm">{user?.full_name}</span>
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
        <main id="main-content" className="p-4 lg:p-6">{children}</main>
      </div>
    </div>
  );
}
