"use client";

import { useEffect, useState } from "react";
import {
  ParticipantEvent,
  Room,
  RoomEvent,
  Track,
  createAudioAnalyser,
  type LocalAudioTrack,
  type LocalParticipant,
} from "livekit-client";

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

  // Thiết bị ĐANG dùng, theo từng loại. Select phải hiện đúng cái này chứ
  // không phải dòng đầu danh sách — không thì bảng nói "đang dùng micro A"
  // trong khi thực ra là B, và chọn lại A cũng không bắn onChange.
  const [active, setActive] = useState<
    Partial<Record<MediaDeviceKind, string>>
  >(() => readActive(room));
  useEffect(() => {
    const sync = () => setActive(readActive(room));
    room.on(RoomEvent.ActiveDeviceChanged, sync);
    return () => {
      room.off(RoomEvent.ActiveDeviceChanged, sync);
    };
  }, [room]);

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
      // SDK báo thất bại bằng giá trị trả về chứ không chỉ bằng exception.
      const ok = await room.switchActiveDevice(kind, deviceId);
      setActive(readActive(room));
      setError(ok ? null : "Không chuyển được sang thiết bị này");
    } catch {
      setActive(readActive(room));
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
        value={active.audioinput}
        onPick={(id) => void pick("audioinput", id)}
      />
      <MicLevel
        participant={room.localParticipant}
        deviceId={active.audioinput}
      />

      <DeviceSelect
        label="Camera"
        devices={cams}
        value={active.videoinput}
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
          value={active.audiooutput}
          onPick={(id) => void pick("audiooutput", id)}
        />
      )}
    </div>
  );
}

function DeviceSelect({
  label,
  devices,
  value,
  onPick,
}: {
  label: string;
  devices: MediaDeviceInfo[];
  value: string | undefined;
  onPick: (deviceId: string) => void;
}) {
  return (
    <label className="mb-3 block">
      <span className="mb-1 block text-xs text-neutral-400">{label}</span>
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
        className="w-full rounded border border-neutral-700 bg-neutral-800 px-2 py-1 text-sm"
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

function readActive(room: Room): Partial<Record<MediaDeviceKind, string>> {
  return {
    audioinput: room.getActiveDevice("audioinput"),
    videoinput: room.getActiveDevice("videoinput"),
    audiooutput: room.getActiveDevice("audiooutput"),
  };
}

/**
 * Thanh đo mức âm thanh của chính mình.
 *
 * Đo NGAY TRÊN MÁY bằng một AnalyserNode gắn vào track micro, không đọc
 * participant.audioLevel. Con số kia do SFU gửi về và chỉ khác 0 khi SFU coi
 * mình là "người đang nói" — phải vượt ngưỡng và nói đủ lâu. Nó là đèn báo
 * đang nói chứ không phải thanh đo: người nói nhỏ sẽ thấy thanh đứng im và
 * kết luận micro hỏng, đúng tình huống bảng này sinh ra để gỡ. Đo tại chỗ
 * thì thanh nhảy theo từng tiếng, kể cả tiếng nhỏ.
 */
function MicLevel({
  participant,
  deviceId,
}: {
  participant: LocalParticipant;
  // Đổi micro thì SDK thay mediaStreamTrack bên dưới — bộ đo gắn vào track
  // cũ sẽ đo một track đã dừng. Truyền vào đây để dựng lại bộ đo.
  deviceId: string | undefined;
}) {
  const [track, setTrack] = useState(() => micTrack(participant));
  const [muted, setMuted] = useState(() => micMuted(participant));
  const [level, setLevel] = useState(0);

  useEffect(() => {
    const sync = () => {
      setTrack(micTrack(participant));
      setMuted(micMuted(participant));
    };
    const events = [
      ParticipantEvent.LocalTrackPublished,
      ParticipantEvent.LocalTrackUnpublished,
      ParticipantEvent.TrackMuted,
      ParticipantEvent.TrackUnmuted,
    ] as const;
    events.forEach((e) => participant.on(e, sync));
    return () => {
      events.forEach((e) => participant.off(e, sync));
    };
  }, [participant]);

  useEffect(() => {
    if (!track || muted) return;
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
  }, [track, muted, deviceId]);

  // Mic tắt thì hiện 0 luôn, không giữ lại số đo cuối cùng trước khi tắt.
  const live = track && !muted;
  const shown = live ? level : 0;

  return (
    <div className="mb-3">
      <div className="mb-1 text-xs text-neutral-400">
        {!live
          ? "Mic đang tắt — bật mic để thử"
          : "Nói thử một câu — thanh dưới phải nhảy"}
      </div>
      <div
        role="meter"
        aria-label="Mức âm thanh micro"
        aria-valuemin={0}
        aria-valuemax={1}
        aria-valuenow={shown}
        className="h-2 overflow-hidden rounded bg-neutral-800"
      >
        <div
          className="h-full bg-emerald-500 transition-[width] duration-100"
          // Nhân 3 vì tiếng nói bình thường chỉ quanh 0.1–0.3. Để nguyên
          // thì thanh gần như không nhúc nhích và người dùng kết luận là
          // micro hỏng. Hệ số này mới đo với micro GIẢ của Chromium (bíp
          // liên tục, ~0.15–0.3) — cần chỉnh lại khi thử với micro thật.
          style={{ width: `${Math.min(100, shown * 300)}%` }}
        />
      </div>
    </div>
  );
}

function micTrack(p: LocalParticipant): LocalAudioTrack | undefined {
  return p.getTrackPublication(Track.Source.Microphone)?.audioTrack;
}

function micMuted(p: LocalParticipant): boolean {
  return p.getTrackPublication(Track.Source.Microphone)?.isMuted ?? true;
}
