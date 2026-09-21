"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useEffect } from "react";

import { useWsMessage } from "@/lib/ws/useWebSocket";
import type { Envelope } from "@/lib/ws/client";

import * as api from "./api";
import type { Notification, NotificationSummary, NotificationType } from "./types";

export const notificationKeys = {
  all: ["notifications"] as const,
  list: (unread: boolean) => [...notificationKeys.all, "list", unread] as const,
  summary: () => [...notificationKeys.all, "summary"] as const,
  preferences: () => [...notificationKeys.all, "preferences"] as const,
};

/**
 * Danh sách thông báo, cuộn vô hạn bằng cursor.
 *
 * Cursor chứ không offset: thông báo mới chen vào TRÊN CÙNG, và trang 2 theo
 * offset sẽ lặp lại những dòng đã thấy ở trang 1.
 */
export function useNotifications(unreadOnly = false) {
  return useInfiniteQuery({
    queryKey: notificationKeys.list(unreadOnly),
    queryFn: ({ pageParam }) =>
      api.listNotifications({
        before: pageParam as string | undefined,
        unread: unreadOnly,
        limit: 20,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      // Trang ngắn hơn giới hạn nghĩa là đã tới đáy. Không kiểm tra điều này
      // thì danh sách luôn còn một "trang sau" rỗng và nút tải thêm không
      // bao giờ tắt.
      last.items.length < 20 ? undefined : (last.next_before ?? undefined),
  });
}

/**
 * Số chưa đọc cho chuông.
 *
 * KHÔNG đặt refetchInterval: con số này được đẩy qua WebSocket mỗi khi nó
 * đổi (xem useNotificationRealtime). Hỏi lại định kỳ chỉ là request thừa,
 * và tệ hơn — nó che mất việc đường realtime đã hỏng.
 */
export function useUnreadCount() {
  return useQuery({
    queryKey: notificationKeys.summary(),
    queryFn: api.getSummary,
    staleTime: 30_000,
  });
}

export function useMarkRead() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: (ids: string[]) => api.markRead(ids),
    onSuccess: (summary) => {
      qc.setQueryData(notificationKeys.summary(), summary);
      void qc.invalidateQueries({ queryKey: notificationKeys.all });
    },
  });
}

export function useMarkAllRead() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: api.markAllRead,
    onSuccess: (summary) => {
      qc.setQueryData(notificationKeys.summary(), summary);
      void qc.invalidateQueries({ queryKey: notificationKeys.all });
    },
  });
}

export function usePreferences() {
  return useQuery({
    queryKey: notificationKeys.preferences(),
    queryFn: api.getPreferences,
  });
}

export function useSetPreference() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({ type, enabled }: { type: NotificationType; enabled: boolean }) =>
      api.setPreference(type, enabled),
    onSuccess: () =>
      qc.invalidateQueries({ queryKey: notificationKeys.preferences() }),
  });
}

/**
 * Nối thông báo realtime vào cache của TanStack Query.
 *
 * Gọi MỘT LẦN ở AppShell. Chèn thẳng vào cache thay vì gọi lại API: bản tin
 * đã mang đủ dữ liệu của một thông báo, và một request cho mỗi thông báo mới
 * sẽ nhân số request lên theo mức độ bận của hệ thống.
 */
export function useNotificationRealtime() {
  const qc = useQueryClient();

  const onNotification = useCallback(
    (e: Envelope) => {
      const n = e.payload as Notification | undefined;
      if (!n?.id) return;

      // Chèn vào đầu trang đầu của mọi danh sách đang mở.
      qc.setQueriesData<{ pages: { items: Notification[] }[] }>(
        { queryKey: notificationKeys.all },
        (old) => {
          if (!old?.pages?.length) return old;
          const [first, ...rest] = old.pages;
          // Chống trùng: cùng một thông báo có thể tới hai lần khi client vừa
          // nối lại và server phát lại cho chắc.
          if (first.items.some((x) => x.id === n.id)) return old;
          return { ...old, pages: [{ ...first, items: [n, ...first.items] }, ...rest] };
        },
      );
    },
    [qc],
  );

  const onBadge = useCallback(
    (e: Envelope) => {
      const p = e.payload as NotificationSummary | undefined;
      if (typeof p?.unread !== "number") return;
      qc.setQueryData(notificationKeys.summary(), { unread: p.unread });
    },
    [qc],
  );

  useWsMessage("notification", onNotification);
  useWsMessage("notification.badge", onBadge);

  // Đồng bộ giữa các tab.
  //
  // Mỗi tab có kết nối WebSocket riêng nên đều nhận được huy hiệu, nhưng tab
  // đang ẩn có thể bị trình duyệt ngắt kết nối. BroadcastChannel bù lại phần
  // đó mà không tốn thêm request nào.
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;

    const ch = new BroadcastChannel("notifications");
    ch.onmessage = (ev: MessageEvent<{ unread?: number }>) => {
      if (typeof ev.data?.unread === "number") {
        qc.setQueryData(notificationKeys.summary(), { unread: ev.data.unread });
      }
    };
    return () => ch.close();
  }, [qc]);
}
