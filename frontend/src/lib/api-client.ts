import { isTokenExpiringSoon, useAuthStore } from "./auth/store";

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost/api/v1";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: unknown,
    public requestId?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

type Envelope<T> = {
  data?: T;
  meta?: unknown;
  error?: {
    code: string;
    message: string;
    details?: unknown;
    request_id?: string;
  };
};

export type PageMeta = {
  page: number;
  page_size: number;
  total_items: number;
  total_pages: number;
};

/* ------------------------------------------------------------------ *
 * Gộp các lần refresh đồng thời
 *
 * Khi access token hết hạn, thường có nhiều request cùng lỗi 401 một lúc
 * (một trang đang tải 5 widget). Nếu mỗi request tự gọi refresh thì sẽ có
 * 5 lần refresh song song — và vì backend XOAY VÒNG refresh token, các lần
 * sau sẽ dùng token đã tiêu.
 *
 * Backend có thời gian ân hạn 10 giây nên việc đó không còn gây đăng xuất,
 * nhưng vẫn là 5 request thừa và 5 phiên mới vô ích. Biến dưới đây bảo đảm
 * mọi request trong CÙNG MỘT TAB chờ chung một lời gọi refresh.
 *
 * Lưu ý: cách này KHÔNG chặn được nhiều tab — mỗi tab có bản sao module
 * riêng. Trường hợp nhiều tab do thời gian ân hạn ở backend lo.
 * ------------------------------------------------------------------ */
let refreshPromise: Promise<string | null> | null = null;

async function doRefresh(): Promise<string | null> {
  const auth = useAuthStore.getState();

  try {
    const res = await fetch(`${API_BASE}/auth/refresh`, {
      method: "POST",
      credentials: "include", // bắt buộc để trình duyệt gửi cookie httpOnly
      headers: { "Content-Type": "application/json" },
    });

    if (!res.ok) {
      // Xoá CẢ token lẫn user.
      //
      // Đây là chỗ trước đây sai: chỉ xoá token, còn `user` vẫn nằm trong
      // React state nên giao diện tưởng vẫn đang đăng nhập và hiện một trang
      // hỏng. Nay cả hai nằm chung một store nên không thể lệch nữa.
      auth.clear();
      return null;
    }

    const body = (await res.json()) as Envelope<{
      access_token: string;
      expires_at: string;
    }>;

    if (!body.data?.access_token) {
      auth.clear();
      return null;
    }

    auth.setToken(body.data.access_token, body.data.expires_at);
    return body.data.access_token;
  } catch {
    auth.clear();
    return null;
  }
}

export async function refreshAccessToken(): Promise<string | null> {
  // ??= chỉ gán khi đang là null — request thứ hai trở đi nhận lại đúng
  // promise mà request đầu tiên đã tạo.
  refreshPromise ??= doRefresh().finally(() => {
    refreshPromise = null;
  });
  return refreshPromise;
}

async function getValidToken(): Promise<string | null> {
  const token = useAuthStore.getState().accessToken;
  if (token && !isTokenExpiringSoon()) return token;
  return refreshAccessToken();
}

type RequestOptions = Omit<RequestInit, "body"> & {
  body?: unknown;
  /** Endpoint công khai: không gắn token, không tự refresh. */
  skipAuth?: boolean;
};

async function request<T>(
  path: string,
  opts: RequestOptions = {},
): Promise<{ data: T; meta?: PageMeta }> {
  const { body, skipAuth, headers, ...rest } = opts;

  const send = async (token: string | null): Promise<Response> => {
    const h = new Headers(headers);
    h.set("Content-Type", "application/json");
    if (token) h.set("Authorization", `Bearer ${token}`);

    return fetch(`${API_BASE}${path}`, {
      ...rest,
      headers: h,
      credentials: "include",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  };

  let token = skipAuth ? null : await getValidToken();
  let res = await send(token);

  // Token vẫn bị từ chối dù ta tưởng nó còn hạn (ví dụ phiên đã bị huỷ ở
  // thiết bị khác) — thử refresh MỘT lần rồi gửi lại.
  if (res.status === 401 && !skipAuth) {
    token = await refreshAccessToken();
    if (token) res = await send(token);
  }

  const raw = await res.text();
  const parsed: Envelope<T> = raw ? JSON.parse(raw) : {};

  if (!res.ok) {
    throw new ApiError(
      res.status,
      parsed.error?.code ?? "UNKNOWN",
      parsed.error?.message ?? "Đã có lỗi xảy ra",
      parsed.error?.details,
      parsed.error?.request_id,
    );
  }

  return { data: parsed.data as T, meta: parsed.meta as PageMeta | undefined };
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "GET" }),

  post: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "POST", body }),

  put: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "PUT", body }),

  // PATCH dùng cho sửa MỘT PHẦN bản ghi — ví dụ kéo-thả trên bảng Kanban chỉ
  // đổi cột và vị trí, không gửi lại toàn bộ công việc như PUT.
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "PATCH", body }),

  delete: <T>(path: string, opts?: RequestOptions) =>
    request<T>(path, { ...opts, method: "DELETE" }),
};
