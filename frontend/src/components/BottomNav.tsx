import { Link, useLocation } from "react-router-dom";

const TABS = [
  {
    href: "/home",
    label: "ホーム",
    icon: <path d="M3 11l9-8 9 8M5 10v10h14V10" />,
  },
  {
    href: "/player",
    label: "プレイヤー",
    icon: <circle cx="12" cy="12" r="9" />,
  },
  {
    href: "/profile",
    label: "プロフィール",
    icon: (
      <>
        <circle cx="12" cy="8" r="4" />
        <path d="M4 21c0-4 4-6 8-6s8 2 8 6" />
      </>
    ),
  },
] as const;

export function BottomNav() {
  const location = useLocation();

  return (
    <nav
      className="flex shrink-0 border-t border-bg-elevated-2 bg-bg/95 backdrop-blur-md"
      style={{ paddingBottom: "max(8px, env(safe-area-inset-bottom))" }}
    >
      {TABS.map((tab) => {
        const active = location.pathname.startsWith(tab.href);
        return (
          <Link
            key={tab.href}
            to={tab.href}
            className={`flex flex-1 flex-col items-center gap-1 py-2 text-[11px] transition-colors ${
              active ? "text-accent" : "text-text-tertiary hover:text-text-secondary"
            }`}
          >
            <svg
              viewBox="0 0 24 24"
              width="20"
              height="20"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              {tab.icon}
            </svg>
            <span>{tab.label}</span>
          </Link>
        );
      })}
    </nav>
  );
}
