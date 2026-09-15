"use client";

import { useAuthBootstrap } from "@/lib/auth/useAuth";

/**
 * Khởi động phiên đăng nhập một lần cho toàn ứng dụng.
 *
 * Không còn Provider bọc cây component nữa: store của Zustand là biến toàn
 * cục, component nào cần thì tự đọc. Thành phần này chỉ để chạy đúng một
 * effect lúc ứng dụng vừa tải.
 */
export function AuthBootstrap({ children }: { children: React.ReactNode }) {
  useAuthBootstrap();
  return <>{children}</>;
}
