import { api } from "@/lib/api-client";

import type {
  NotificationPage,
  NotificationPref,
  NotificationSummary,
  NotificationType,
} from "./types";

export async function listNotifications(params: {
  before?: string;
  unread?: boolean;
  type?: NotificationType;
  limit?: number;
}): Promise<NotificationPage> {
  const q = new URLSearchParams();
  if (params.before) q.set("before", params.before);
  if (params.unread) q.set("unread", "true");
  if (params.type) q.set("type", params.type);
  if (params.limit) q.set("limit", String(params.limit));

  const { data } = await api.get<NotificationPage>(`/notifications?${q}`);
  return { items: data?.items ?? [], next_before: data?.next_before ?? null };
}

export async function getSummary(): Promise<NotificationSummary> {
  const { data } = await api.get<NotificationSummary>("/notifications/summary");
  return data ?? { unread: 0 };
}

export async function markRead(ids: string[]): Promise<NotificationSummary> {
  const { data } = await api.post<NotificationSummary>("/notifications/read", { ids });
  return data ?? { unread: 0 };
}

export async function markAllRead(): Promise<NotificationSummary> {
  const { data } = await api.post<NotificationSummary>("/notifications/read-all");
  return data ?? { unread: 0 };
}

export async function getPreferences(): Promise<NotificationPref[]> {
  const { data } = await api.get<NotificationPref[]>("/notifications/preferences");
  return data ?? [];
}

export async function setPreference(
  type: NotificationType,
  enabled: boolean,
): Promise<void> {
  await api.put("/notifications/preferences", { type, enabled });
}
