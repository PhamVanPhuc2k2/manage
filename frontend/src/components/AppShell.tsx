"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";

import { WorkStatusWidget } from "@/features/attendance/WorkStatusWidget";
import { CallHost } from "@/features/call/CallHost";
import { ChatBubble } from "@/features/chat/ChatBubble";
import { useChatRealtime } from "@/features/chat/queries";
import { NotificationBell } from "@/features/notifications/NotificationBell";
import { useNotificationRealtime } from "@/features/notifications/queries";
import { useAuth, usePermission } from "@/lib/auth/useAuth";
import { useWebSocketConnection } from "@/lib/ws/useWebSocket";

const NAV = [
  { href: "/chat", label: "Tin nhắn", permission: "chat:read" },
  { href: "/projects", label: "Dự án", permission: "project:read" },
  { href: "/tasks", label: "Công việc", permission: "task:read" },
  { href: "/attendance", label: "Chấm công", permission: "attendance:read" },
  { href: "/attendance/team", label: "Công phòng ban", permission: "attendance:read_all" },
  { href: "/leaves", label: "Nghỉ phép", permission: "leave:read" },
  { href: "/payroll/my", label: "Phiếu lương", permission: "payroll:read_own" },
  { href: "/payroll", label: "Kỳ lương", permission: "payroll:read_all" },
  { href: "/payroll/settings", label: "Cấu hình lương", permission: "salary:read" },
  { href: "/employees", label: "Nhân viên", permission: "employee:read" },
  { href: "/departments", label: "Phòng ban", permission: "department:read" },
  { href: "/positions", label: "Chức vụ", permission: "position:read" },
];

/**
 * AppShell bọc mọi trang cần đăng nhập.
 *
 * Việc chuyển hướng ở đây CHỈ để trải nghiệm mượt, KHÔNG phải kiểm soát bảo
 * mật: ai cũng gọi được API bằng curl. Quyết định thật nằm ở backend.
 */
export function AppShell({ children }: { children: React.ReactNode }) {
  const { user, loading, logout } = useAuth();
  const { can } = usePermission();
  const router = useRouter();
  const pathname = usePathname();

  // Mở WebSocket một lần cho cả ứng dụng. Đây cũng là nguồn dữ liệu chấm
  // công: client gửi nhịp tim qua chính kết nối này.
  useWebSocketConnection();

  // Nối thông báo và chat vào cache truy vấn. Gọi ở ĐÂY, một lần cho cả ứng
  // dụng: đăng ký ở từng trang sẽ khiến bản tin đến lúc đang ở trang khác bị
  // bỏ rơi, và chuông sẽ chỉ đúng khi người dùng đang mở đúng trang đó.
  useNotificationRealtime();
  useChatRealtime();

  useEffect(() => {
    if (!loading && !user) router.replace("/login");
  }, [loading, user, router]);

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center text-sm text-neutral-500">
        Đang tải...
      </div>
    );
  }
  if (!user) return null;

  const visibleNav = NAV.filter((item) => can(item.permission));

  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900">
        <div className="border-b border-neutral-200 px-4 py-4 dark:border-neutral-800">
          <div className="font-semibold">Quản lý công ty</div>
        </div>

        <nav className="p-2">
          {visibleNav.map((item) => {
            const active = pathname.startsWith(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                className={`block rounded px-3 py-2 text-sm transition ${
                  active
                    ? "bg-neutral-900 text-white dark:bg-white dark:text-neutral-900"
                    : "hover:bg-neutral-200 dark:hover:bg-neutral-800"
                }`}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center justify-between border-b border-neutral-200 px-6 py-3 dark:border-neutral-800">
          <div className="text-sm text-neutral-500">
            {user.roles.join(", ")}
          </div>
          <div className="flex items-center gap-4">
            {can("attendance:read") && <WorkStatusWidget />}
            <NotificationBell />
            <Link href="/profile" className="text-sm hover:underline">
              {user.email}
            </Link>
            <button
              onClick={logout}
              className="rounded border border-neutral-300 px-3 py-1 text-sm transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
            >
              Đăng xuất
            </button>
          </div>
        </header>

        <main className="min-w-0 flex-1 p-6">{children}</main>
      </div>

      {can("chat:read") && <ChatBubble />}
      {/* Gắn ở đây chứ không trong trang chat: cuộc gọi phải sống qua việc
          điều hướng, và cuộc gọi tới phải reo ở MỌI trang. */}
      {can("call:read") && <CallHost />}
    </div>
  );
}
