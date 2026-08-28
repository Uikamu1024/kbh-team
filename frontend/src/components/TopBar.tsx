import { useNavigate, useLocation } from "react-router-dom";
import { formatDateLabel } from "@/lib/format";

export function TopBar() {
  const navigate = useNavigate();
  const location = useLocation();
  const showBack = location.pathname.startsWith("/player");
  const today = formatDateLabel(new Date().toISOString());

  return (
    <header className="shrink-0 px-5 pb-2 pt-4">
      <div className="flex items-center justify-between">
        <span className="text-[17px] font-bold tracking-tight">通学ラジオ</span>
        <span className="text-[13px] text-text-secondary">{today}</span>
      </div>
      {showBack && (
        <button
          type="button"
          aria-label="閉じる"
          onClick={() => navigate("/home")}
          className="mt-3 flex h-8 w-8 items-center justify-center rounded-full hover:bg-bg-elevated-2"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M18 15l-6-6-6 6" />
          </svg>
        </button>
      )}
    </header>
  );
}
