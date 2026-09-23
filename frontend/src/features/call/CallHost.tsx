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
      <CallNotice />
    </>
  );
}

/**
 * Dải báo nhỏ khi KHÔNG có màn hình cuộc gọi nào đang mở.
 *
 * Màn hình cuộc gọi có chỗ hiện báo của riêng nó, nhưng những câu đáng
 * nói nhất lại rơi vào lúc không có màn hình nào: bấm "Nghe" nhưng cuộc
 * gọi vừa kết thúc, người nhận đang bận, hay hết giờ đổ chuông. Không
 * có dải này thì những câu đó đi thẳng vào hư vô và người dùng chỉ thấy
 * màn hình đứng im.
 */
function CallNotice() {
  const notice = useCallStore((s) => s.notice);
  const active = useCallStore((s) => s.active);

  if (!notice || active) return null;

  return (
    <div
      role="status"
      className="fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-full bg-neutral-900 px-4 py-2 text-sm text-white shadow-lg dark:bg-neutral-100 dark:text-neutral-900"
    >
      {notice}
    </div>
  );
}
