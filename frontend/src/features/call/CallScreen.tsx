"use client";

import { useEffect, useRef, useState } from "react";

import { useAuth } from "@/lib/auth/useAuth";

import * as api from "./api";
import { useCallStore } from "./store";
import type { CallJoin } from "./types";
import { useLiveKitRoom } from "./useLiveKitRoom";
import { VideoTile } from "./VideoTile";

/**
 * Màn hình cuộc gọi.
 *
 * Phủ toàn màn hình khi đang mở, thu nhỏ thành một ô nổi khi người dùng bấm
 * thu gọn — họ cần tra một con số trong bảng lương GIỮA cuộc họp, và bắt họ
 * cúp máy để làm việc đó là thiết kế sai.
 */
export function CallScreen() {
  const active = useCallStore((s) => s.active);

  // Gắn theo KHOÁ là id cuộc gọi: cuộc gọi mới phải dựng một phòng
  // LiveKit mới từ đầu, không tái dùng phòng cũ với một token khác.
  if (!active) return null;
  return <CallStage key={active.call.id} join={active} />;
}

function CallStage({ join: active }: { join: CallJoin }) {
  const outgoing = useCallStore((s) => s.outgoing);
  const notice = useCallStore((s) => s.notice);
  const hangUp = useCallStore((s) => s.hangUp);
  const setNotice = useCallStore((s) => s.setNotice);
  const { user } = useAuth();

  const [minimized, setMinimized] = useState(false);
  const [micOn, setMicOn] = useState(true);
  const [camOn, setCamOn] = useState(false);
  const [sharing, setSharing] = useState(false);
  const [elapsed, setElapsed] = useState(0);

  const kind = active.call.kind;
  const { room, phase, error, tiles } = useLiveKitRoom(active, kind);

  // Trạng thái nút lấy từ PHÒNG chứ không phải từ state riêng.
  //
  // Bật camera có thể thất bại (máy không có webcam, người dùng từ chối
  // quyền), và một cái nút sáng lên trong khi camera vẫn tắt là lời nói dối
  // tệ nhất mà giao diện gọi video có thể nói.
  useEffect(() => {
    if (!room) return;
    const sync = () => {
      const p = room.localParticipant;
      setMicOn(p.isMicrophoneEnabled);
      setCamOn(p.isCameraEnabled);
      setSharing(p.isScreenShareEnabled);
    };
    sync();
    const t = setInterval(sync, 700);
    return () => clearInterval(t);
  }, [room]);

  // Đồng hồ cuộc gọi, tính từ mốc của MÁY CHỦ.
  const startedAt = active.call.started_at;
  useEffect(() => {
    if (!startedAt || outgoing) return;
    const base = new Date(startedAt).getTime();
    const tick = () => setElapsed(Math.max(0, Math.floor((Date.now() - base) / 1000)));
    tick();
    const t = setInterval(tick, 1000);
    return () => clearInterval(t);
  }, [startedAt, outgoing]);

  const acting = useRef(false);
  const guard = async (fn: () => Promise<void>) => {
    if (acting.current) return;
    acting.current = true;
    try {
      await fn();
    } catch (err) {
      setNotice(err instanceof Error ? err.message : "Không thực hiện được");
    } finally {
      acting.current = false;
    }
  };

  const callId = active.call.id;
  const isInitiator = active.call.initiator_id === user?.employee_id;

  const toggleMic = () =>
    guard(async () => {
      if (!room) return;
      await room.localParticipant.setMicrophoneEnabled(!micOn);
    });

  const toggleCam = () =>
    guard(async () => {
      if (!room) return;
      await room.localParticipant.setCameraEnabled(!camOn);
    });

  const toggleShare = () =>
    guard(async () => {
      if (!room) return;
      // Người dùng bấm "Huỷ" trên hộp chọn màn hình của trình duyệt thì
      // SDK ném lỗi. Đó không phải sự cố, nên không hiện thông báo lỗi.
      try {
        await room.localParticipant.setScreenShareEnabled(!sharing, {
          audio: true,
        });
      } catch {
        /* người dùng đổi ý */
      }
    });

  const leave = () =>
    guard(async () => {
      await api.leaveCall(callId);
      hangUp(callId);
    });

  const endForAll = () =>
    guard(async () => {
      await api.endCall(callId);
      hangUp(callId);
    });

  const pip = () =>
    guard(async () => {
      const el = document.querySelector<HTMLVideoElement>("video[data-remote]");
      if (!el) return;
      if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
      } else {
        await el.requestPictureInPicture();
      }
    });

  /* ---------------------------------------------------------- thu gọn */

  if (minimized) {
    return (
      <button
        onClick={() => setMinimized(false)}
        className="fixed bottom-6 left-6 z-50 flex items-center gap-2 rounded-full bg-emerald-600 px-4 py-2 text-sm text-white shadow-lg transition hover:bg-emerald-700"
      >
        <span className="h-2 w-2 animate-pulse rounded-full bg-white" />
        {outgoing ? "Đang gọi..." : clock(elapsed)}
        <span className="opacity-80">· mở lại</span>
      </button>
    );
  }

  /* ------------------------------------------------------- toàn màn hình */

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-neutral-950 text-white">
      <header className="flex items-center gap-3 px-4 py-3">
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">
            {kind === "video" ? "Cuộc gọi video" : "Cuộc gọi thoại"}
          </div>
          <div className="text-xs text-neutral-400">
            {outgoing
              ? "Đang đổ chuông..."
              : phase === "connecting"
                ? "Đang kết nối..."
                : phase === "failed"
                  ? "Không kết nối được"
                  : clock(elapsed)}
          </div>
        </div>
        <button
          onClick={() => setMinimized(true)}
          className="rounded border border-neutral-700 px-3 py-1 text-sm transition hover:bg-neutral-800"
        >
          Thu gọn
        </button>
      </header>

      {(error || notice) && (
        <div className="mx-4 mb-2 rounded bg-amber-500/15 px-3 py-2 text-sm text-amber-200">
          {error || notice}
        </div>
      )}

      <div className="flex-1 overflow-auto p-4">
        {tiles.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-neutral-400">
            Đang chờ người khác vào phòng...
          </div>
        ) : (
          <div
            className={`grid gap-3 ${
              tiles.length === 1
                ? "grid-cols-1"
                : tiles.length <= 4
                  ? "grid-cols-1 sm:grid-cols-2"
                  : "grid-cols-2 lg:grid-cols-3"
            }`}
          >
            {tiles.map((t) => (
              <VideoTile key={t.key} tile={t} />
            ))}
          </div>
        )}
      </div>

      <footer className="flex flex-wrap items-center justify-center gap-2 border-t border-neutral-800 px-4 py-3">
        <ControlButton on={micOn} onClick={toggleMic}>
          {micOn ? "Tắt mic" : "Bật mic"}
        </ControlButton>
        <ControlButton on={camOn} onClick={toggleCam}>
          {camOn ? "Tắt camera" : "Bật camera"}
        </ControlButton>
        <ControlButton on={sharing} onClick={toggleShare}>
          {sharing ? "Dừng chia sẻ" : "Chia sẻ màn hình"}
        </ControlButton>
        <ControlButton on={false} onClick={pip}>
          Cửa sổ nhỏ
        </ControlButton>

        <div className="mx-2 h-6 w-px bg-neutral-800" />

        <button
          onClick={leave}
          className="rounded-full bg-red-600 px-5 py-2 text-sm font-medium transition hover:bg-red-700"
        >
          Rời cuộc gọi
        </button>
        {isInitiator && (
          // Chỉ người khởi tạo thấy nút này. "Tôi xong rồi" và "cuộc họp
          // này xong rồi" là hai việc khác nhau, và backend cũng chặn.
          <button
            onClick={endForAll}
            className="rounded-full border border-red-600 px-4 py-2 text-sm text-red-400 transition hover:bg-red-600/15"
          >
            Kết thúc cho tất cả
          </button>
        )}
      </footer>

      {/* Ô hình đầu tiên của người KHÁC, dùng làm nguồn cho cửa sổ nhỏ. */}
      <RemoteAnchor />
    </div>
  );
}

function ControlButton({
  on,
  onClick,
  children,
}: {
  on: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      className={`rounded-full px-4 py-2 text-sm transition ${
        on
          ? "bg-white text-neutral-900 hover:bg-neutral-200"
          : "border border-neutral-700 hover:bg-neutral-800"
      }`}
    >
      {children}
    </button>
  );
}

/**
 * Đánh dấu một thẻ video của người khác cho Picture-in-Picture.
 *
 * Trình duyệt chỉ cho phép thu nhỏ MỘT thẻ video, và thẻ đó phải là của
 * người khác: thu nhỏ hình của chính mình thì không ai làm.
 */
function RemoteAnchor() {
  useEffect(() => {
    const mark = () => {
      const all = Array.from(document.querySelectorAll("video"));
      for (const v of all) v.removeAttribute("data-remote");
      const remote = all.find((v) => !v.muted);
      remote?.setAttribute("data-remote", "1");
    };
    mark();
    const t = setInterval(mark, 1000);
    return () => clearInterval(t);
  }, []);
  return null;
}

function clock(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}
