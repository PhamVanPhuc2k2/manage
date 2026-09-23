"use client";

import { useEffect, useReducer, useState } from "react";
import {
  ConnectionQuality,
  Room,
  RoomEvent,
  Track,
  VideoPresets,
  type Participant,
  type TrackPublication,
} from "livekit-client";

import type { CallJoin, CallKind } from "./types";

export type RoomPhase = "idle" | "connecting" | "connected" | "failed";

/** Một ô hình trên lưới. */
export type Tile = {
  key: string;
  identity: string;
  name: string;
  isLocal: boolean;
  isScreen: boolean;
  publication?: TrackPublication;
  micOn: boolean;
  speaking: boolean;
  quality: ConnectionQuality;
};

/**
 * Mở phòng LiveKit và giữ nó sống theo vòng đời của cuộc gọi.
 *
 * Cách dựng lại giao diện ở đây là CỐ Ý đơn giản: mọi sự kiện của phòng chỉ
 * tăng một bộ đếm, và danh sách ô hình được tính lại từ trạng thái thật của
 * đối tượng Room mỗi lần render.
 *
 * Cách kia — giữ một bản sao danh sách người tham gia trong React state và
 * vá nó theo từng sự kiện — là nguồn lỗi kinh điển của mọi ứng dụng gọi
 * video: chỉ cần lỡ một sự kiện là bản sao lệch khỏi sự thật vĩnh viễn, và
 * người dùng thấy một ô đen của người đã rời phòng từ mười phút trước.
 */
export function useLiveKitRoom(join: CallJoin, kind: CallKind) {
  const [phase, setPhase] = useState<RoomPhase>("idle");
  const [error, setError] = useState<string | null>(null);
  const [version, bump] = useReducer((x: number) => x + 1, 0);

  // Room dựng bằng hàm khởi tạo lười, KHÔNG phải trong effect.
  //
  // Dựng trong effect thì phải setState để component thấy nó, và một
  // setState đồng bộ trong effect kéo theo một lượt render thừa ngay sau
  // lượt đầu. Hook này được gắn theo khoá là id cuộc gọi, nên "một lần cho
  // mỗi lần gắn" đúng bằng "một lần cho mỗi cuộc gọi".
  const [room] = useState(
    () =>
      new Room({
        // adaptiveStream giảm độ nét của ô hình nhỏ trên màn hình. dynacast
        // dừng hẳn việc gửi lớp độ nét cao mà không ai đang xem.
        adaptiveStream: true,
        dynacast: true,
        videoCaptureDefaults: { resolution: VideoPresets.h720.resolution },
        publishDefaults: {
          // Simulcast là ĐIỀU KIỆN để chạy được, không phải tối ưu hoá.
          //
          // Không có nó, mỗi người gửi một luồng độ nét cao duy nhất và SFU
          // phải chuyển nguyên luồng đó cho mọi người — kể cả người đang xem
          // ô hình nhỏ bằng con tem. Băng thông phình theo bình phương số
          // người trong phòng và mạng công ty nghẽn từ người thứ năm.
          simulcast: true,
        },
      }),
  );

  const callId = join.call.id;
  const mediaURL = join.media_url;
  const token = join.token;

  useEffect(() => {
    const r = room;

    // Mọi sự kiện đổi cái nhìn về phòng đều chỉ tăng bộ đếm.
    const events: RoomEvent[] = [
      RoomEvent.ParticipantConnected,
      RoomEvent.ParticipantDisconnected,
      RoomEvent.TrackSubscribed,
      RoomEvent.TrackUnsubscribed,
      RoomEvent.TrackPublished,
      RoomEvent.TrackUnpublished,
      RoomEvent.LocalTrackPublished,
      RoomEvent.LocalTrackUnpublished,
      RoomEvent.TrackMuted,
      RoomEvent.TrackUnmuted,
      RoomEvent.ActiveSpeakersChanged,
      RoomEvent.ConnectionStateChanged,
      RoomEvent.ConnectionQualityChanged,
    ];
    for (const e of events) r.on(e, bump);

    let cancelled = false;

    const connect = async () => {
      setPhase("connecting");
      setError(null);
      try {
        await r.connect(mediaURL, token);
        if (cancelled) return;

        // Bật mic trước, camera sau, và mỗi cái một try riêng.
        //
        // Gộp chung thì máy không có webcam sẽ hỏng luôn cả micro, và
        // người dùng mất tiếng ở một cuộc gọi lẽ ra chỉ thiếu hình.
        try {
          await r.localParticipant.setMicrophoneEnabled(true);
        } catch {
          setError("Không truy cập được micro. Kiểm tra quyền của trình duyệt.");
        }
        if (kind === "video") {
          try {
            await r.localParticipant.setCameraEnabled(true);
          } catch {
            setError("Không truy cập được camera. Cuộc gọi vẫn có tiếng.");
          }
        }

        if (!cancelled) setPhase("connected");
      } catch (err) {
        if (cancelled) return;
        setPhase("failed");
        setError(
          err instanceof Error
            ? err.message
            : "Không kết nối được tới máy chủ media",
        );
      }
    };

    void connect();

    return () => {
      cancelled = true;
      for (const e of events) r.off(e, bump);
      // disconnect() dừng cả camera và micro. Bỏ bước này thì đèn webcam
      // vẫn sáng sau khi cúp máy — và đó là thứ người dùng báo lên như
      // một sự cố bảo mật, rất đúng.
      void r.disconnect();
    };
    // kind chỉ đọc một lần lúc vào phòng; đổi nó giữa cuộc gọi không có
    // nghĩa gì, nên nó không nằm trong danh sách phụ thuộc.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [callId, room, mediaURL, token]);

  const tiles = collectTiles(room);

  return {
    room,
    phase,
    error,
    tiles,
    /** Dùng để buộc component con tính lại khi phòng đổi. */
    version,
    connectionState: room.state,
  };
}

/** Dựng danh sách ô hình từ trạng thái THẬT của phòng. */
function collectTiles(room: Room): Tile[] {
  const out: Tile[] = [];

  const add = (p: Participant, isLocal: boolean) => {
    const camera = p.getTrackPublication(Track.Source.Camera);
    const screen = p.getTrackPublication(Track.Source.ScreenShare);
    const mic = p.getTrackPublication(Track.Source.Microphone);
    const name = p.name || p.identity;

    out.push({
      key: `${p.identity}:cam`,
      identity: p.identity,
      name,
      isLocal,
      isScreen: false,
      publication: camera,
      micOn: !!mic && !mic.isMuted,
      speaking: p.isSpeaking,
      quality: p.connectionQuality,
    });

    // Màn hình chia sẻ là một ô RIÊNG, không thay ô camera: người trình
    // bày vẫn phải nhìn thấy mặt mình, và người xem vẫn cần thấy mặt người
    // đang nói trong lúc đọc slide.
    if (screen?.isSubscribed || (isLocal && screen)) {
      out.push({
        key: `${p.identity}:screen`,
        identity: p.identity,
        name: `${name} (màn hình)`,
        isLocal,
        isScreen: true,
        publication: screen,
        micOn: false,
        speaking: false,
        quality: p.connectionQuality,
      });
    }
  };

  add(room.localParticipant, true);
  for (const p of room.remoteParticipants.values()) add(p, false);

  // Màn hình chia sẻ lên trước: khi có người trình bày, đó là thứ mọi người
  // đang thật sự nhìn.
  return out.sort((a, b) => Number(b.isScreen) - Number(a.isScreen));
}
