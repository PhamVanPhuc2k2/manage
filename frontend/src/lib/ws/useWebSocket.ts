"use client";

import { useEffect, useState } from "react";

import { useAuthStore } from "../auth/store";
import { wsClient, type Envelope, type WsStatus } from "./client";

/**
 * Mở kết nối WebSocket khi đã đăng nhập, đóng khi đăng xuất.
 *
 * Gọi ĐÚNG MỘT LẦN ở component gốc của khu vực cần đăng nhập (AppShell).
 * Gọi ở nhiều nơi không làm hỏng gì — client là singleton và connect() tự
 * bỏ qua khi đã có kết nối — nhưng cũng không có tác dụng gì thêm.
 */
export function useWebSocketConnection() {
  const user = useAuthStore((s) => s.user);

  useEffect(() => {
    if (!user) {
      wsClient.disconnect();
      return;
    }
    void wsClient.connect();

    // KHÔNG ngắt kết nối trong hàm dọn dẹp của effect này.
    //
    // React 18 ở chế độ Strict chạy effect hai lần khi phát triển, nên ngắt
    // ở đây sẽ đóng đúng kết nối vừa mở. Việc ngắt do nhánh `!user` phía
    // trên lo — đó mới là lúc thật sự cần đóng.
  }, [user]);
}

/** Trạng thái kết nối, để hiển thị chấm màu trên giao diện. */
export function useWsStatus(): WsStatus {
  const [status, setStatus] = useState<WsStatus>(() => wsClient.getStatus());

  useEffect(() => wsClient.onStatus(setStatus), []);

  return status;
}

/**
 * Lắng nghe một loại bản tin.
 *
 * Handler được giữ trong ref ngầm qua closure của effect: truyền một hàm mới
 * mỗi lần render sẽ đăng ký lại liên tục, nên nơi gọi nên bọc handler trong
 * useCallback hoặc khai báo ngoài component.
 */
export function useWsMessage(
  type: string,
  handler: (e: Envelope) => void,
): void {
  useEffect(() => {
    return wsClient.subscribe((e) => {
      if (e.type === type) handler(e);
    });
  }, [type, handler]);
}
