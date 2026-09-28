"use client";

import { useEffect, useRef } from "react";
import {
  RoomEvent,
  Track,
  type Participant,
  type RemoteParticipant,
  type RemoteTrackPublication,
  type Room,
} from "livekit-client";

/**
 * Mỗi lúc chỉ MỘT người chiếu màn hình — người chiếu sau thay người trước.
 *
 * Vì sao "thay" chứ không phải "chặn": người muốn chiếu thường đang được
 * mời chiếu ("anh chiếu bảng số liệu lên đi"), và bắt họ chờ người kia tự
 * dừng là thêm một vòng nói qua nói lại. Người bấm thấy trước lời cảnh báo
 * rằng mình sẽ thay ai; người bị thay nhận thông báo nói rõ ai đã thay.
 *
 * Luật được giữ ở TỪNG MÁY chứ không ở máy chủ, theo hai đường:
 *
 *  1. Người bấm "Chiếu thay" gửi một bản tin "tiếp quản" qua kênh dữ liệu
 *     của phòng. Người đang chiếu nhận được thì dừng NGAY, không xét gì
 *     thêm — đây là ý định rõ ràng của người vừa bấm.
 *  2. Hai người cùng bấm chiếu khi chưa ai chiếu thì không ai gửi bản tin
 *     tiếp quản. Mỗi máy thấy luồng của máy kia và phân xử theo luật ở
 *     remoteWins — hai máy tính ra cùng một kết quả nên đúng một người dừng.
 *
 * Chỉ dựa vào đường 2 là không đủ: người bấm "Chiếu thay" ngay sau khi ai
 * đó vừa bắt đầu chiếu sẽ rơi vào cửa sổ "bấm cùng lúc", và cả hai cùng
 * chiếu. Phép thử Playwright đã bắt được đúng chuyện này.
 *
 * Đủ cho một hệ thống nội bộ — mục đích là tránh hai màn hình tranh nhau,
 * không phải chống một người cố tình sửa mã trình duyệt.
 */

const TOPIC = "presenter";
const TAKEOVER = "screen-takeover";

/**
 * Hai người bấm chiếu cách nhau dưới khoảng này thì coi như bấm cùng lúc.
 * Lúc đó mỗi máy đều thấy luồng của máy kia là "mới hơn" — không có luật
 * phân xử thì CẢ HAI cùng tự dừng và không ai chiếu được.
 */
const RACE_MS = 2000;

/**
 * Báo cả phòng rằng mình vừa chiếu THAY người đang chiếu. Gọi sau khi
 * luồng màn hình của mình đã lên.
 */
export async function announceTakeover(room: Room | null): Promise<void> {
  if (!room?.localParticipant.isScreenShareEnabled) return;
  await room.localParticipant.publishData(
    // Bọc lại: encode() trả Uint8Array<ArrayBufferLike>, publishData đòi ArrayBuffer.
    new Uint8Array(new TextEncoder().encode(TAKEOVER)),
    { reliable: true, topic: TOPIC },
  );
}

/** Người khác đang chiếu màn hình, nếu có. */
export function currentPresenter(
  room: Room | null,
): RemoteParticipant | undefined {
  if (!room) return undefined;
  for (const p of room.remoteParticipants.values()) {
    if (p.getTrackPublication(Track.Source.ScreenShare)) return p;
  }
  return undefined;
}

export function useSinglePresenter(
  room: Room | null,
  onReplaced: (byName: string) => void,
) {
  const startedAt = useRef(0);
  const notify = useRef(onReplaced);
  useEffect(() => {
    notify.current = onReplaced;
  }, [onReplaced]);

  useEffect(() => {
    if (!room) return;

    const onLocal = (pub: { source: Track.Source }) => {
      if (pub.source === Track.Source.ScreenShare)
        startedAt.current = Date.now();
    };

    // Dừng luồng của mình và báo ai đã thay. Kiểm lại trạng thái trước:
    // cả hai đường có thể cùng tới cho một lần tiếp quản, và chỉ báo MỘT lần.
    const yieldTo = (from: RemoteParticipant) => {
      const local = room.localParticipant;
      if (!local.isScreenShareEnabled) return;
      void local.setScreenShareEnabled(false).then(() => {
        notify.current(from.name || from.identity);
      });
    };

    const onRemote = (pub: RemoteTrackPublication, from: RemoteParticipant) => {
      if (pub.source !== Track.Source.ScreenShare) return;
      if (!room.localParticipant.isScreenShareEnabled) return;
      if (
        remoteWins(from, room.localParticipant, Date.now() - startedAt.current)
      ) {
        yieldTo(from);
      }
    };

    const onData = (
      payload: Uint8Array,
      from?: RemoteParticipant,
      _kind?: unknown,
      topic?: string,
    ) => {
      if (topic !== TOPIC || !from) return;
      if (new TextDecoder().decode(payload) === TAKEOVER) yieldTo(from);
    };

    room.on(RoomEvent.LocalTrackPublished, onLocal);
    room.on(RoomEvent.TrackPublished, onRemote);
    room.on(RoomEvent.DataReceived, onData);
    return () => {
      room.off(RoomEvent.LocalTrackPublished, onLocal);
      room.off(RoomEvent.TrackPublished, onRemote);
      room.off(RoomEvent.DataReceived, onData);
    };
  }, [room]);
}

/**
 * Luồng của người kia có thắng luồng của mình không.
 *
 * Bình thường người đến sau thắng. Hai người bấm gần như cùng lúc thì
 * phân xử theo identity — cả hai máy tính ra CÙNG một kết quả, nên đúng
 * một người dừng.
 */
export function remoteWins(
  remote: Pick<Participant, "identity">,
  local: Pick<Participant, "identity">,
  localSharingForMs: number,
): boolean {
  if (localSharingForMs > RACE_MS) return true;
  return remote.identity < local.identity;
}
