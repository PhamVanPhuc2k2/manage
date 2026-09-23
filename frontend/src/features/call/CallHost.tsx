"use client";

import { useEffect } from "react";

import { CallScreen } from "./CallScreen";
import { IncomingCall } from "./IncomingCall";
import { useCallStore } from "./store";
import { useCallRealtime } from "./useCallRealtime";
import { useTabLock } from "./useTabLock";

/**
 * Gắn một lần ở AppShell.
 *
 * Cuộc gọi phải sống ngoài cây route: người ta vừa họp vừa mở bảng công
 * việc, và nếu màn hình gọi nằm trong trang chat thì rời trang là rớt cuộc
 * gọi.
 */
export function CallHost() {
  useCallRealtime();
  useTabLock();

  // Cảnh báo trước khi đóng tab GIỮA cuộc gọi.
  //
  // Đóng tab làm rớt WebSocket, và backend sẽ coi đó là mất kết nối rồi cho
  // người này rời phòng. Không có gì hỏng, nhưng một cú lỡ tay làm mất chỗ
  // trong cuộc họp thì đáng hỏi lại một câu.
  const active = useCallStore((s) => s.active);
  useEffect(() => {
    if (!active) return;
    const onLeave = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", onLeave);
    return () => window.removeEventListener("beforeunload", onLeave);
  }, [active]);

  return (
    <>
      <IncomingCall />
      <CallScreen />
    </>
  );
}
