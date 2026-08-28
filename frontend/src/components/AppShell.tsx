import { Outlet } from "react-router-dom";
import { TopBar } from "./TopBar";
import { BottomNav } from "./BottomNav";

// ホーム／プレイヤー／プロフィールの3タブで共有する画面の外枠。
export function AppShell() {
  return (
    <div className="flex flex-1 flex-col">
      <TopBar />
      <main className="min-h-0 flex-1 overflow-y-auto px-5 pb-6">
        <Outlet />
      </main>
      <BottomNav />
    </div>
  );
}
