/**
 * Nơi giữ access token.
 *
 * CỐ Ý để trong biến JavaScript, KHÔNG dùng localStorage hay sessionStorage.
 *
 * localStorage đọc được bằng JavaScript, nên chỉ cần một lỗ hổng XSS ở bất kỳ
 * đâu trong ứng dụng là kẻ tấn công lấy được token. Để trong biến thì token
 * mất khi tải lại trang — nhưng đó chính là lúc refresh token trong cookie
 * httpOnly phát huy tác dụng: trang vừa tải xong gọi /auth/refresh, lấy token
 * mới, người dùng không thấy gì cả.
 */

let accessToken: string | null = null;
let expiresAt: number | null = null; // epoch milliseconds

export const tokenStore = {
  get(): string | null {
    return accessToken;
  },

  set(token: string, expires: string | Date): void {
    accessToken = token;
    expiresAt = new Date(expires).getTime();
  },

  clear(): void {
    accessToken = null;
    expiresAt = null;
  },

  /**
   * Token sắp hết hạn chưa?
   *
   * Refresh sớm 60 giây để tránh trường hợp token hết hạn ngay giữa lúc
   * request đang bay trên đường.
   */
  isExpiringSoon(): boolean {
    if (!accessToken || !expiresAt) return true;
    return Date.now() >= expiresAt - 60_000;
  },
};
