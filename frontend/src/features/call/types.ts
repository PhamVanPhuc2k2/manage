export type CallKind = "audio" | "video";

export type CallStatus =
  | "ringing"
  | "active"
  | "ended"
  | "missed"
  | "rejected"
  | "cancelled"
  | "failed";

export type CallParticipant = {
  employee_id: string;
  employee_name?: string;
  avatar_key?: string;
  joined_at?: string;
  left_at?: string;
  in_room: boolean;
  had_audio: boolean;
  had_video: boolean;
  had_screen: boolean;
};

export type Call = {
  id: string;
  conversation_id: string;
  initiator_id?: string;
  initiator_name?: string;
  kind: CallKind;
  status: CallStatus;
  room_name: string;
  started_at: string;
  ended_at?: string;
  end_reason?: string;
  /** Máy chủ tính sẵn — xem chú thích ở callDTO phía backend. */
  duration_seconds: number;
  relay_ratio?: number;
  participants?: CallParticipant[];
};

/** Mọi thứ cần để vào phòng ngay, gói trong một lượt gọi. */
export type CallJoin = {
  call: Call;
  token: string;
  media_url: string;
  ringing?: string[];
  /** Người không được đổ chuông vì đang bận cuộc khác. */
  busy?: string[];
};

export type ICEServer = {
  urls: string[];
  username?: string;
  credential?: string;
};

/* ------------------------------------------------------------------ *
 * Payload của bản tin realtime
 *
 * Khớp với payload.go ở backend. Khai báo lại ở đây thay vì sinh tự động
 * vì chỉ có bốn hình dạng, và một tệp khai báo tay đọc được thì rõ hơn một
 * bộ sinh mã mà không ai chạy lại sau khi sửa backend.
 * ------------------------------------------------------------------ */

export type IncomingPayload = {
  call_id: string;
  conversation_id: string;
  kind: CallKind;
  initiator_id: string;
  initiator_name: string;
  /** Để client tự tắt chuông đúng lúc thay vì chờ máy chủ báo. */
  expires_at: string;
};

export type RingingPayload = {
  call_id: string;
  ringing: string[];
  busy?: string[];
};

/** Dùng chung cho accepted, rejected, cancelled và ended. */
export type StatusPayload = {
  call_id: string;
  status: CallStatus;
  by?: string;
  reason?: string;
  duration_seconds?: number;
};

export type ParticipantPayload = {
  call_id: string;
  employee_id: string;
  name?: string;
  joined: boolean;
};

export const CALL_EVENTS = {
  incoming: "call.incoming",
  ringing: "call.ringing",
  accepted: "call.accepted",
  rejected: "call.rejected",
  cancelled: "call.cancelled",
  ended: "call.ended",
  participant: "call.participant",
} as const;

/** Câu hiển thị cho người dùng khi cuộc gọi kết thúc. */
export function endReasonLabel(reason?: string): string {
  switch (reason) {
    case "timeout":
      return "Không có người bắt máy";
    case "busy":
      return "Người nhận đang bận";
    case "rejected":
      return "Cuộc gọi bị từ chối";
    case "network":
      return "Mất kết nối";
    case "empty":
      return "Không còn ai trong phòng";
    default:
      return "Cuộc gọi đã kết thúc";
  }
}
