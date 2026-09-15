import type { Metadata } from "next";
import "./globals.css";
import { AuthProvider } from "@/lib/auth/AuthProvider";
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
          QueryProvider bọc NGOÀI AuthProvider.

          Thứ tự này để sau có thể dùng react-query bên trong AuthProvider
          (ví dụ cache thông tin người dùng hiện tại). Đảo lại thì AuthProvider
          không truy cập được query client.
        */}
        <QueryProvider>
          <AuthProvider>{children}</AuthProvider>
        </QueryProvider>
      </body>
    </html>
  );
}
