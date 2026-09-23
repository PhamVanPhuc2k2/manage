"use client";

import { useEffect, useState } from "react";
import { Room, type LocalParticipant } from "livekit-client";

/**
 * Bảng chọn thiết bị, mở ngay TRONG cuộc gọi.
 *
 * Vì sao không phải một màn hình chặn trước khi vào phòng: người ta phát
 * hiện thiết bị sai vào đúng lúc có người nói "không nghe thấy gì", tức là
 * GIỮA cuộc gọi. Một màn hình kiểm tra trước khi vào chỉ làm chậm mọi cuộc
 * gọi bình thường và vẫn không cứu được đúng tình huống đó.
 *
 * Thanh đo mức âm thanh là phần quan trọng nhất ở đây. Nó trả lời câu hỏi
 * mà danh sách thiết bị không trả lời được: "micro tôi chọn có thật sự thu
 * được tiếng tôi không". Nhìn thanh nhảy khi mình nói là xong, không phải
 * gọi thêm một người nữa để thử.
 */
export function DevicePanel({
  room,
  onClose,
}: {
  room: Room;
  onClose: () => void;
}) {
  const [mics, setMics] = useState<MediaDeviceInfo[]>([]);
  const [cams, setCams] = useState<MediaDeviceInfo[]>([]);
  const [speakers, setSpeakers] = useState<MediaDeviceInfo[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Tải danh sách MỘT lần khi mở, và tải lại khi người dùng cắm hay rút
  // thiết bị. Không có bước thứ hai thì cắm tai nghe giữa cuộc gọi xong
  // mở bảng này ra vẫn không thấy nó.
  useEffect(() => {
    const load = async () => {
      try {
        setMics(await Room.getLocalDevices("audioinput"));
        setCams(await Room.getLocalDevices("videoinput"));
        setSpeakers(await Room.getLocalDevices("audiooutput"));
      } catch {
        setError("Không đọc được danh sách thiết bị");
      }
    };

    void load();
    navigator.mediaDevices?.addEventListener("devicechange", load);
    return () => {
      navigator.mediaDevices?.removeEventListener("devicechange", load);
    };
  }, []);

  const pick = async (kind: MediaDeviceKind, deviceId: string) => {
    try {
      await room.switchActiveDevice(kind, deviceId);
      setError(null);
    } catch {
      setError("Không chuyển được sang thiết bị này");
    }
  };

  return (
    <div className="absolute bottom-20 left-1/2 z-10 w-80 -translate-x-1/2 rounded-lg border border-neutral-700 bg-neutral-900 p-4 text-sm shadow-xl">
      <div className="mb-3 flex items-center justify-between">
        <span className="font-medium">Thiết bị</span>
        <button
          onClick={onClose}
          className="text-neutral-400 transition hover:text-white"
        >
          Đóng
        </button>
      </div>

      {error && (
        <div className="mb-3 rounded bg-amber-500/15 px-2 py-1 text-xs text-amber-200">
          {error}
        </div>
      )}

      <DeviceSelect
        label="Micro"
        devices={mics}
        onPick={(id) => void pick("audioinput", id)}
      />
      <MicLevel participant={room.localParticipant} />

      <DeviceSelect
        label="Camera"
        devices={cams}
        onPick={(id) => void pick("videoinput", id)}
      />

      {/*
        Danh sách loa chỉ có trên Chrome và Edge. Firefox và Safari không
        cho trang web chọn thiết bị phát — người dùng đổi ở cài đặt của hệ
        điều hành. Ẩn hẳn thay vì hiện một ô rỗng khó hiểu.
      */}
      {speakers.length > 0 && (
        <DeviceSelect
          label="Loa"
          devices={speakers}
          onPick={(id) => void pick("audiooutput", id)}
        />
      )}
    </div>
  );
}

function DeviceSelect({
  label,
  devices,
  onPick,
}: {
  label: string;
  devices: MediaDeviceInfo[];
  onPick: (deviceId: string) => void;
}) {
  return (
    <label className="mb-3 block">
      <span className="mb-1 block text-xs text-neutral-400">{label}</span>
      <select
        onChange={(e) => onPick(e.target.value)}
        className="w-full rounded border border-neutral-700 bg-neutral-800 px-2 py-1 text-sm"
      >
        {devices.length === 0 && <option>Không tìm thấy thiết bị</option>}
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
 * Thanh đo mức âm thanh của chính mình.
 *
 * Đọc audioLevel mà SDK đã tính sẵn thay vì tự dựng một AnalyserNode:
 * cùng con số mà SFU dùng để chọn người đang nói, nên thanh này nhảy đúng
 * lúc người khác thấy viền sáng quanh ô hình của mình.
 */
function MicLevel({ participant }: { participant: LocalParticipant }) {
  const [level, setLevel] = useState(0);

  useEffect(() => {
    const t = setInterval(() => setLevel(participant.audioLevel), 100);
    return () => clearInterval(t);
  }, [participant]);

  return (
    <div className="mb-3">
      <div className="mb-1 text-xs text-neutral-400">
        Nói thử một câu — thanh dưới phải nhảy
      </div>
      <div className="h-2 overflow-hidden rounded bg-neutral-800">
        <div
          className="h-full bg-emerald-500 transition-[width] duration-100"
          // Nhân 3 vì tiếng nói bình thường chỉ quanh 0.1–0.3. Để nguyên
          // thì thanh gần như không nhúc nhích và người dùng kết luận là
          // micro hỏng.
          style={{ width: `${Math.min(100, level * 300)}%` }}
        />
      </div>
    </div>
  );
}
