"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

import { useAuth } from "@/lib/auth/useAuth";

/**
 * Trang gốc chỉ điều hướng: đã đăng nhập thì vào danh sách nhân viên,
 * chưa thì ra trang đăng nhập.
 *
 * Phải chờ `loading` xong mới quyết định. Chuyển hướng ngay khi chưa biết
 * trạng thái sẽ đá người dùng ra trang đăng nhập mỗi lần tải lại trang —
 * vì access token chỉ sống trong bộ nhớ, lúc khởi động nó luôn rỗng cho tới
 * khi gọi refresh xong.
 */
export default function HomePage() {
  const { user, loading } = useAuth();
  const router = useRouter();

  useEffect(() => {
    if (loading) return;
    router.replace(user ? "/employees" : "/login");
  }, [user, loading, router]);

  return (
    <div className="flex min-h-screen items-center justify-center text-sm text-neutral-500">
      Đang tải...
    </div>
  );
}
