"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { useAuth } from "@/lib/auth/useAuth";
import { clockTime, dayKey, dayLabel } from "@/features/notifications/format";

import { MessageComposer } from "./MessageComposer";
import { GroupPanel } from "./GroupPanel";
import { StatusDot } from "./StatusDot";
import {
  flattenMessages,
  useConversation,
  useMarkConversationRead,
  useMessages,
  useTypingIndicator,
} from "./queries";
import * as api from "./api";
import type { Message } from "./types";

export function MessagePane({ conversationId }: { conversationId: string }) {
  const { user } = useAuth();
  const [search, setSearch] = useState("");
  const [showPanel, setShowPanel] = useState(false);
  const [replyTo, setReplyTo] = useState<Message | null>(null);

  const { data: detail } = useConversation(conversationId);
  const { data, hasNextPage, isFetchingNextPage, fetchNextPage, isLoading } =
    useMessages(conversationId, search);
  const markRead = useMarkConversationRead();
  const typing = useTypingIndicator(conversationId);

  const messages = flattenMessages(data?.pages);
  const conversation = detail?.conversation;

  const scrollRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  // Mốc tin cuối đã báo đọc, để không gửi lại cùng một mốc mỗi lần render.
  const lastReported = useRef<string | null>(null);

  // Cuộn xuống đáy khi có tin mới, NHƯNG chỉ khi người dùng đang ở gần đáy.
  //
  // Kéo lên đọc lịch sử mà bị giật xuống đáy vì người khác vừa nhắn là lỗi
  // khó chịu nhất của mọi khung chat.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 200;
    if (nearBottom) bottomRef.current?.scrollIntoView({ block: "end" });
  }, [messages.length]);

  // Mở hội thoại thì nhảy thẳng xuống đáy, không hoạt ảnh.
  //
  // Không phải đặt lại replyTo ở đây: cả hai nơi dùng MessagePane đều truyền
  // key={conversationId}, nên đổi hội thoại là gắn lại component từ đầu và
  // mọi state cục bộ đã tự về mặc định.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [conversationId]);

  // Báo đã đọc tới tin cuối cùng.
  useEffect(() => {
    if (messages.length === 0) return;

    const last = messages[messages.length - 1];
    if (last.pending || last.id === lastReported.current) return;

    lastReported.current = last.id;
    markRead.mutate({ id: conversationId, messageId: last.id });
    // markRead là mutation ổn định của react-query, không đưa vào phụ thuộc
    // để tránh vòng lặp gọi lại.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [messages, conversationId]);

  // Cuộn NGƯỢC lên tải trang cũ hơn.
  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el || el.scrollTop > 80 || !hasNextPage || isFetchingNextPage) return;

    // Giữ nguyên vị trí nhìn thấy sau khi chèn trang cũ vào đầu: không làm
    // thì nội dung nhảy lên và người dùng mất chỗ đang đọc.
    const before = el.scrollHeight;
    void fetchNextPage().then(() => {
      requestAnimationFrame(() => {
        el.scrollTop = el.scrollHeight - before;
      });
    });
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  if (!conversation) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-neutral-500">
        {isLoading ? "Đang tải..." : "Không mở được hội thoại"}
      </div>
    );
  }

  return (
    <div className="flex h-full min-w-0 flex-1">
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-2 border-b border-neutral-200 px-4 py-2.5 dark:border-neutral-800">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5">
              <span className="truncate font-medium">{conversation.name}</span>
              {conversation.kind === "direct" && (
                <StatusDot status={conversation.peer_status} />
              )}
            </div>
            <div className="text-xs text-neutral-500">
              {conversation.kind === "direct"
                ? "Tin nhắn riêng"
                : `${conversation.member_count} thành viên`}
            </div>
          </div>

          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Tìm trong hội thoại..."
            className="w-48 rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-900"
          />
          <button
            onClick={() => setShowPanel((v) => !v)}
            className="rounded border border-neutral-300 px-2.5 py-1 text-sm transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
          >
            Chi tiết
          </button>
        </header>

        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex-1 overflow-y-auto px-4 py-3"
        >
          {isFetchingNextPage && (
            <div className="py-2 text-center text-xs text-neutral-500">
              Đang tải tin cũ hơn...
            </div>
          )}
          {!hasNextPage && messages.length > 0 && !search && (
            <div className="py-2 text-center text-xs text-neutral-400">
              Đầu cuộc trò chuyện
            </div>
          )}
          {messages.length === 0 && !isLoading && (
            <div className="py-8 text-center text-sm text-neutral-500">
              {search ? "Không tìm thấy tin nhắn nào" : "Chưa có tin nhắn nào"}
            </div>
          )}

          {messages.map((m, i) => {
            const prev = messages[i - 1];
            const newDay = !prev || dayKey(prev.created_at) !== dayKey(m.created_at);

            return (
              <div key={m.id}>
                {newDay && (
                  <div className="my-3 text-center text-xs text-neutral-400">
                    {dayLabel(m.created_at)}
                  </div>
                )}
                <MessageRow
                  message={m}
                  own={m.sender_id === user?.employee_id}
                  groupedWithPrev={
                    !newDay && !!prev && prev.sender_id === m.sender_id && !prev.deleted
                  }
                  onReply={() => setReplyTo(m)}
                />
              </div>
            );
          })}

          <div ref={bottomRef} />
        </div>

        {typing.length > 0 && (
          <div className="px-4 pb-1 text-xs text-neutral-500">
            {typing.join(", ")} đang nhập...
          </div>
        )}

        <MessageComposer
          conversationId={conversationId}
          replyTo={replyTo}
          onCancelReply={() => setReplyTo(null)}
        />
      </div>

      {showPanel && (
        <GroupPanel
          conversationId={conversationId}
          onClose={() => setShowPanel(false)}
        />
      )}
    </div>
  );
}

function MessageRow({
  message: m,
  own,
  groupedWithPrev,
  onReply,
}: {
  message: Message;
  own: boolean;
  groupedWithPrev: boolean;
  onReply: () => void;
}) {
  const [busy, setBusy] = useState(false);

  // Tin hệ thống nằm giữa, không có bong bóng: nó không thuộc về ai.
  if (m.kind === "system") {
    return (
      <div className="my-2 text-center text-xs text-neutral-500">{m.content}</div>
    );
  }

  async function onDelete() {
    if (!confirm("Thu hồi tin nhắn này?")) return;
    setBusy(true);
    try {
      await api.deleteMessage(m.id);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className={`group mb-1 flex ${own ? "justify-end" : "justify-start"}`}>
      <div className={`max-w-[70%] ${own ? "items-end" : "items-start"}`}>
        {!own && !groupedWithPrev && (
          <div className="mb-0.5 text-xs text-neutral-500">{m.sender_name}</div>
        )}

        <div
          className={`rounded-2xl px-3 py-2 text-sm ${
            m.deleted
              ? "bg-neutral-100 italic text-neutral-400 dark:bg-neutral-800"
              : own
                ? "bg-blue-600 text-white"
                : "bg-neutral-100 dark:bg-neutral-800"
          } ${m.pending ? "opacity-60" : ""} ${m.failed ? "ring-1 ring-red-500" : ""}`}
        >
          {m.reply_to_id && !m.deleted && (
            <div
              className={`mb-1 border-l-2 pl-2 text-xs ${
                own ? "border-white/50 text-white/80" : "border-neutral-400 text-neutral-500"
              }`}
            >
              <div className="font-medium">{m.reply_to_sender || "Tin nhắn"}</div>
              <div className="truncate">{m.reply_to_content}</div>
            </div>
          )}

          {m.deleted ? (
            "Tin nhắn đã được thu hồi"
          ) : (
            <span className="whitespace-pre-wrap break-words">{m.content}</span>
          )}

          {!m.deleted && m.attachments?.map((a) => <AttachmentView key={a.id} a={a} />)}

          <div
            className={`mt-0.5 text-[10px] ${own ? "text-white/70" : "text-neutral-400"}`}
          >
            {clockTime(m.created_at)}
            {m.edited_at && " · đã sửa"}
            {m.pending && " · đang gửi"}
            {m.failed && " · gửi lỗi"}
          </div>
        </div>

        {!m.deleted && !m.pending && (
          <div className="mt-0.5 flex gap-2 opacity-0 transition group-hover:opacity-100">
            <button onClick={onReply} className="text-[11px] text-neutral-500 hover:underline">
              Trả lời
            </button>
            {own && (
              <button
                onClick={onDelete}
                disabled={busy}
                className="text-[11px] text-neutral-500 hover:underline disabled:opacity-50"
              >
                Thu hồi
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function AttachmentView({ a }: { a: { file_name: string; content_type: string; size_bytes: number; url?: string } }) {
  const isImage = a.content_type.startsWith("image/");

  if (isImage && a.url) {
    return (
      <a href={a.url} target="_blank" rel="noopener noreferrer" className="mt-1 block">
        {/* Thẻ img thường chứ không phải next/image: URL đã ký từ R2 có chữ ký
            trong query string và hết hạn, nên tối ưu hoá phía máy chủ của
            Next không dùng được. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={a.url}
          alt={a.file_name}
          className="max-h-64 rounded"
          loading="lazy"
        />
      </a>
    );
  }

  return (
    <a
      href={a.url}
      target="_blank"
      rel="noopener noreferrer"
      className="mt-1 flex items-center gap-1.5 rounded bg-black/10 px-2 py-1 text-xs hover:underline"
    >
      📎 {a.file_name} ({Math.round(a.size_bytes / 1024)} KB)
    </a>
  );
}
