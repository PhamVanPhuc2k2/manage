import type { Metadata } from "next";
import { headers } from "next/headers";
import "./globals.css";
import { AuthBootstrap } from "@/components/AuthBootstrap";
import { QueryProvider } from "@/lib/query-provider";
import { THEME_SCRIPT } from "@/lib/theme";

export const metadata: Metadata = {
  title: "Manage — Quản lý công ty",
  description: "Hệ thống quản lý nhân sự, dự án, chấm công và lương",
};

export default async function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  // CSP chỉ cho script mang đúng nonce của request chạy (xem middleware.ts).
  const nonce = (await headers()).get("x-nonce") ?? undefined;

  return (
    // suppressHydrationWarning: THEME_SCRIPT đổi class của <html> trước khi
    // React gắn vào, nên class lệch với HTML máy chủ là CỐ Ý.
    <html lang="vi" suppressHydrationWarning>
      <head>
        <script
          nonce={nonce}
          dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }}
        />
      </head>
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
