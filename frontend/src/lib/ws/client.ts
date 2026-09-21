import { useAuthStore } from "../auth/store";
import { refreshAccessToken } from "../api-client";

/** Hình dạng DUY NHẤT của mọi bản tin, khớp với Envelope ở backend. */
export type Envelope<T = unknown> = {
  type: string;
  payload?: T;
  ts: string;
  trace_id?: string;
};

export type WsStatus = "connecting" | "open" | "closed";

type Handler = (e: Envelope) => void;
type StatusHandler = (s: WsStatus) => void;

const WS_BASE =
  process.env.NEXT_PUBLIC_WS_URL ??
  (typeof window !== "undefined"
    ? `${window.location.protocol === "https:" ? "wss:" : "ws:"}//${window.location.host}/ws`
    : "");

/** Chu kỳ gửi heartbeat. Server trả về con số này trong bản tin `welcome`. */
const DEFAULT_HEARTBEAT_MS = 30_000;

/** Không có chuột/bàn phím quá ngưỡng này thì coi là không hoạt động. */
const IDLE_AFTER_MS = 5 * 60_000;

/** Giới hạn trên của backoff. Quá số này thì nối lại chậm tới mức vô dụng. */
const MAX_BACKOFF_MS = 30_000;

/**
 * Client WebSocket dùng chung cho cả ứng dụng.
 *
 * Là một singleton chứ không phải một hook: mỗi tab chỉ nên có ĐÚNG MỘT kết
 * nối. Mỗi component tự mở kết nối riêng thì một trang có 5 widget sẽ mở 5
 * kết nối, và server đếm nhầm số người online lên 5 lần.
 */
class WsClient {
  private ws: WebSocket | null = null;
  private status: WsStatus = "closed";

  private handlers = new Set<Handler>();
  private statusHandlers = new Set<StatusHandler>();

  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatMs = DEFAULT_HEARTBEAT_MS;

  private attempt = 0;
  private lastActivityAt = Date.now();
  private closedByUs = false;
  private listenersBound = false;

  /* ---------------------------------------------------------------- *
   * Vòng đời
   * ---------------------------------------------------------------- */

  async connect(): Promise<void> {
    if (typeof window === "undefined") return;
    if (this.ws && (this.ws.readyState === WebSocket.OPEN ||
                    this.ws.readyState === WebSocket.CONNECTING)) {
      return;
    }

    // Token phải còn hạn TRƯỚC khi mở kết nối.
    //
    // Khác với REST, WebSocket không thể "thử lại với token mới" giữa chừng:
    // token chỉ được kiểm đúng một lần lúc bắt tay. Mở bằng token hết hạn sẽ
    // nhận 401 và rơi vào vòng nối lại vô ích.
    const token =
      useAuthStore.getState().accessToken ?? (await refreshAccessToken());
    if (!token) {
      this.setStatus("closed");
      return;
    }

    this.closedByUs = false;
    this.setStatus("connecting");

    // Token đi trên QUERY STRING vì trình duyệt không cho đặt header
    // Authorization khi mở WebSocket. Không dùng cookie: cookie tự bay theo
    // và mở đường cho Cross-Site WebSocket Hijacking.
    const ws = new WebSocket(`${WS_BASE}?token=${encodeURIComponent(token)}`);
    this.ws = ws;

    ws.onopen = () => {
      this.attempt = 0;
      this.setStatus("open");
      this.bindActivityListeners();
      this.startHeartbeat();
    };

    ws.onmessage = (ev) => {
      let e: Envelope;
      try {
        e = JSON.parse(ev.data as string) as Envelope;
      } catch {
        return;
      }

      // Server báo chu kỳ heartbeat mong muốn trong bản tin chào. Nghe theo
      // nó thay vì cứng hoá ở client: đổi chu kỳ ở server là đủ, không phải
      // đợi mọi người tải lại trang.
      if (e.type === "welcome") {
        const p = e.payload as { heartbeat_interval?: number } | undefined;
        if (p?.heartbeat_interval) {
          this.heartbeatMs = p.heartbeat_interval * 1000;
          this.startHeartbeat();
        }
      }

      this.handlers.forEach((h) => h(e));
    };

    ws.onclose = () => {
      this.stopHeartbeat();
      this.setStatus("closed");
      if (!this.closedByUs) this.scheduleReconnect();
    };

    // onerror luôn kéo theo onclose, nên không xử lý riêng — làm vậy sẽ nối
    // lại hai lần cho cùng một sự cố.
    ws.onerror = () => {};
  }

  disconnect(): void {
    this.closedByUs = true;
    this.stopHeartbeat();
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.ws?.close();
    this.ws = null;
    this.setStatus("closed");
  }

  /**
   * Nối lại với backoff tăng dần kèm nhiễu ngẫu nhiên.
   *
   * Nhiễu là phần quan trọng: không có nó, khi server restart thì TOÀN BỘ
   * client nối lại cùng một thời điểm và đánh sập server vừa mới hồi phục.
   */
  private scheduleReconnect(): void {
    if (this.reconnectTimer) return;

    const base = Math.min(1000 * 2 ** this.attempt, MAX_BACKOFF_MS);
    const delay = base * (0.5 + Math.random() * 0.5);
    this.attempt++;

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      void this.connect();
    }, delay);
  }

  /* ---------------------------------------------------------------- *
   * Heartbeat
   * ---------------------------------------------------------------- */

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.sendHeartbeat();
    this.heartbeatTimer = setInterval(() => this.sendHeartbeat(), this.heartbeatMs);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  /**
   * Gửi nhịp tim kèm cờ đang-hoạt-động.
   *
   * Cờ này là thứ phân biệt "mở tab" với "đang làm việc", và nó quyết định
   * con số trên bảng chấm công. Tab bị ẩn hoặc không có thao tác quá 5 phút
   * đều tính là không hoạt động — máy để đó qua đêm vẫn online, nhưng không
   * ai làm việc.
   */
  private sendHeartbeat(): void {
    if (this.ws?.readyState !== WebSocket.OPEN) return;

    const hidden =
      typeof document !== "undefined" && document.visibilityState === "hidden";
    const idle = Date.now() - this.lastActivityAt > IDLE_AFTER_MS;

    this.send("heartbeat", { is_active: !hidden && !idle });
  }

  private bindActivityListeners(): void {
    if (this.listenersBound || typeof window === "undefined") return;
    this.listenersBound = true;

    const touch = () => {
      this.lastActivityAt = Date.now();
    };

    // passive: true — các sự kiện này không bao giờ bị preventDefault, và
    // báo trước cho trình duyệt giúp nó không phải chờ trước mỗi lần cuộn.
    window.addEventListener("mousemove", touch, { passive: true });
    window.addEventListener("keydown", touch, { passive: true });
    window.addEventListener("scroll", touch, { passive: true });
    window.addEventListener("click", touch, { passive: true });

    document.addEventListener("visibilitychange", () => {
      if (document.visibilityState === "visible") {
        touch();
        // Quay lại tab: gửi nhịp tim NGAY thay vì đợi hết chu kỳ. Máy vừa
        // ngủ dậy có thể đã mất presence, và chờ thêm 30 giây là mất từng
        // đó thời gian làm việc.
        this.sendHeartbeat();
        if (this.ws?.readyState !== WebSocket.OPEN) void this.connect();
      } else {
        // Rời tab: báo ngay là không hoạt động, không đợi hết chu kỳ.
        this.sendHeartbeat();
      }
    });

    window.addEventListener("online", () => void this.connect());
  }

  /* ---------------------------------------------------------------- *
   * Gửi / nhận
   * ---------------------------------------------------------------- */

  send(type: string, payload?: unknown): void {
    if (this.ws?.readyState !== WebSocket.OPEN) return;
    this.ws.send(JSON.stringify({ type, payload, ts: new Date().toISOString() }));
  }

  /** Đăng ký nhận bản tin. Trả về hàm huỷ đăng ký. */
  subscribe(h: Handler): () => void {
    this.handlers.add(h);
    return () => this.handlers.delete(h);
  }

  onStatus(h: StatusHandler): () => void {
    this.statusHandlers.add(h);
    h(this.status); // báo trạng thái hiện tại ngay, không bắt chờ lần đổi sau
    return () => this.statusHandlers.delete(h);
  }

  getStatus(): WsStatus {
    return this.status;
  }

  private setStatus(s: WsStatus): void {
    if (this.status === s) return;
    this.status = s;
    this.statusHandlers.forEach((h) => h(s));
  }
}

export const wsClient = new WsClient();
