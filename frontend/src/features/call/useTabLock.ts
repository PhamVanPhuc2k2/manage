"use client";

import { useEffect } from "react";

import { useCallStore } from "./store";

const CHANNEL = "manage-call";

type TabMessage =
  | { type: "joined"; callId: string }
  | { type: "left"; callId: string }
  | { type: "who" };

/**
 * Đảm bảo một người chỉ ở trong cuộc gọi ở MỘT tab.
 *
 * Vì sao cần: mỗi tab mở một kết nối WebRTC riêng và publish một luồng
 * micro riêng. Hai tab cùng vào một phòng thì phòng có hai "người" cùng
 * tên, hai luồng tiếng của cùng một cái micro, và tiếng vọng lại giữa hai
 * tab tạo ra tiếng hú. Người dùng nghe thấy điều đó và kết luận là hệ
 * thống hỏng — họ không sai.
 *
 * Backend KHÔNG giải được việc này: với nó cả hai tab đều là cùng một nhân
 * viên đang có quyền hợp lệ. Đây là việc của client.
 *
 * BroadcastChannel chỉ thấy các tab CÙNG một trình duyệt trên cùng máy.
 * Người dùng mở cuộc gọi ở máy khác vẫn vào được — đó là hành vi mong
 * muốn, không phải lỗ hổng: máy tính và điện thoại là hai thiết bị thật.
 */
export function useTabLock(): void {
  const active = useCallStore((s) => s.active);
  const hangUp = useCallStore((s) => s.hangUp);
  const setNotice = useCallStore((s) => s.setNotice);

  const callId = active?.call.id;

  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;
    const ch = new BroadcastChannel(CHANNEL);

    ch.onmessage = (e: MessageEvent<TabMessage>) => {
      const msg = e.data;
      if (msg.type !== "joined") return;

      const mine = useCallStore.getState().active;
      if (!mine) return;

      // Một tab khác vừa vào ĐÚNG cuộc gọi này: nhường cho nó.
      //
      // Nhường cho tab MỚI chứ không giữ tab cũ, vì tab mới là nơi người
      // dùng đang thật sự nhìn — họ vừa bấm ở đó.
      if (mine.call.id === msg.callId) {
        hangUp(mine.call.id);
        setNotice("Cuộc gọi đã chuyển sang tab khác");
      }
    };

    if (callId) {
      const joined: TabMessage = { type: "joined", callId };
      ch.postMessage(joined);
    }

    return () => {
      if (callId) {
        const left: TabMessage = { type: "left", callId };
        ch.postMessage(left);
      }
      ch.close();
    };
  }, [callId, hangUp, setNotice]);
}
