"use client";

import Link from "next/link";

import { useWsStatus } from "@/lib/ws/useWebSocket";
import { useToday } from "./queries";
import { PRESENCE_DOT, PRESENCE_LABEL, formatDuration } from "./types";

/**
 * Widget trạng thái làm việc trên topbar.
 *
 * Hiện thời gian làm việc hôm nay và trạng thái kết nối. Việc hiển thị này
 * là MINH BẠCH DỮ LIỆU, không phải trang trí: nhân viên phải luôn nhìn thấy
 * rằng thời gian online của họ đang được ghi nhận, và thấy đúng con số mà hệ
 * thống đang ghi. Đó là điều kiện để cơ chế chấm công qua presence không bị
 * cảm nhận như một công cụ theo dõi ngầm.
 */
export function WorkStatusWidget() {
  const { data, isPending } = useToday();
  const wsStatus = useWsStatus();

  if (isPending || !data) {
    return <div className="text-sm text-neutral-400">Đang tải...</div>;
  }

  // Trạng thái hiển thị bám theo KẾT NỐI trước, rồi mới tới presence: mất
  // WebSocket thì thời gian không còn được ghi nhận, và người dùng cần biết
  // điều đó ngay chứ không phải cuối ngày mới phát hiện thiếu giờ.
  const status = wsStatus === "open" ? data.status : "offline";

  const pct = data.expected_minutes
    ? Math.min((data.online_minutes / data.expected_minutes) * 100, 100)
    : 0;

  return (
    <Link
      href="/attendance"
      title={
        wsStatus === "open"
          ? PRESENCE_LABEL[status]
          : "Mất kết nối — thời gian làm việc tạm thời không được ghi nhận"
      }
      className="flex items-center gap-2 rounded px-2 py-1 text-sm transition hover:bg-neutral-100 dark:hover:bg-neutral-800"
    >
      <span
        className={`h-2 w-2 shrink-0 rounded-full ${PRESENCE_DOT[status]} ${
          status === "online" ? "animate-pulse" : ""
        }`}
      />

      <span className="tabular-nums">{formatDuration(data.online_minutes)}</span>

      {data.expected_minutes > 0 && (
        <span className="hidden h-1 w-12 overflow-hidden rounded-full bg-neutral-200 sm:block dark:bg-neutral-700">
          <span
            className="block h-full rounded-full bg-neutral-900 dark:bg-white"
            style={{ width: `${pct}%` }}
          />
        </span>
      )}
    </Link>
  );
}
