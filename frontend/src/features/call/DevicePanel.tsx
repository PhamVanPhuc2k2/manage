"use client";

import { useEffect, useState } from "react";
import {
  ParticipantEvent,
  Room,
  RoomEvent,
  Track,
  type LocalAudioTrack,
  type LocalParticipant,
} from "livekit-client";

import { saveDevicePref, type DeviceKind } from "./devicePrefs";
import { DeviceSelect, LevelBar, useMicLevel } from "./deviceUi";

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

  const pick = async (kind: DeviceKind, deviceId: string) => {
    try {
      // SDK báo thất bại bằng giá trị trả về chứ không chỉ bằng exception.
      const ok = await room.switchActiveDevice(kind, deviceId);
      setActive(readActive(room));
      setError(ok ? null : "Không chuyển được sang thiết bị này");
      // Nhớ cho cuộc gọi sau — chọn tai nghe một lần là đủ.
      if (ok) {
        const list =
          kind === "audioinput"
            ? mics
            : kind === "videoinput"
              ? cams
              : speakers;
        const device = list.find((d) => d.deviceId === deviceId);
        if (device) saveDevicePref(kind, device);
      }
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

function readActive(room: Room): Partial<Record<MediaDeviceKind, string>> {
  return {
    audioinput: room.getActiveDevice("audioinput"),
    videoinput: room.getActiveDevice("videoinput"),
    audiooutput: room.getActiveDevice("audiooutput"),
  };
}

/** Thanh đo mức micro của chính mình trong cuộc gọi. */
function MicLevel({
  participant,
  deviceId,
}: {
  participant: LocalParticipant;
  deviceId: string | undefined;
}) {
  const [track, setTrack] = useState(() => micTrack(participant));
  const [muted, setMuted] = useState(() => micMuted(participant));

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

  const live = Boolean(track) && !muted;
  const level = useMicLevel(track, live, deviceId);

  return (
    <LevelBar
      level={level}
      hint={
        live
          ? "Nói thử một câu — thanh dưới phải nhảy"
          : "Mic đang tắt — bật mic để thử"
      }
    />
  );
}

function micTrack(p: LocalParticipant): LocalAudioTrack | undefined {
  return p.getTrackPublication(Track.Source.Microphone)?.audioTrack;
}

function micMuted(p: LocalParticipant): boolean {
  return p.getTrackPublication(Track.Source.Microphone)?.isMuted ?? true;
}
