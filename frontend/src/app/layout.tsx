import type { Metadata } from "next";
import "./globals.css";
import { AuthBootstrap } from "@/components/AuthBootstrap";
import { QueryProvider } from "@/lib/query-provider";

export const metadata: Metadata = {
  title: "Manage — Quản lý công ty",
  description: "Hệ thống quản lý nhân sự, dự án, chấm công và lương",
};

export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="vi">
      <body className="bg-white text-neutral-900 antialiased dark:bg-neutral-950 dark:text-neutral-100">
        {/*
          Không còn AuthProvider bọc cây component.

          Trạng thái đăng nhập nằm trong store Zustand — một biến toàn cục,
          component nào cần thì tự đọc. AuthBootstrap chỉ chạy một effect lúc
          ứng dụng vừa tải để khôi phục phiên từ cookie refresh.

          QueryProvider vẫn phải là Provider thật vì QueryClient cần tạo riêng
          cho mỗi request khi render phía máy chủ.
        */}
        <QueryProvider>
          <AuthBootstrap>{children}</AuthBootstrap>
        </QueryProvider>
      </body>
    </html>
  );
}
