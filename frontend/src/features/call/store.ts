import { create } from "zustand";

import type { CallJoin, IncomingPayload } from "./types";

/**
 * Trạng thái cuộc gọi của cả ứng dụng.
 *
 * Vì sao là store toàn cục chứ không phải state trong trang chat?
 *
 * Cuộc gọi phải sống SÓT qua việc điều hướng. Người ta vừa họp vừa mở bảng
 * công việc, vừa nói vừa tra phiếu lương — nếu trạng thái nằm trong trang
 * chat thì rời trang là rớt cuộc gọi. Store này nằm ngoài cây route, và
 * CallHost được gắn ở AppShell nên tồn tại ở MỌI trang cần đăng nhập.
 */
type CallState = {
  /** Cuộc gọi đến đang đổ chuông. null khi không có. */
  incoming: IncomingPayload | null;

  /** Cuộc gọi mình đang tham gia (hoặc đang chờ đầu kia bắt máy). */
  active: CallJoin | null;

  /**
   * Cuộc gọi đã bấm nhưng chưa ai bắt máy.
   *
   * Tách khỏi `active` vì giao diện khác hẳn: một bên là màn hình "đang
   * gọi..." với đúng một nút cúp máy, bên kia là phòng họp đầy đủ.
   */
  outgoing: boolean;

  /** Người không đổ chuông được vì đang bận, để nói rõ thay vì để người gọi đoán. */
  busy: string[];

  /** Câu báo cuối cùng, hiện thành một dải nhỏ rồi tự tắt. */
  notice: string | null;

  ring: (p: IncomingPayload) => void;
  stopRinging: (callId?: string) => void;

  join: (j: CallJoin, outgoing: boolean) => void;
  answered: () => void;
  hangUp: (callId?: string) => void;

  setBusy: (ids: string[]) => void;
  setNotice: (text: string | null) => void;
};

export const useCallStore = create<CallState>((set, get) => ({
  incoming: null,
  active: null,
  outgoing: false,
  busy: [],
  notice: null,

  ring: (p) => {
    // Đang trong cuộc gọi khác thì KHÔNG đổ chuông đè lên.
    //
    // Backend đã từ chối thay người đang bận, nên trường hợp này chỉ xảy
    // ra khi hai bản tin chạy đua nhau. Vẫn phải chặn ở đây: một cái
    // chuông kêu giữa cuộc họp là thứ người dùng nhớ rất lâu.
    if (get().active) return;
    set({ incoming: p });
  },

  stopRinging: (callId) => {
    const cur = get().incoming;
    if (!cur) return;
    // Chỉ tắt đúng cuộc gọi được nhắc tới. Không kiểm thì một bản tin
    // "kết thúc" của cuộc cũ sẽ tắt chuông của cuộc vừa tới.
    if (callId && cur.call_id !== callId) return;
    set({ incoming: null });
  },

  join: (j, outgoing) =>
    set({
      active: j,
      outgoing,
      incoming: null,
      busy: j.busy ?? [],
      notice: null,
    }),

  answered: () => set({ outgoing: false }),

  hangUp: (callId) => {
    const cur = get().active;
    if (callId && cur && cur.call.id !== callId) return;
    set({ active: null, outgoing: false, busy: [] });
  },

  setBusy: (ids) => set({ busy: ids }),
  setNotice: (notice) => set({ notice }),
}));
