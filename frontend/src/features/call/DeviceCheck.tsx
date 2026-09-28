"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  Room,
  createLocalAudioTrack,
  createLocalVideoTrack,
  type LocalAudioTrack,
  type LocalVideoTrack,
} from "livekit-client";

import {
  loadDevicePrefs,
  resolveDevice,
  saveDevicePref,
  type DeviceKind,
} from "./devicePrefs";
import { DeviceSelect, LevelBar, useMicLevel } from "./deviceUi";

/**
 * Kiểm tra thiết bị TRƯỚC khi gọi: xem hình mình, thấy micro thu tiếng,
 * nghe thử loa — và chọn thiết bị cho các cuộc gọi sau.
 *
 * Là một trang mở được bất cứ lúc nào chứ không phải một bước chặn trước
 * mỗi cuộc gọi: người nhận cuộc gọi đang có chuông đếm ngược, và bắt ai
 * cũng qua màn hình kiểm tra chỉ làm chậm mọi cuộc gọi bình thường. Người
 * muốn chắc chắn trước một cuộc họp quan trọng thì mở trang này.
 */
export function DeviceCheck() {
  const [devices, setDevices] = useState<Record<DeviceKind, MediaDeviceInfo[]>>(
    {
      audioinput: [],
      videoinput: [],
      audiooutput: [],
    },
  );
  // Mã thiết bị ĐANG chọn, theo từng loại — đã quy đổi từ lựa chọn lưu
  // trước đó (xem resolveDevice) sang mã của lần tải trang này.
  const [selected, setSelected] = useState<Partial<Record<DeviceKind, string>>>(
    {},
  );
  const [video, setVideo] = useState<LocalVideoTrack>();
  const [audio, setAudio] = useState<LocalAudioTrack>();
  const [camError, setCamError] = useState<string | null>(null);
  const [micError, setMicError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const videoEl = useRef<HTMLVideoElement>(null);

  const readDevices = useCallback(async () => {
    const list = {
      audioinput: await Room.getLocalDevices("audioinput", false),
      videoinput: await Room.getLocalDevices("videoinput", false),
      audiooutput: await Room.getLocalDevices("audiooutput", false),
    };
    setDevices(list);
    return list;
  }, []);

  useEffect(() => {
    let cancelled = false;
    // Track mở trong lần gắn này, để dọn khi rời trang: không dừng thì đèn
    // camera vẫn sáng sau khi người dùng đã sang trang khác.
    const held: { v?: LocalVideoTrack; a?: LocalAudioTrack } = {};

    const start = async () => {
      // Mở bằng thiết bị MẶC ĐỊNH trước: chưa có quyền thì trình duyệt không
      // cho biết tên thiết bị, mà tên là thứ để tìm lại thiết bị đã lưu.
      // Micro và camera mở RIÊNG: máy không có webcam vẫn thử được micro.
      try {
        const a = await createLocalAudioTrack();
        if (cancelled) return a.stop();
        held.a = a;
        setAudio(a);
      } catch {
        setMicError("Không mở được micro. Kiểm tra quyền của trình duyệt.");
      }
      try {
        const v = await createLocalVideoTrack();
        if (cancelled) return v.stop();
        held.v = v;
        setVideo(v);
      } catch {
        setCamError("Không mở được camera. Gọi thoại vẫn dùng được.");
      }

      let list: Awaited<ReturnType<typeof readDevices>>;
      try {
        list = await readDevices();
      } catch {
        return; // không đọc được danh sách thì vẫn thử bằng thiết bị mặc định
      }
      if (cancelled) return;

      // Chuyển sang thiết bị đã chọn lần trước, nếu nó vẫn còn.
      const prefs = loadDevicePrefs();
      const pickNow: Partial<Record<DeviceKind, string>> = {
        audioinput: held.a?.mediaStreamTrack.getSettings().deviceId,
        videoinput: held.v?.mediaStreamTrack.getSettings().deviceId,
      };
      for (const kind of ["audioinput", "videoinput", "audiooutput"] as const) {
        const id = resolveDevice(prefs[kind], list[kind]);
        if (!id) continue;
        try {
          if (kind === "audioinput")
            await held.a?.restartTrack({ deviceId: id });
          if (kind === "videoinput")
            await held.v?.restartTrack({ deviceId: id });
          pickNow[kind] = id;
        } catch {
          // thiết bị còn trong danh sách nhưng không mở được — giữ mặc định
        }
      }
      if (!cancelled) setSelected(pickNow);
    };
    void start();

    const onChange = () => void readDevices().catch(() => undefined);
    navigator.mediaDevices?.addEventListener("devicechange", onChange);
    return () => {
      cancelled = true;
      navigator.mediaDevices?.removeEventListener("devicechange", onChange);
      held.a?.stop();
      held.v?.stop();
    };
  }, [readDevices]);

  useEffect(() => {
    const el = videoEl.current;
    if (!video || !el) return;
    video.attach(el);
    return () => {
      video.detach(el);
    };
  }, [video]);

  const pick = async (kind: DeviceKind, deviceId: string) => {
    const device = devices[kind].find((d) => d.deviceId === deviceId);
    if (!device) return;
    try {
      if (kind === "audioinput" && audio)
        await audio.restartTrack({ deviceId });
      if (kind === "videoinput" && video)
        await video.restartTrack({ deviceId });
      saveDevicePref(kind, device);
      setSelected((p) => ({ ...p, [kind]: deviceId }));
      setSaved(true);
    } catch {
      if (kind === "audioinput")
        setMicError("Không chuyển được sang micro này");
      else setCamError("Không chuyển được sang camera này");
    }
  };

  const level = useMicLevel(audio, Boolean(audio), selected.audioinput);

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <div>
        <div className="aspect-video overflow-hidden rounded-lg bg-neutral-900">
          {video ? (
            <video
              ref={videoEl}
              aria-label="Hình từ camera"
              muted
              playsInline
              // Lật ngang như soi gương: người ta quen thấy mình như vậy, và
              // hình không lật khiến động tác chỉnh tóc hay nghiêng đầu ngược.
              className="h-full w-full -scale-x-100 object-cover"
            />
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-neutral-400">
              {camError ?? "Đang mở camera..."}
            </div>
          )}
        </div>
        <p className="mt-2 text-xs text-neutral-500">
          Người khác thấy hình không lật; ở đây lật như soi gương cho dễ chỉnh.
        </p>
      </div>

      <div className="text-sm">
        {micError && (
          <p className="mb-3 rounded bg-amber-500/15 px-2 py-1 text-xs text-amber-700 dark:text-amber-200">
            {micError}
          </p>
        )}
        <DeviceSelect
          tone="page"
          label="Micro"
          devices={devices.audioinput}
          value={selected.audioinput}
          onPick={(id) => void pick("audioinput", id)}
        />
        <LevelBar
          tone="page"
          level={level}
          hint={
            audio
              ? "Nói thử một câu — thanh dưới phải nhảy"
              : "Chưa mở được micro"
          }
        />

        <DeviceSelect
          tone="page"
          label="Camera"
          devices={devices.videoinput}
          value={selected.videoinput}
          onPick={(id) => void pick("videoinput", id)}
        />

        {/* Chỉ Chrome và Edge cho trang web chọn loa. */}
        {devices.audiooutput.length > 0 && (
          <DeviceSelect
            tone="page"
            label="Loa"
            devices={devices.audiooutput}
            value={selected.audiooutput}
            onPick={(id) => void pick("audiooutput", id)}
          />
        )}
        <SpeakerTest deviceId={selected.audiooutput} />

        {saved && (
          <p
            role="status"
            className="mt-3 text-xs text-emerald-700 dark:text-emerald-400"
          >
            Đã lưu — các cuộc gọi sau sẽ dùng thiết bị này.
          </p>
        )}
      </div>
    </div>
  );
}

/**
 * Phát một tiếng "ting" ngắn qua loa đã chọn.
 *
 * Tự tạo tiếng bằng Web Audio thay vì một tệp âm thanh: không phải tải gì,
 * và AudioContext.setSinkId gửi được tiếng tới đúng loa đã chọn.
 */
function SpeakerTest({ deviceId }: { deviceId?: string }) {
  const [playing, setPlaying] = useState(false);

  const play = async () => {
    setPlaying(true);
    const ctx = new AudioContext();
    try {
      const withSink = ctx as AudioContext & {
        setSinkId?: (id: string) => Promise<void>;
      };
      if (deviceId && withSink.setSinkId) await withSink.setSinkId(deviceId);
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.frequency.value = 880;
      gain.gain.setValueAtTime(0.0001, ctx.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.3, ctx.currentTime + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, ctx.currentTime + 0.8);
      osc.connect(gain).connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.8);
      await new Promise((r) => setTimeout(r, 900));
    } finally {
      void ctx.close();
      setPlaying(false);
    }
  };

  return (
    <button
      type="button"
      onClick={() => void play()}
      disabled={playing}
      className="rounded border border-neutral-300 px-3 py-1.5 text-sm disabled:opacity-50 dark:border-neutral-700"
    >
      {playing ? "Đang phát..." : "Phát thử tiếng loa"}
    </button>
  );
}
