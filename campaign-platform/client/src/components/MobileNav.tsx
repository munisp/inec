import { Link, useLocation } from "wouter";
import { Home, Zap, Scale, BarChart2, UserCog } from "lucide-react";

const NAV_ITEMS = [
  { path: "/", icon: Home, label: "Home" },
  { path: "/war-room", icon: Zap, label: "War Room" },
  { path: "/legal-compliance", icon: Scale, label: "Compliance" },
  { path: "/results", icon: BarChart2, label: "Results" },
  { path: "/team", icon: UserCog, label: "Team" },
];

export default function MobileNav() {
  const [location] = useLocation();

  return (
    <nav
      aria-label="Campaign navigation"
      className="fixed bottom-0 left-0 right-0 z-50 border-t border-border bg-background/95 pb-[env(safe-area-inset-bottom)] backdrop-blur sm:hidden"
    >
      <div className="flex min-h-14 items-center justify-around gap-1 px-2 py-1.5">
        {NAV_ITEMS.map(({ path, icon: Icon, label }) => {
          const active = location === path;
          return (
            <Link
              key={path}
              href={path}
              aria-current={active ? "page" : undefined}
              className={`flex min-h-11 min-w-11 flex-1 flex-col items-center justify-center gap-0.5 rounded-md px-2 py-1.5 text-[10px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--inec-green)] focus-visible:ring-offset-2 ${
                active
                  ? "bg-[#4A1525]/10 text-[#4A1525]"
                  : "text-muted-foreground hover:bg-[#4A1525]/5 hover:text-[#4A1525]"
              }`}
            >
              <Icon size={20} aria-hidden="true" />
              <span className="truncate">{label}</span>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
