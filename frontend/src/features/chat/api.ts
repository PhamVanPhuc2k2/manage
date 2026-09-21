import { api } from "@/lib/api-client";

import type { ChatMember, Conversation, Message, MessagePage } from "./types";

export async function listConversations(search?: string): Promise<Conversation[]> {
  const q = search ? `?q=${encodeURIComponent(search)}` : "";
  const { data } = await api.get<Conversation[]>(`/chat/conversations${q}`);
  return data ?? [];
}

export async function getConversation(
  id: string,
): Promise<{ conversation: Conversation; members: ChatMember[] }> {
  const { data } = await api.get<{ conversation: Conversation; members: ChatMember[] }>(
    `/chat/conversations/${id}`,
  );
  return data;
}

export async function openDirect(peerId: string): Promise<Conversation> {
  const { data } = await api.post<Conversation>("/chat/conversations", {
    kind: "direct",
    peer_id: peerId,
  });
  return data;
}

export async function createGroup(
  name: string,
  memberIds: string[],
): Promise<Conversation> {
  const { data } = await api.post<Conversation>("/chat/conversations", {
    kind: "group",
    name,
    member_ids: memberIds,
  });
  return data;
}

export async function renameConversation(
  id: string,
  name: string,
): Promise<Conversation> {
  const { data } = await api.put<Conversation>(`/chat/conversations/${id}`, { name });
  return data;
}

export async function addMembers(id: string, memberIds: string[]): Promise<void> {
  await api.post(`/chat/conversations/${id}/members`, { member_ids: memberIds });
}

export async function removeMember(id: string, employeeId: string): Promise<void> {
  await api.delete(`/chat/conversations/${id}/members/${employeeId}`);
}

export async function setAdmin(
  id: string,
  employeeId: string,
  isAdmin: boolean,
): Promise<void> {
  await api.put(`/chat/conversations/${id}/members/${employeeId}/admin`, {
    is_admin: isAdmin,
  });
}

export async function leaveConversation(id: string): Promise<void> {
  await api.post(`/chat/conversations/${id}/leave`);
}

export async function setFlags(
  id: string,
  flags: { pinned?: boolean; muted?: boolean },
): Promise<void> {
  await api.patch(`/chat/conversations/${id}/flags`, flags);
}

export async function getHistory(
  id: string,
  params: { before?: string; q?: string; limit?: number } = {},
): Promise<MessagePage> {
  const q = new URLSearchParams();
  if (params.before) q.set("before", params.before);
  if (params.q) q.set("q", params.q);
  if (params.limit) q.set("limit", String(params.limit));

  const { data } = await api.get<MessagePage>(
    `/chat/conversations/${id}/messages?${q}`,
  );
  return { items: data?.items ?? [], next_before: data?.next_before ?? null };
}

/**
 * Gửi tin qua REST.
 *
 * Đường chính là WebSocket (nhanh hơn, không tốn một vòng bắt tay HTTP cho
 * mỗi tin). Đường này là dự phòng khi kết nối đang đứt — cùng một
 * client_message_id nên gửi lại bằng đường nào cũng không sinh tin trùng.
 */
export async function sendMessage(
  id: string,
  body: {
    content: string;
    kind?: string;
    reply_to_id?: string;
    client_message_id: string;
    attachments?: unknown[];
  },
): Promise<Message> {
  const { data } = await api.post<Message>(`/chat/conversations/${id}/messages`, body);
  return data;
}

export async function editMessage(messageId: string, content: string): Promise<Message> {
  const { data } = await api.put<Message>(`/chat/messages/${messageId}`, { content });
  return data;
}

export async function deleteMessage(messageId: string): Promise<void> {
  await api.delete(`/chat/messages/${messageId}`);
}

export async function markRead(id: string, messageId: string): Promise<number> {
  const { data } = await api.post<{ unread: number }>(
    `/chat/conversations/${id}/read`,
    { message_id: messageId },
  );
  return data?.unread ?? 0;
}

export async function getUnread(): Promise<number> {
  const { data } = await api.get<{ unread: number }>("/chat/unread");
  return data?.unread ?? 0;
}

export async function requestUpload(
  id: string,
  file: File,
): Promise<{ storage_key: string; upload_url: string }> {
  const { data } = await api.post<{ storage_key: string; upload_url: string }>(
    `/chat/conversations/${id}/upload-url`,
    {
      file_name: file.name,
      content_type: file.type || "application/octet-stream",
      size_bytes: file.size,
    },
  );
  return data;
}

/**
 * Tải tệp THẲNG lên R2 bằng URL đã ký.
 *
 * Không đi qua api: đẩy vài chục megabyte qua tiến trình Go chỉ để chuyển
 * tiếp sang R2 là lãng phí băng thông và bộ nhớ của chính máy chủ đang phục
 * vụ mọi request khác.
 */
export async function uploadToStorage(url: string, file: File): Promise<void> {
  const res = await fetch(url, {
    method: "PUT",
    body: file,
    headers: { "Content-Type": file.type || "application/octet-stream" },
  });
  if (!res.ok) throw new Error(`Tải tệp lên thất bại (${res.status})`);
}
