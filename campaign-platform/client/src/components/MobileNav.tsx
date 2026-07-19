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
    <nav className="fixed bottom-0 left-0 right-0 z-50 sm:hidden border-t border-gray-200 bg-white">
      <div className="flex items-center justify-around px-2 py-2">
        {NAV_ITEMS.map(({ path, icon: Icon, label }) => {
          const active = location === path;
          return (
            <Link key={path} href={path}>
              <div className="flex flex-col items-center gap-0.5 px-3 py-1 cursor-pointer">
                <Icon
                  size={20}
                  style={{ color: active ? "#4A1525" : "#9CA3AF" }}
                />
                <span
                  className="text-[10px] font-medium"
                  style={{ color: active ? "#4A1525" : "#9CA3AF" }}
                >
                  {label}
                </span>
              </div>
            </Link>
          );
        })}
      </div>
    </nav>
  );
}
