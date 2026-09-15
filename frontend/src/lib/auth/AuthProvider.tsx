"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { useRouter } from "next/navigation";

import { api, ApiError, refreshAccessToken } from "../api-client";
import { tokenStore } from "./token-store";

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
  user: CurrentUser | null;
  /** Đang kiểm tra phiên lúc khởi động — chưa biết đã đăng nhập hay chưa. */
  loading: boolean;
  login: (email: string, password: string) => Promise<{ mustChangePassword: boolean }>;
  logout: () => Promise<void>;
  can: (permission: string) => boolean;
  reload: () => Promise<void>;
};

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<CurrentUser | null>(null);
  const [loading, setLoading] = useState(true);
  const router = useRouter();

  const loadMe = useCallback(async () => {
    try {
      const { data } = await api.get<CurrentUser>("/auth/me");
      setUser(data);
    } catch {
      setUser(null);
    }
  }, []);

  // Lúc khởi động, access token luôn rỗng (nó chỉ sống trong bộ nhớ và mất
  // khi tải lại trang). Gọi refresh để lấy token mới từ cookie httpOnly —
  // nếu cookie còn hạn thì người dùng vào thẳng, không phải đăng nhập lại.
  useEffect(() => {
    let cancelled = false;

    (async () => {
      const token = await refreshAccessToken();
      if (cancelled) return;
      if (token) await loadMe();
      if (!cancelled) setLoading(false);
    })();

    return () => {
      cancelled = true;
    };
  }, [loadMe]);

  const login = useCallback(
    async (email: string, password: string) => {
      const { data } = await api.post<{
        access_token: string;
        expires_at: string;
        must_change_password: boolean;
      }>("/auth/login", { email, password }, { skipAuth: true });

      tokenStore.set(data.access_token, data.expires_at);
      await loadMe();

      return { mustChangePassword: data.must_change_password };
    },
    [loadMe],
  );

  const logout = useCallback(async () => {
    try {
      await api.post("/auth/logout");
    } catch (e) {
      // Token đã hết hạn thì server không nhận diện được phiên — vẫn phải
      // dọn phía client và chuyển về trang đăng nhập.
      if (!(e instanceof ApiError)) throw e;
    } finally {
      tokenStore.clear();
      setUser(null);
      router.push("/login");
    }
  }, [router]);

  const can = useCallback(
    (permission: string) => user?.permissions.includes(permission) ?? false,
    [user],
  );

  const value = useMemo<AuthState>(
    () => ({ user, loading, login, logout, can, reload: loadMe }),
    [user, loading, login, logout, can, loadMe],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth phải nằm trong AuthProvider");
  return ctx;
}

/**
 * Hook tiện lợi để ẩn/hiện nút theo quyền.
 *
 * LƯU Ý: đây CHỈ LÀ TRANG TRÍ. Ai cũng gọi được API bằng curl, nên backend
 * vẫn phải kiểm tra quyền đầy đủ. Ẩn nút chỉ để giao diện gọn gàng.
 */
export function usePermission() {
  const { can } = useAuth();
  return { can };
}
