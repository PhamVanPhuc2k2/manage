"use client";

import { useEffect, useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { api, ApiError } from "@/lib/api-client";
import { useAuth } from "@/lib/auth/AuthProvider";

type Session = {
  id: string;
  device_name: string;
  ip: string;
  created_at: string;
  last_seen_at: string;
  current: boolean;
};

const formatTime = (s: string) =>
  new Date(s).toLocaleString("vi-VN", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });

export default function ProfilePage() {
  const { user, logout } = useAuth();

  const [sessions, setSessions] = useState<Session[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Xem ghi chú ở trang phòng ban: không setState đồng bộ trong thân effect,
  // và có cờ cancelled để tránh setState sau khi component đã unmount.
  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const { data } = await api.get<Session[]>("/auth/sessions");
        if (!cancelled) setSessions(data ?? []);
      } catch (e) {
        if (!cancelled) {
          setError(
            e instanceof ApiError ? e.message : "Không tải được danh sách thiết bị",
          );
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  async function handleLogoutAll() {
    try {
      await api.post("/auth/logout-all");
    } finally {
      // logout-all cắt cả phiên hiện tại, nên phải dọn phía client
      // và về trang đăng nhập.
      await logout();
    }
  }

  return (
    <AppShell>
      <h1 className="mb-6 text-xl font-semibold">Hồ sơ cá nhân</h1>

      <section className="mb-8 max-w-md rounded border border-neutral-200 p-4 dark:border-neutral-800">
        <dl className="space-y-2 text-sm">
          <div className="flex justify-between">
            <dt className="text-neutral-500">Họ tên</dt>
            <dd>{user?.full_name || "—"}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-neutral-500">Email</dt>
            <dd>{user?.email}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-neutral-500">Vai trò</dt>
            <dd>{user?.roles.join(", ")}</dd>
          </div>
          <div className="flex justify-between">
            <dt className="text-neutral-500">Phạm vi dữ liệu</dt>
            <dd>
              {{ all: "Toàn công ty", department: "Phòng ban", self: "Cá nhân" }[
                user?.scope ?? "self"
              ] ?? user?.scope}
            </dd>
          </div>
        </dl>

        <Link
          href="/change-password"
          className="mt-4 inline-block rounded border border-neutral-300 px-3 py-1.5 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
        >
          Đổi mật khẩu
        </Link>
      </section>

      <section>
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-medium">Thiết bị đang đăng nhập</h2>
          <button
            onClick={handleLogoutAll}
            className="rounded border border-red-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-300 dark:hover:bg-red-950"
          >
            Đăng xuất khỏi mọi thiết bị
          </button>
        </div>

        {error && (
          <div className="mb-3 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
            {error}
          </div>
        )}

        <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
          <table className="w-full text-sm">
            <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
              <tr>
                <th className="px-4 py-2 font-medium">Thiết bị</th>
                <th className="px-4 py-2 font-medium">Địa chỉ IP</th>
                <th className="px-4 py-2 font-medium">Đăng nhập lúc</th>
                <th className="px-4 py-2 font-medium">Hoạt động cuối</th>
              </tr>
            </thead>
            <tbody>
              {sessions.map((s) => (
                <tr
                  key={s.id}
                  className="border-t border-neutral-200 dark:border-neutral-800"
                >
                  <td className="px-4 py-2">
                    {s.device_name}
                    {s.current && (
                      <span className="ml-2 rounded bg-green-100 px-1.5 py-0.5 text-xs text-green-800 dark:bg-green-900 dark:text-green-200">
                        hiện tại
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 font-mono text-xs">{s.ip}</td>
                  <td className="px-4 py-2">{formatTime(s.created_at)}</td>
                  <td className="px-4 py-2">{formatTime(s.last_seen_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </AppShell>
  );
}
