"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";

import { useAuth } from "@/lib/auth/AuthProvider";

const NAV = [
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
  const { user, loading, logout, can } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

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
    </div>
  );
}
