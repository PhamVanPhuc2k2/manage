import { api } from "@/lib/api-client";

import type { Call, CallJoin, CallKind, ICEServer } from "./types";

export async function startCall(
  conversationId: string,
  kind: CallKind,
): Promise<CallJoin> {
  const { data } = await api.post<CallJoin>("/calls", {
    conversation_id: conversationId,
    kind,
  });
  return data;
}

export async function acceptCall(callId: string): Promise<CallJoin> {
  const { data } = await api.post<CallJoin>(`/calls/${callId}/accept`);
  return data;
}

export async function rejectCall(callId: string): Promise<void> {
  await api.post(`/calls/${callId}/reject`);
}

export async function leaveCall(callId: string): Promise<void> {
  await api.post(`/calls/${callId}/leave`);
}

export async function endCall(callId: string): Promise<void> {
  await api.post(`/calls/${callId}/end`);
}

export async function refreshToken(
  callId: string,
): Promise<{ token: string; media_url: string }> {
  const { data } = await api.post<{ token: string; media_url: string }>(
    `/calls/${callId}/token`,
  );
  return data;
}

/**
 * Cuộc gọi đang diễn ra của một hội thoại, hoặc null.
 *
 * null là câu trả lời BÌNH THƯỜNG, không phải lỗi — backend cũng trả 200.
 */
export async function liveCall(conversationId: string): Promise<Call | null> {
  const { data } = await api.get<Call | null>(`/calls/live/${conversationId}`);
  return data ?? null;
}

export async function callHistory(
  conversationId: string,
  limit = 50,
): Promise<Call[]> {
  const { data } = await api.get<Call[]>(
    `/calls/history/${conversationId}?limit=${limit}`,
  );
  return data ?? [];
}

/**
 * Danh sách STUN/TURN cho đường P2P của gọi 1-1.
 *
 * KHÔNG dùng cho đường qua SFU: LiveKit tự cấp credential TURN của nó qua
 * đường signaling riêng, nên truyền thêm danh sách này vào chỉ thừa.
 */
export async function iceServers(): Promise<ICEServer[]> {
  const { data } = await api.get<{ ice_servers: ICEServer[] }>(
    "/calls/ice-servers",
  );
  return data?.ice_servers ?? [];
}
