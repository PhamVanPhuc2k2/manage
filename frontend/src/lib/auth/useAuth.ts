"use client";

import { useCallback, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";

import { api, ApiError, refreshAccessToken } from "../api-client";
import { useAuthStore } from "./store";

/**
 * Khởi động phiên khi ứng dụng vừa tải.
 *
 * Access token luôn rỗng lúc này — nó chỉ sống trong bộ nhớ nên mất khi tải
 * lại trang. Gọi refresh để lấy token mới từ cookie httpOnly: cookie còn hạn
 * thì người dùng vào thẳng, không phải đăng nhập lại sau mỗi lần F5.
 *
 * Gọi ĐÚNG MỘT LẦN ở component gốc.
 */
export function useAuthBootstrap() {
  const setUser = useAuthStore((s) => s.setUser);
  const setLoading = useAuthStore((s) => s.setLoading);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const token = await refreshAccessToken();

      if (token) {
        try {
          const { data } = await api.get("/auth/me");
          if (!cancelled) setUser(data as never);
        } catch {
          if (!cancelled) setUser(null);
        }
      }
      if (!cancelled) setLoading(false);
    })();

    return () => {
      cancelled = true;
    };
  }, [setUser, setLoading]);
}

/**
 * Trạng thái và thao tác đăng nhập.
 *
 * Mỗi giá trị lấy bằng một selector riêng để component chỉ render lại khi
 * đúng phần nó dùng thay đổi — khác với Context, nơi mọi consumer render lại
 * mỗi khi bất kỳ phần nào của giá trị đổi.
 */
export function useAuth() {
  const user = useAuthStore((s) => s.user);
  const loading = useAuthStore((s) => s.loading);
  const router = useRouter();
  const qc = useQueryClient();

  const login = useCallback(
    async (email: string, password: string) => {
      const { data } = await api.post<{
        access_token: string;
        expires_at: string;
        must_change_password: boolean;
      }>("/auth/login", { email, password }, { skipAuth: true });

      const store = useAuthStore.getState();
      store.setToken(data.access_token, data.expires_at);

      const me = await api.get("/auth/me");
      store.setUser(me.data as never);

      return { mustChangePassword: data.must_change_password };
    },
    [],
  );

  const logout = useCallback(async () => {
    try {
      await api.post("/auth/logout");
    } catch (e) {
      // Token đã hết hạn thì server không nhận diện được phiên — vẫn phải
      // dọn phía client và chuyển về trang đăng nhập.
      if (!(e instanceof ApiError)) throw e;
    } finally {
      useAuthStore.getState().clear();

      // Xoá sạch cache dữ liệu.
      //
      // Không xoá thì người đăng nhập sau trên cùng máy sẽ thấy thoáng qua
      // dữ liệu của người trước trong lúc chờ request mới về — rò rỉ thông
      // tin, nhất là với dữ liệu nhân sự.
      qc.clear();

      router.push("/login");
    }
  }, [router, qc]);

  return { user, loading, login, logout };
}

/**
 * Hook tiện lợi để ẩn/hiện nút theo quyền.
 *
 * LƯU Ý: đây CHỈ LÀ TRANG TRÍ. Ai cũng gọi được API bằng curl, nên backend
 * vẫn phải kiểm tra quyền đầy đủ. Ẩn nút chỉ để giao diện gọn gàng.
 */
export function usePermission() {
  const permissions = useAuthStore((s) => s.user?.permissions);

  const can = useCallback(
    (permission: string) => permissions?.includes(permission) ?? false,
    [permissions],
  );

  return { can };
}
