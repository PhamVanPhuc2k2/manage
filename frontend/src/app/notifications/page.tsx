"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { relativeTime } from "@/features/notifications/format";
import {
  useMarkAllRead,
  useMarkRead,
  useNotifications,
  usePreferences,
  useSetPreference,
  useUnreadCount,
} from "@/features/notifications/queries";
import type { Notification } from "@/features/notifications/types";

export default function NotificationsPage() {
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [showPrefs, setShowPrefs] = useState(false);
  const router = useRouter();

  const { data, isLoading, hasNextPage, isFetchingNextPage, fetchNextPage, dataUpdatedAt } =
    useNotifications(unreadOnly);
  const { data: summary } = useUnreadCount();
  const markRead = useMarkRead();
  const markAllRead = useMarkAllRead();

  const items = data?.pages.flatMap((p) => p.items) ?? [];
  const unread = summary?.unread ?? 0;

  function openItem(n: Notification) {
    if (!n.read) markRead.mutate([n.id]);
    if (n.link) router.push(n.link);
  }

  return (
    <AppShell>
      <div className="mx-auto max-w-3xl">
        <div className="mb-4 flex items-center justify-between">
          <h1 className="text-xl font-semibold">
            Thông báo
            {unread > 0 && (
              <span className="ml-2 rounded-full bg-red-600 px-2 py-0.5 text-xs font-medium text-white">
                {unread}
              </span>
            )}
          </h1>

          <div className="flex items-center gap-3 text-sm">
            <label className="flex items-center gap-1.5">
              <input
                type="checkbox"
                checked={unreadOnly}
                onChange={(e) => setUnreadOnly(e.target.checked)}
              />
              Chỉ chưa đọc
            </label>
            {unread > 0 && (
              <button
                onClick={() => markAllRead.mutate()}
                className="text-blue-600 hover:underline dark:text-blue-400"
              >
                Đánh dấu đã đọc tất cả
              </button>
            )}
            <button
              onClick={() => setShowPrefs((v) => !v)}
              className="rounded border border-neutral-300 px-3 py-1 transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
            >
              Cấu hình
            </button>
          </div>
        </div>

        {showPrefs && <Preferences />}

        <div className="rounded-lg border border-neutral-200 dark:border-neutral-800">
          {isLoading && (
            <div className="px-4 py-8 text-center text-sm text-neutral-500">
              Đang tải...
            </div>
          )}
          {!isLoading && items.length === 0 && (
            <div className="px-4 py-8 text-center text-sm text-neutral-500">
              {unreadOnly ? "Không còn thông báo chưa đọc" : "Chưa có thông báo nào"}
            </div>
          )}

          {items.map((n) => (
            <button
              key={n.id}
              onClick={() => openItem(n)}
              className={`block w-full border-b border-neutral-100 px-4 py-3 text-left transition last:border-0 hover:bg-neutral-50 dark:border-neutral-800 dark:hover:bg-neutral-800 ${
                n.read ? "" : "bg-blue-50/60 dark:bg-blue-950/30"
              }`}
            >
              <div className="flex items-start gap-2">
                {!n.read && (
                  <span className="mt-2 h-2 w-2 shrink-0 rounded-full bg-blue-600" />
                )}
                <div className="min-w-0 flex-1">
                  <div className="text-sm font-medium">{n.title}</div>
                  {n.body && (
                    <div className="mt-0.5 text-sm text-neutral-600 dark:text-neutral-400">
                      {n.body}
                    </div>
                  )}
                  <div className="mt-1 text-xs text-neutral-400">
                    {relativeTime(n.created_at, dataUpdatedAt)}
                  </div>
                </div>
              </div>
            </button>
          ))}
        </div>

        {hasNextPage && (
          <div className="mt-4 text-center">
            <button
              onClick={() => void fetchNextPage()}
              disabled={isFetchingNextPage}
              className="rounded border border-neutral-300 px-4 py-2 text-sm transition hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
            >
              {isFetchingNextPage ? "Đang tải..." : "Tải thêm"}
            </button>
          </div>
        )}
      </div>
    </AppShell>
  );
}

/** Bảng bật/tắt từng loại thông báo. */
function Preferences() {
  const { data, isLoading } = usePreferences();
  const setPref = useSetPreference();

  if (isLoading) {
    return (
      <div className="mb-4 rounded-lg border border-neutral-200 px-4 py-4 text-sm text-neutral-500 dark:border-neutral-800">
        Đang tải cấu hình...
      </div>
    );
  }

  return (
    <div className="mb-4 rounded-lg border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="mb-3 text-sm font-medium">Nhận thông báo cho</div>
      <div className="grid gap-2 sm:grid-cols-2">
        {(data ?? []).map((p) => (
          <label
            key={p.type}
            className={`flex items-center gap-2 text-sm ${
              p.locked ? "text-neutral-400" : ""
            }`}
          >
            <input
              type="checkbox"
              checked={p.enabled}
              disabled={p.locked || setPref.isPending}
              onChange={(e) =>
                setPref.mutate({ type: p.type, enabled: e.target.checked })
              }
            />
            {p.label}
            {/* Loại bắt buộc: người dùng cần biết vì sao công tắc bị khoá, nếu
                không họ sẽ tưởng giao diện hỏng. */}
            {p.locked && <span className="text-xs">(bắt buộc)</span>}
          </label>
        ))}
      </div>
    </div>
  );
}
