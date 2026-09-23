"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";

import { usePermission } from "@/lib/auth/useAuth";

import * as api from "./api";
import { useCallStore } from "./store";
import type { CallKind } from "./types";

/**
 * Nút gọi trên thanh tiêu đề hội thoại.
 *
 * Hỏi máy chủ xem hội thoại này có cuộc gọi đang diễn ra không, để hiện
 * "Tham gia" thay vì "Gọi". Không hỏi thì người vào muộn sẽ bấm gọi và
 * backend cho họ vào đúng cuộc đang chạy — đúng nhưng nhãn nút nói sai,
 * và họ ngần ngại bấm vì sợ làm phiền cả nhóm.
 */
export function CallButtons({ conversationId }: { conversationId: string }) {
  const { can } = usePermission();
  const join = useCallStore((s) => s.join);
  const active = useCallStore((s) => s.active);
  const setNotice = useCallStore((s) => s.setNotice);
  const [busy, setBusy] = useState(false);

  const { data: live } = useQuery({
    queryKey: ["calls", "live", conversationId],
    queryFn: () => api.liveCall(conversationId),
    // Làm mới định kỳ: cuộc gọi bắt đầu ở hội thoại mình KHÔNG mở sẽ không
    // có bản tin nào tới (chỉ thành viên được mời mới nhận call.incoming,
    // và nhóm lớn thì mọi người đều là thành viên nhưng chuông chỉ reo một
    // lần). 15 giây là đủ để nút đổi trước khi người ta để ý.
    refetchInterval: 15_000,
    enabled: can("call:read"),
  });

  if (!can("call:start")) return null;

  // Đang ở trong một cuộc gọi rồi thì không hiện nút gọi cuộc khác.
  if (active) return null;

  const start = async (kind: CallKind) => {
    if (busy) return;
    setBusy(true);
    try {
      const joined = await api.startCall(conversationId, kind);
      // outgoing = true khi CHƯA ai bắt máy. Tham gia một cuộc đang chạy
      // thì vào thẳng phòng, không phải màn hình "đang gọi...".
      join(joined, joined.call.status === "ringing");
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "Không gọi được");
    } finally {
      setBusy(false);
    }
  };

  if (live) {
    return (
      <button
        onClick={() => void start(live.kind)}
        disabled={busy}
        className="rounded bg-emerald-600 px-2.5 py-1 text-sm text-white transition hover:bg-emerald-700 disabled:opacity-50"
      >
        Tham gia cuộc gọi
      </button>
    );
  }

  return (
    <>
      <button
        onClick={() => void start("audio")}
        disabled={busy}
        title="Gọi thoại"
        className="rounded border border-neutral-300 px-2.5 py-1 text-sm transition hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
      >
        Gọi
      </button>
      <button
        onClick={() => void start("video")}
        disabled={busy}
        title="Gọi video"
        className="rounded border border-neutral-300 px-2.5 py-1 text-sm transition hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
      >
        Gọi video
      </button>
    </>
  );
}
