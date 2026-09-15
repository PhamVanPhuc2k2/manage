import { create } from "zustand";

export type CurrentUser = {
  id: string;
  employee_id: string;
  email: string;
  full_name: string;
  roles: string[];
  permissions: string[];
  scope: string;
};

type AuthState = {
  /**
   * Access token.
   *
   * CỐ Ý để trong bộ nhớ, KHÔNG dùng localStorage. localStorage đọc được
   * bằng JavaScript nên chỉ cần một lỗ hổng XSS là kẻ tấn công lấy được
   * token. Mất khi tải lại trang — nhưng đó chính là lúc refresh token
   * trong cookie httpOnly phát huy tác dụng.
   */
  accessToken: string | null;
  expiresAt: number | null;

  user: CurrentUser | null;

  /** Đang kiểm tra phiên lúc khởi động — chưa biết đã đăng nhập hay chưa. */
  loading: boolean;

  setToken: (token: string, expires: string | Date) => void;
  setUser: (user: CurrentUser | null) => void;
  setLoading: (loading: boolean) => void;
  /** Xoá SẠCH phiên: cả token lẫn thông tin người dùng. */
  clear: () => void;
};

/**
 * Store phiên đăng nhập.
 *
 * Vì sao là Zustand chứ không phải React Context?
 *
 * Trước đây token nằm trong một biến module (`tokenStore`) còn `user` nằm
 * trong React state. Lý do của sự chia đôi đó là api-client cần đọc token
 * nhưng nó không phải component React nên không dùng được Context.
 *
 * Chia đôi thì hai nửa lệch nhau được, và đã lệch thật: api-client xoá token
 * khi refresh thất bại nhưng KHÔNG xoá được `user`. Phiên bị huỷ từ thiết bị
 * khác thì giao diện vẫn tưởng đang đăng nhập, người dùng nhìn thấy một trang
 * hỏng với mọi bảng báo lỗi thay vì được đưa về trang đăng nhập.
 *
 * Zustand đọc ghi được cả trong lẫn ngoài React:
 *   - trong component:  useAuthStore((s) => s.user)
 *   - ở api-client:     useAuthStore.getState().clear()
 *
 * Nhờ vậy chỉ còn MỘT nguồn sự thật, không thể lệch.
 */
export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  expiresAt: null,
  user: null,
  loading: true,

  setToken: (token, expires) =>
    set({ accessToken: token, expiresAt: new Date(expires).getTime() }),

  setUser: (user) => set({ user }),

  setLoading: (loading) => set({ loading }),

  clear: () => set({ accessToken: null, expiresAt: null, user: null }),
}));

/**
 * Token sắp hết hạn chưa?
 *
 * Refresh sớm 60 giây để tránh trường hợp token hết hạn ngay giữa lúc request
 * đang bay trên đường.
 *
 * Là hàm thường chứ không phải hook: api-client gọi nó ngoài React.
 */
export function isTokenExpiringSoon(): boolean {
  const { accessToken, expiresAt } = useAuthStore.getState();
  if (!accessToken || !expiresAt) return true;
  return Date.now() >= expiresAt - 60_000;
}
