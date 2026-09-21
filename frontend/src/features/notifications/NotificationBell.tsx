"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { useMarkAllRead, useMarkRead, useNotifications, useUnreadCount } from "./queries";
import { relativeTime } from "./format";
import type { Notification } from "./types";

/**
 * Chuông thông báo trên thanh tiêu đề.
 *
 * Dùng CHUNG khoá truy vấn với trang /notifications, nên mở bảng rồi mở trang
 * không tải lại: cùng một danh sách thì nên là cùng một cache. Bản tin realtime
 * chèn thẳng vào cache đó, và cả hai chỗ cùng cập nhật.
 */
export function NotificationBell() {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const router = useRouter();

  const { data: summary } = useUnreadCount();
  const unread = summary?.unread ?? 0;

  const { data, isLoading, dataUpdatedAt } = useNotifications(false);
  const markRead = useMarkRead();
  const markAllRead = useMarkAllRead();

  // Đóng khi bấm ra ngoài.
  useEffect(() => {
    if (!open) return;

    function onClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    }
    document.addEventListener("mousedown", onClick);
    return () => document.removeEventListener("mousedown", onClick);
  }, [open]);

  const items = data?.pages.flatMap((p) => p.items).slice(0, 10) ?? [];

  function openItem(n: Notification) {
    if (!n.read) markRead.mutate([n.id]);
    setOpen(false);
    if (n.link) router.push(n.link);
  }

  return (
    <div className="relative" ref={ref}>
      <button
        onClick={() => setOpen((v) => !v)}
        aria-label={unread > 0 ? `Thông báo (${unread} chưa đọc)` : "Thông báo"}
        className="relative rounded p-1.5 transition hover:bg-neutral-100 dark:hover:bg-neutral-800"
      >
        <BellIcon />
        {unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-red-600 px-1 text-[10px] font-medium text-white">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {open && (
        <div className="absolute right-0 z-50 mt-2 w-96 rounded-lg border border-neutral-200 bg-white shadow-lg dark:border-neutral-700 dark:bg-neutral-900">
          <div className="flex items-center justify-between border-b border-neutral-200 px-4 py-2 dark:border-neutral-700">
            <span className="text-sm font-medium">Thông báo</span>
            {unread > 0 && (
              <button
                onClick={() => markAllRead.mutate()}
                className="text-xs text-blue-600 hover:underline dark:text-blue-400"
              >
                Đánh dấu đã đọc tất cả
              </button>
            )}
          </div>

          <div className="max-h-96 overflow-y-auto">
            {isLoading && (
              <div className="px-4 py-6 text-center text-sm text-neutral-500">
                Đang tải...
              </div>
            )}
            {!isLoading && items.length === 0 && (
              <div className="px-4 py-6 text-center text-sm text-neutral-500">
                Chưa có thông báo nào
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
                    <span className="mt-1.5 h-2 w-2 shrink-0 rounded-full bg-blue-600" />
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{n.title}</div>
                    {n.body && (
                      <div className="truncate text-xs text-neutral-500">{n.body}</div>
                    )}
                    <div className="mt-0.5 text-[11px] text-neutral-400">
                      {relativeTime(n.created_at, dataUpdatedAt)}
                    </div>
                  </div>
                </div>
              </button>
            ))}
          </div>

          <div className="border-t border-neutral-200 px-4 py-2 text-center dark:border-neutral-700">
            <Link
              href="/notifications"
              onClick={() => setOpen(false)}
              className="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              Xem tất cả
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}

function BellIcon() {
  return (
    <svg
      width="18"
      height="18"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
    </svg>
  );
}
