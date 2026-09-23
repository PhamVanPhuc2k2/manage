"use client";

import { useEffect, useRef, useState } from "react";

import * as api from "./api";
import { startRingtone } from "./ringtone";
import { useCallStore } from "./store";

/**
 * Màn hình đổ chuông.
 *
 * Nổi lên trên MỌI trang, không chỉ trang chat: người ta đang xem bảng công
 * việc thì cuộc gọi vẫn phải tới. Đó là lý do component này gắn ở AppShell.
 */
export function IncomingCall() {
  const incoming = useCallStore((s) => s.incoming);
  const stopRinging = useCallStore((s) => s.stopRinging);
  const join = useCallStore((s) => s.join);
  const setNotice = useCallStore((s) => s.setNotice);

  const [busy, setBusy] = useState(false);
  const [left, setLeft] = useState(0);

  // Chuông kêu chừng nào màn hình còn hiện. Dừng trong hàm dọn dẹp để nó
  // tắt theo mọi đường thoát — bấm nghe, bấm từ chối, hay hết giờ.
  useEffect(() => {
    if (!incoming) return;
    const ring = startRingtone();
    return () => ring.stop();
  }, [incoming]);

  // Đếm ngược theo expires_at của MÁY CHỦ, không phải một bộ đếm 45 giây
  // của client. Hai đồng hồ lệch nhau thì bộ đếm client sẽ tắt chuông sớm
  // hoặc muộn hơn lúc máy chủ thật sự bỏ cuộc gọi.
  const callId = incoming?.call_id;
  const expiresAt = incoming?.expires_at;
  useEffect(() => {
    if (!expiresAt || !callId) return;

    const tick = () => {
      const ms = new Date(expiresAt).getTime() - Date.now();
      setLeft(Math.max(0, Math.ceil(ms / 1000)));
      // Tự tắt khi hết giờ. Máy chủ cũng gửi call.cancelled, nhưng bản tin
      // đó có thể tới muộn hoặc lạc nếu mạng chớp — và một cái chuông
      // không chịu tắt là thứ người dùng nhớ rất lâu.
      if (ms <= 0) stopRinging(callId);
    };

    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [expiresAt, callId, stopRinging]);

  // Chặn bấm hai lần: người sốt ruột bấm "Nghe" ba cái, và ba request accept
  // chạy đua nhau thì hai cái sau nhận 409.
  const acting = useRef(false);

  if (!incoming) return null;

  const act = async (fn: () => Promise<void>) => {
    if (acting.current) return;
    acting.current = true;
    setBusy(true);
    try {
      await fn();
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "Không thực hiện được");
      stopRinging(incoming.call_id);
    } finally {
      acting.current = false;
      setBusy(false);
    }
  };

  const onAccept = () =>
    act(async () => {
      const joined = await api.acceptCall(incoming.call_id);
      join(joined, false);
    });

  const onReject = () =>
    act(async () => {
      await api.rejectCall(incoming.call_id);
      stopRinging(incoming.call_id);
    });

  return (
    <div className="fixed inset-x-0 bottom-6 z-50 flex justify-center px-4 sm:inset-x-auto sm:right-6">
      <div className="w-full max-w-sm animate-pulse-slow rounded-lg border border-neutral-200 bg-white p-4 shadow-xl dark:border-neutral-700 dark:bg-neutral-900">
        <div className="text-xs uppercase tracking-wide text-neutral-500">
          {incoming.kind === "video" ? "Cuộc gọi video đến" : "Cuộc gọi đến"}
        </div>
        <div className="mt-1 truncate text-lg font-medium">
          {incoming.initiator_name || "Không rõ người gọi"}
        </div>
        <div className="mt-0.5 text-xs text-neutral-500">
          Tự tắt sau {left} giây
        </div>

        <div className="mt-4 flex gap-2">
          <button
            onClick={onAccept}
            disabled={busy}
            className="flex-1 rounded bg-emerald-600 px-3 py-2 text-sm font-medium text-white transition hover:bg-emerald-700 disabled:opacity-50"
          >
            Nghe
          </button>
          <button
            onClick={onReject}
            disabled={busy}
            className="flex-1 rounded bg-red-600 px-3 py-2 text-sm font-medium text-white transition hover:bg-red-700 disabled:opacity-50"
          >
            Từ chối
          </button>
        </div>
      </div>
    </div>
  );
}
