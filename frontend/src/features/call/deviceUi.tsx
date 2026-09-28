"use client";

import { useEffect, useState } from "react";
import { createAudioAnalyser, type LocalAudioTrack } from "livekit-client";

/**
 * Phần giao diện chọn thiết bị dùng chung cho bảng Thiết bị trong cuộc gọi
 * và trang Kiểm tra thiết bị.
 *
 * `tone="call"` là nền tối cố định của màn hình cuộc gọi; `tone="page"` đi
 * theo giao diện sáng/tối của phần còn lại.
 */
type Tone = "call" | "page";

export function DeviceSelect({
  label,
  devices,
  value,
  onPick,
  tone = "call",
}: {
  label: string;
  devices: MediaDeviceInfo[];
  value: string | undefined;
  onPick: (deviceId: string) => void;
  tone?: Tone;
}) {
  return (
    <label className="mb-3 block">
      <span
        className={`mb-1 block text-xs ${tone === "call" ? "text-neutral-400" : "text-neutral-500"}`}
      >
        {label}
      </span>
      <select
        // Chưa từng đổi thiết bị thì SDK chưa ghi nhận gì — lúc đó trình
        // duyệt đang dùng thiết bị mặc định, tức dòng đầu danh sách.
        value={
          value && devices.some((d) => d.deviceId === value)
            ? value
            : (devices[0]?.deviceId ?? "")
        }
        disabled={devices.length === 0}
        onChange={(e) => onPick(e.target.value)}
        className={
          tone === "call"
            ? "w-full rounded border border-neutral-700 bg-neutral-800 px-2 py-1 text-sm"
            : "w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        }
      >
        {devices.length === 0 && (
          <option value="">Không tìm thấy thiết bị</option>
        )}
        {devices.map((d, i) => (
          <option key={d.deviceId} value={d.deviceId}>
            {/*
              Nhãn thiết bị chỉ có sau khi người dùng đã cấp quyền. Trước
              đó trình duyệt trả về chuỗi rỗng — để nguyên thì danh sách
              là mấy dòng trắng không chọn được.
            */}
            {d.label || `Thiết bị ${i + 1}`}
          </option>
        ))}
      </select>
    </label>
  );
}

/**
 * Mức âm thanh của một track micro, đo NGAY TRÊN MÁY bằng AnalyserNode.
 *
 * Không đọc participant.audioLevel: con số đó do SFU gửi về và chỉ khác 0
 * khi SFU coi mình là "người đang nói" — người nói nhỏ sẽ thấy thanh đứng
 * im và kết luận micro hỏng.
 *
 * `key` đổi thì dựng lại bộ đo: đổi micro là SDK thay mediaStreamTrack bên
 * dưới, và bộ đo gắn vào track cũ sẽ đo một track đã dừng.
 */
export function useMicLevel(
  track: LocalAudioTrack | undefined,
  live: boolean,
  key?: string,
): number {
  const [level, setLevel] = useState(0);

  useEffect(() => {
    if (!track || !live) return;
    let analyser: ReturnType<typeof createAudioAnalyser>;
    try {
      // Dải -100..-30 dB là mặc định của Web Audio. Mặc định của SDK
      // (-100..-80) hẹp tới mức gần như mọi tiếng động đều đầy thanh, kể cả
      // tiếng ồn nền — thanh luôn đầy thì cũng vô dụng như thanh đứng im.
      analyser = createAudioAnalyser(track, {
        minDecibels: -100,
        maxDecibels: -30,
      });
    } catch {
      return;
    }
    const t = setInterval(() => setLevel(analyser.calculateVolume()), 100);
    return () => {
      clearInterval(t);
      void analyser.cleanup();
    };
  }, [track, live, key]);

  // Mic tắt thì hiện 0 luôn, không giữ lại số đo cuối cùng trước khi tắt.
  return track && live ? level : 0;
}

export function LevelBar({
  level,
  hint,
  tone = "call",
}: {
  level: number;
  hint: string;
  tone?: Tone;
}) {
  return (
    <div className="mb-3">
      <div
        className={`mb-1 text-xs ${tone === "call" ? "text-neutral-400" : "text-neutral-500"}`}
      >
        {hint}
      </div>
      <div
        role="meter"
        aria-label="Mức âm thanh micro"
        aria-valuemin={0}
        aria-valuemax={1}
        aria-valuenow={level}
        className={`h-2 overflow-hidden rounded ${tone === "call" ? "bg-neutral-800" : "bg-neutral-200 dark:bg-neutral-800"}`}
      >
        <div
          className="h-full bg-emerald-500 transition-[width] duration-100"
          // Nhân 3 vì tiếng nói bình thường chỉ quanh 0.1–0.3. Để nguyên
          // thì thanh gần như không nhúc nhích và người dùng kết luận là
          // micro hỏng. Hệ số này mới đo với micro GIẢ của Chromium (bíp
          // liên tục, ~0.15–0.3) — cần chỉnh lại khi thử với micro thật.
          style={{ width: `${Math.min(100, level * 300)}%` }}
        />
      </div>
    </div>
  );
}
