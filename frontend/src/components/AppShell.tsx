import { useEffect, useRef, useState } from "react";
import { Outlet, useLocation } from "react-router-dom";
import { TopBar } from "./TopBar";
import { BottomNav } from "./BottomNav";
import { MiniPlayer } from "./MiniPlayer";
import { PlaybackProvider } from "@/lib/PlaybackContext";

// ホーム／プレイヤー／プロフィールの3タブで共有する画面の外枠。
// TopBar/BottomNavはposition:fixedで常に画面上下に固定し、スクロールするのは
// main部分だけにする（TopBarはPlayer画面だけ「閉じる」ボタン分だけ高さが変わる
// ため、固定の余白ではなく実際の高さを測ってmainのpadding量に反映する）。
export function AppShell() {
  const location = useLocation();
  const topRef = useRef<HTMLElement>(null);
  const bottomRef = useRef<HTMLElement>(null);
  const [topHeight, setTopHeight] = useState(0);
  const [bottomHeight, setBottomHeight] = useState(0);

  useEffect(() => {
    function measure() {
      setTopHeight(topRef.current?.offsetHeight ?? 0);
      setBottomHeight(bottomRef.current?.offsetHeight ?? 0);
    }
    measure();
    window.addEventListener("resize", measure);
    return () => window.removeEventListener("resize", measure);
  }, [location.pathname]);

  return (
    <PlaybackProvider>
      <div className="flex flex-1 flex-col">
        <TopBar ref={topRef} />
        <main
          className="min-h-0 flex-1 overflow-y-auto px-5 pb-6"
          style={{ paddingTop: topHeight, paddingBottom: bottomHeight }}
        >
          <Outlet />
        </main>
        <MiniPlayer />
        <BottomNav ref={bottomRef} />
      </div>
    </PlaybackProvider>
  );
}
