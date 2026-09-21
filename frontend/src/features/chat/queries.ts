"use client";

import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

import type { Envelope } from "@/lib/ws/client";
import { wsClient } from "@/lib/ws/client";
import { useWsMessage } from "@/lib/ws/useWebSocket";

import * as api from "./api";
import type {
  Conversation,
  Message,
  MessagePage,
  ReadEvent,
  TypingEvent,
} from "./types";

export const chatKeys = {
  all: ["chat"] as const,
  conversations: (q: string) => [...chatKeys.all, "conversations", q] as const,
  conversation: (id: string) => [...chatKeys.all, "conversation", id] as const,
  messages: (id: string, q: string) => [...chatKeys.all, "messages", id, q] as const,
  unread: () => [...chatKeys.all, "unread"] as const,
};

const PAGE_SIZE = 50;

export function useConversations(search = "") {
  return useQuery({
    queryKey: chatKeys.conversations(search),
    queryFn: () => api.listConversations(search),
    staleTime: 15_000,
  });
}

export function useConversation(id: string | null) {
  return useQuery({
    queryKey: chatKeys.conversation(id ?? ""),
    queryFn: () => api.getConversation(id as string),
    enabled: !!id,
  });
}

/**
 * Lịch sử tin nhắn, cuộn NGƯỢC lên bằng cursor.
 *
 * Mỗi trang trả về theo thứ tự tăng dần, và trang "sau" là trang CŨ hơn —
 * ngược với mọi danh sách khác trong hệ thống. Vì vậy khi ghép các trang lại
 * phải đảo thứ tự mảng pages, xem flattenMessages.
 */
export function useMessages(id: string | null, search = "") {
  return useInfiniteQuery({
    queryKey: chatKeys.messages(id ?? "", search),
    queryFn: ({ pageParam }) =>
      api.getHistory(id as string, {
        before: pageParam as string | undefined,
        q: search || undefined,
        limit: PAGE_SIZE,
      }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) =>
      last.items.length < PAGE_SIZE ? undefined : (last.next_before ?? undefined),
    enabled: !!id,
  });
}

/** Ghép các trang thành một mảng tăng dần theo thời gian. */
export function flattenMessages(pages?: MessagePage[]): Message[] {
  if (!pages) return [];
  return [...pages].reverse().flatMap((p) => p.items);
}

export function useChatUnread() {
  return useQuery({
    queryKey: chatKeys.unread(),
    queryFn: api.getUnread,
    staleTime: 30_000,
  });
}

/* ------------------------------------------------------------------ *
 * Gửi tin
 * ------------------------------------------------------------------ */

/** Sinh mã chống trùng cho một tin sắp gửi. */
export function newClientMessageId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * Gửi tin: ưu tiên WebSocket, rơi về REST khi kết nối đang đứt.
 *
 * Cùng một client_message_id cho cả hai đường, nên gửi lại bằng đường kia sau
 * khi mất mạng KHÔNG sinh tin trùng — server tra mã đó trước khi ghi.
 */
export function useSendMessage(conversationId: string | null) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (input: {
      content: string;
      kind?: string;
      replyToId?: string;
      clientMessageId: string;
      attachments?: unknown[];
    }) => {
      if (!conversationId) return;

      const viaWs =
        !input.attachments?.length &&
        wsClient.getStatus() === "open" &&
        wsClient.send("chat.send", {
          conversation_id: conversationId,
          content: input.content,
          kind: input.kind,
          reply_to_id: input.replyToId,
          client_message_id: input.clientMessageId,
        });
      if (viaWs) return;

      await api.sendMessage(conversationId, {
        content: input.content,
        kind: input.kind,
        reply_to_id: input.replyToId,
        client_message_id: input.clientMessageId,
        attachments: input.attachments,
      });
    },
    onError: () => {
      // Tin lạc quan vẫn nằm trên màn hình; đánh dấu hỏng để người dùng biết
      // mà gửi lại, thay vì tưởng đã gửi xong.
      if (conversationId) {
        void qc.invalidateQueries({ queryKey: chatKeys.all });
      }
    },
  });
}

export function useMarkConversationRead() {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({ id, messageId }: { id: string; messageId: string }) =>
      api.markRead(id, messageId),
    onSuccess: (unread, { id }) => {
      qc.setQueryData(chatKeys.unread(), unread);

      // Hạ số chưa đọc của đúng hội thoại này trong danh sách, không tải lại
      // cả danh sách: người dùng mở một hội thoại là chuyện xảy ra liên tục.
      qc.setQueriesData<Conversation[]>(
        { queryKey: [...chatKeys.all, "conversations"] },
        (old) =>
          old?.map((c) => (c.id === id ? { ...c, unread_count: 0 } : c)),
      );

      broadcastRead(id);
    },
  });
}

/* ------------------------------------------------------------------ *
 * Đồng bộ giữa các tab
 * ------------------------------------------------------------------ */

const READ_CHANNEL = "chat-read";

function broadcastRead(conversationId: string) {
  if (typeof BroadcastChannel === "undefined") return;
  const ch = new BroadcastChannel(READ_CHANNEL);
  ch.postMessage({ conversationId });
  ch.close();
}

/* ------------------------------------------------------------------ *
 * Realtime
 * ------------------------------------------------------------------ */

/**
 * Nối mọi sự kiện chat vào cache. Gọi MỘT LẦN ở AppShell.
 *
 * Chèn thẳng vào cache thay vì tải lại: một nhóm đang trao đổi sôi nổi sẽ
 * sinh vài tin mỗi giây, và tải lại cả trang lịch sử cho mỗi tin là cách
 * chắc chắn để khung chat giật.
 */
export function useChatRealtime() {
  const qc = useQueryClient();

  const onMessage = useCallback(
    (e: Envelope) => {
      const m = e.payload as Message | undefined;
      if (!m?.id) return;

      qc.setQueriesData<{ pages: MessagePage[] }>(
        { queryKey: [...chatKeys.all, "messages", m.conversation_id] },
        (old) => {
          if (!old?.pages?.length) return old;

          // Trang ĐẦU là trang mới nhất, và tin mới nằm ở cuối trang đó.
          const pages = [...old.pages];
          const first = pages[0];

          // Ba trường hợp trùng phải xử lý ở đây:
          //   - cùng id: bản tin về hai lần (nối lại kết nối)
          //   - cùng client_message_id: tin lạc quan của chính mình vừa được
          //     server xác nhận, phải THAY chứ không thêm
          const byId = first.items.findIndex((x) => x.id === m.id);
          if (byId >= 0) return old;

          const byClient = m.client_message_id
            ? first.items.findIndex(
                (x) => x.client_message_id === m.client_message_id && x.pending,
              )
            : -1;

          const items =
            byClient >= 0
              ? first.items.map((x, i) => (i === byClient ? m : x))
              : [...first.items, m];

          pages[0] = { ...first, items };
          return { ...old, pages };
        },
      );

      // Danh sách hội thoại: nâng số chưa đọc và mốc hoạt động.
      qc.setQueriesData<Conversation[]>(
        { queryKey: [...chatKeys.all, "conversations"] },
        (old) =>
          old?.map((c) =>
            c.id === m.conversation_id
              ? {
                  ...c,
                  last_message_at: m.created_at,
                  unread_count: c.unread_count + 1,
                }
              : c,
          ),
      );
      void qc.invalidateQueries({ queryKey: chatKeys.unread() });
    },
    [qc],
  );

  const onEdited = useCallback(
    (e: Envelope) => {
      const m = e.payload as Message | undefined;
      if (!m?.id) return;

      qc.setQueriesData<{ pages: MessagePage[] }>(
        { queryKey: [...chatKeys.all, "messages", m.conversation_id] },
        (old) =>
          old && {
            ...old,
            pages: old.pages.map((p) => ({
              ...p,
              items: p.items.map((x) => (x.id === m.id ? m : x)),
            })),
          },
      );
    },
    [qc],
  );

  const onDeleted = useCallback(
    (e: Envelope) => {
      const p = e.payload as { id: string; conversation_id: string } | undefined;
      if (!p?.id) return;

      qc.setQueriesData<{ pages: MessagePage[] }>(
        { queryKey: [...chatKeys.all, "messages", p.conversation_id] },
        (old) =>
          old && {
            ...old,
            pages: old.pages.map((page) => ({
              ...page,
              items: page.items.map((x) =>
                x.id === p.id ? { ...x, deleted: true, content: "" } : x,
              ),
            })),
          },
      );
    },
    [qc],
  );

  const onBadge = useCallback(
    (e: Envelope) => {
      const p = e.payload as { unread?: number } | undefined;
      if (typeof p?.unread === "number") qc.setQueryData(chatKeys.unread(), p.unread);
    },
    [qc],
  );

  useWsMessage("chat.message", onMessage);
  useWsMessage("chat.message_edited", onEdited);
  useWsMessage("chat.message_deleted", onDeleted);
  useWsMessage("chat.badge", onBadge);

  // Đọc ở tab này thì tab kia cũng hạ số chưa đọc xuống.
  useEffect(() => {
    if (typeof BroadcastChannel === "undefined") return;

    const ch = new BroadcastChannel(READ_CHANNEL);
    ch.onmessage = (ev: MessageEvent<{ conversationId?: string }>) => {
      const id = ev.data?.conversationId;
      if (!id) return;

      qc.setQueriesData<Conversation[]>(
        { queryKey: [...chatKeys.all, "conversations"] },
        (old) => old?.map((c) => (c.id === id ? { ...c, unread_count: 0 } : c)),
      );
      void qc.invalidateQueries({ queryKey: chatKeys.unread() });
    };
    return () => ch.close();
  }, [qc]);
}

/**
 * Danh sách người đang gõ trong một hội thoại.
 *
 * Tự xoá sau 4 giây: client gửi chỉ báo mỗi 2 giây trong lúc gõ, nên quá hai
 * chu kỳ mà im nghĩa là họ đã ngừng. Không có hạn này thì chữ "đang nhập"
 * đứng lại vĩnh viễn khi ai đó đóng tab giữa chừng.
 */
export function useTypingIndicator(conversationId: string | null): string[] {
  // State mang theo id hội thoại mà nó thuộc về.
  //
  // Nhờ vậy việc "đổi hội thoại thì xoá sạch" là một phép so sánh lúc đọc,
  // không phải một effect gọi setState — effect như thế gây thêm một vòng
  // render và bị quy tắc react-hooks/set-state-in-effect chặn.
  const [typing, setTyping] = useState<{
    conv: string | null;
    map: Record<string, { name: string; at: number }>;
  }>({ conv: conversationId, map: {} });

  const onTyping = useCallback(
    (e: Envelope) => {
      const p = e.payload as TypingEvent | undefined;
      if (!p || p.conversation_id !== conversationId) return;

      setTyping((prev) => ({
        conv: conversationId,
        map: {
          ...(prev.conv === conversationId ? prev.map : {}),
          [p.employee_id]: { name: p.employee_name, at: Date.now() },
        },
      }));
    },
    [conversationId],
  );

  useWsMessage("chat.typing", onTyping);

  // Dọn định kỳ. Đặt trong interval chứ không lọc lúc render: lọc lúc render
  // dùng Date.now() làm hàm render không thuần khiết, và React sẽ không vẽ
  // lại khi không có gì thay đổi — chữ "đang nhập" sẽ đứng mãi.
  useEffect(() => {
    const t = setInterval(() => {
      setTyping((prev) => {
        const now = Date.now();
        const map = Object.fromEntries(
          Object.entries(prev.map).filter(([, v]) => now - v.at < 4000),
        );
        return Object.keys(map).length === Object.keys(prev.map).length
          ? prev
          : { ...prev, map };
      });
    }, 1000);
    return () => clearInterval(t);
  }, []);

  if (typing.conv !== conversationId) return [];
  return Object.values(typing.map).map((v) => v.name);
}

/** Nhận sự kiện "đã đọc" của người khác, để hiện dấu đã xem. */
export function useReadReceipts(conversationId: string | null) {
  // Cùng cách như useTypingIndicator: state mang theo id hội thoại của nó,
  // nên không cần một effect chỉ để xoá state khi đổi phòng.
  const [reads, setReads] = useState<{
    conv: string | null;
    map: Record<string, string>;
  }>({ conv: conversationId, map: {} });

  const onRead = useCallback(
    (e: Envelope) => {
      const p = e.payload as ReadEvent | undefined;
      if (!p || p.conversation_id !== conversationId) return;

      setReads((prev) => ({
        conv: conversationId,
        map: {
          ...(prev.conv === conversationId ? prev.map : {}),
          [p.employee_id]: p.message_id,
        },
      }));
    },
    [conversationId],
  );

  useWsMessage("chat.read", onRead);

  return reads.conv === conversationId ? reads.map : {};
}
