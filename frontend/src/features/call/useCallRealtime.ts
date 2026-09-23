"use client";

import { useCallback, useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { Envelope } from "@/lib/ws/client";
import { useWsMessage } from "@/lib/ws/useWebSocket";

import { useCallStore } from "./store";
import {
  CALL_EVENTS,
  endReasonLabel,
  type IncomingPayload,
  type ParticipantPayload,
  type RingingPayload,
  type StatusPayload,
} from "./types";

/**
 * Nối bản tin call.* vào store.
 *
 * Gọi ĐÚNG MỘT LẦN ở CallHost. Đăng ký ở từng trang sẽ khiến cuộc gọi tới
 * lúc đang ở trang khác bị bỏ rơi — mà "đang ở trang khác" là trường hợp
 * thường gặp nhất, không phải ngoại lệ.
 */
export function useCallRealtime(): void {
  const qc = useQueryClient();
  const { ring, stopRinging, hangUp, answered, setBusy, setNotice } =
    useCallStore();

  const onIncoming = useCallback(
    (e: Envelope) => ring(e.payload as IncomingPayload),
    [ring],
  );

  const onRinging = useCallback(
    (e: Envelope) => {
      const p = e.payload as RingingPayload;
      setBusy(p.busy ?? []);
      if (p.busy?.length && !p.ringing.length) {
        // Không ai đổ chuông và tất cả đều bận: nói thẳng ra. Để im thì
        // người gọi ngồi nghe hồi chuông giả trong 45 giây rồi kết luận
        // là hệ thống hỏng.
        setNotice("Người nhận đang bận cuộc gọi khác");
      }
    },
    [setBusy, setNotice],
  );

  const onAccepted = useCallback(
    (e: Envelope) => {
      const p = e.payload as StatusPayload;
      // Cũng dùng để tắt chuông ở CÁC THIẾT BỊ KHÁC của chính người vừa
      // bắt máy — backend đẩy call.cancelled cho việc đó, nhưng bắt cả
      // accepted ở đây làm màn hình đúng ngay cả khi bản tin kia lạc.
      stopRinging(p.call_id);
      answered();
    },
    [stopRinging, answered],
  );

  // Ba sự kiện đóng — và chúng KHÔNG giống nhau.
  //
  // Gộp cả ba vào một nhánh "cuộc gọi đã đóng" là lỗi đã xảy ra thật
  // ở đây, và triệu chứng rất khó đoán: bấm "Nghe" thì màn hình cuộc gọi
  // hiện lên rồi biến mất ngay, không báo gì. Lý do: máy chủ đẩy
  // call.cancelled về CHÍNH người vừa bắt máy để các thiết bị khác của họ
  // ngừng đổ chuông — nó không phân biệt được thiết bị, chỉ biết nhân
  // viên, nên bản sao đó về luôn tab vừa bấm.

  // ended: cuộc gọi thật sự đã xong cho MỌI người.
  const onEnded = useCallback(
    (e: Envelope) => {
      const p = e.payload as StatusPayload;
      stopRinging(p.call_id);
      hangUp(p.call_id);
      if (p.status !== "ended" || p.reason !== "hangup") {
        setNotice(endReasonLabel(p.reason));
      }
      // Lịch sử hội thoại có thêm một dòng tin nhắn hệ thống. Đánh dấu
      // hỏng thay vì tự dựng lại: bản tin không mang đủ dữ liệu để vẽ.
      void qc.invalidateQueries({ queryKey: ["calls"] });
    },
    [stopRinging, hangUp, setNotice, qc],
  );

  // cancelled: lời mời không còn hiệu lực. Chỉ tắt CHUÔNG.
  //
  // Ngoại lệ duy nhất: người GỌI đang chờ đầu kia bắt máy thì đây là
  // tin báo hết giờ, và họ phải thoát khỏi màn hình "đang gọi...".
  const onCancelled = useCallback(
    (e: Envelope) => {
      const p = e.payload as StatusPayload;
      stopRinging(p.call_id);

      if (useCallStore.getState().outgoing) {
        hangUp(p.call_id);
        setNotice(endReasonLabel(p.reason));
      }
      void qc.invalidateQueries({ queryKey: ["calls"] });
    },
    [stopRinging, hangUp, setNotice, qc],
  );

  // rejected: MỘT người từ chối, không phải cả cuộc gọi kết thúc.
  //
  // Trong cuộc gọi nhóm, một người bấm từ chối không được làm những
  // người đang nói rớt khỏi phòng. Khi gọi 1-1 thì máy chủ gửi tiếp
  // call.ended ngay sau đó, và chính bản tin đó mới đóng màn hình.
  const onRejected = useCallback(
    (e: Envelope) => {
      const p = e.payload as StatusPayload;
      stopRinging(p.call_id);
      setNotice(
        p.reason === "busy" ? "Người nhận đang bận" : "Cuộc gọi bị từ chối",
      );
    },
    [stopRinging, setNotice],
  );

  const onParticipant = useCallback(
    (e: Envelope) => {
      const p = e.payload as ParticipantPayload;
      if (!p.name) return;
      setNotice(`${p.name} ${p.joined ? "đã vào" : "đã rời"} cuộc gọi`);
    },
    [setNotice],
  );

  useWsMessage(CALL_EVENTS.incoming, onIncoming);
  useWsMessage(CALL_EVENTS.ringing, onRinging);
  useWsMessage(CALL_EVENTS.accepted, onAccepted);
  useWsMessage(CALL_EVENTS.rejected, onRejected);
  useWsMessage(CALL_EVENTS.cancelled, onCancelled);
  useWsMessage(CALL_EVENTS.ended, onEnded);
  useWsMessage(CALL_EVENTS.participant, onParticipant);

  // Câu báo tự tắt sau vài giây. Không tắt thì nó nằm mãi trên màn hình và
  // người dùng tưởng cuộc gọi vẫn đang ở trạng thái đó.
  const notice = useCallStore((s) => s.notice);
  useEffect(() => {
    if (!notice) return;
    const t = setTimeout(() => setNotice(null), 5000);
    return () => clearTimeout(t);
  }, [notice, setNotice]);
}
