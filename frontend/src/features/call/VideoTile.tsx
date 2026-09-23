"use client";

import { useEffect, useRef } from "react";

import type { Tile } from "./useLiveKitRoom";

/**
 * Một ô hình.
 *
 * Việc gắn luồng media vào thẻ <video> đi qua attach/detach của SDK chứ
 * không phải gán srcObject bằng tay: SDK còn lo việc đăng ký với bộ đếm
 * người xem, và chính bộ đếm đó là thứ cho phép dynacast dừng gửi lớp độ
 * nét cao mà không ai xem.
 */
export function VideoTile({ tile }: { tile: Tile }) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const track = tile.publication?.track;

  useEffect(() => {
    const el = videoRef.current;
    if (!el || !track) return;
    track.attach(el);
    return () => {
      track.detach(el);
    };
  }, [track]);

  const hasVideo = !!track && !tile.publication?.isMuted;

  return (
    <div
      className={`relative overflow-hidden rounded-lg bg-neutral-900 ${
        tile.speaking ? "ring-2 ring-emerald-400" : ""
      }`}
    >
      {hasVideo ? (
        <video
          ref={videoRef}
          autoPlay
          playsInline
          // Luồng của chính mình phải TẮT TIẾNG, nếu không người dùng nghe
          // lại giọng mình vọng về và tưởng máy hỏng.
          muted={tile.isLocal}
          // Camera của mình lật ngang để giống soi gương. Màn hình chia sẻ
          // thì KHÔNG lật — chữ trên slide sẽ ngược hết.
          className={`h-full w-full ${
            tile.isScreen ? "object-contain" : "object-cover"
          } ${tile.isLocal && !tile.isScreen ? "-scale-x-100" : ""}`}
        />
      ) : (
        <div className="flex aspect-video items-center justify-center">
          <div className="flex h-16 w-16 items-center justify-center rounded-full bg-neutral-700 text-xl font-medium text-white">
            {initials(tile.name)}
          </div>
        </div>
      )}

      <div className="absolute inset-x-0 bottom-0 flex items-center gap-1.5 bg-gradient-to-t from-black/70 to-transparent px-2 py-1.5">
        <span className="truncate text-xs text-white">
          {tile.name}
          {tile.isLocal && !tile.isScreen ? " (bạn)" : ""}
        </span>
        {!tile.isScreen && !tile.micOn && (
          <span className="rounded bg-red-600 px-1 text-[10px] text-white">
            tắt mic
          </span>
        )}
      </div>
    </div>
  );
}

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "?";
  // Tên tiếng Việt viết họ trước, nên chữ cái ĐẦU của từ CUỐI mới là tên
  // gọi. "Nguyễn Văn An" ra "A", không phải "N".
  return (parts[parts.length - 1][0] ?? "?").toUpperCase();
}
