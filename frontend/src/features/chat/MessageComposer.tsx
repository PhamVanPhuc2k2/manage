"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";

import { useAuth } from "@/lib/auth/useAuth";
import { wsClient } from "@/lib/ws/client";

import * as api from "./api";
import { chatKeys, newClientMessageId, useSendMessage } from "./queries";
import type { Message, MessagePage } from "./types";

/** Khoảng tối thiểu giữa hai lần báo "đang nhập". */
const TYPING_THROTTLE_MS = 2000;

type Props = {
  conversationId: string;
  replyTo: Message | null;
  onCancelReply: () => void;
};

export function MessageComposer({ conversationId, replyTo, onCancelReply }: Props) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { user } = useAuth();
  const qc = useQueryClient();
  const send = useSendMessage(conversationId);

  const fileInput = useRef<HTMLInputElement>(null);
  const lastTypingAt = useRef(0);

  /**
   * Báo "đang nhập", có tiết chế.
   *
   * Gửi mỗi lần gõ phím sẽ là hàng chục bản tin mỗi câu, và server có giới
   * hạn 60 bản tin trong 10 giây — gõ nhanh là tự làm mình bị chặn.
   */
  function onType(value: string) {
    setText(value);

    const now = Date.now();
    if (value && now - lastTypingAt.current > TYPING_THROTTLE_MS) {
      lastTypingAt.current = now;
      wsClient.send("chat.typing", { conversation_id: conversationId });
    }
  }

  /**
   * Chèn một tin LẠC QUAN vào cache ngay khi bấm gửi.
   *
   * Khung chat phải phản hồi tức thì, kể cả khi mạng chậm. Bản thật về qua
   * WebSocket sẽ THAY tin này nhờ khớp client_message_id — xem useChatRealtime.
   */
  function addOptimistic(clientMessageId: string, content: string) {
    const optimistic: Message = {
      id: `pending-${clientMessageId}`,
      conversation_id: conversationId,
      sender_id: user?.employee_id,
      sender_name: "",
      kind: "text",
      content,
      reply_to_id: replyTo?.id,
      reply_to_sender: replyTo?.sender_name,
      reply_to_content: replyTo?.content,
      client_message_id: clientMessageId,
      deleted: false,
      created_at: new Date().toISOString(),
      pending: true,
    };

    qc.setQueriesData<{ pages: MessagePage[] }>(
      { queryKey: [...chatKeys.all, "messages", conversationId] },
      (old) => {
        if (!old?.pages?.length) return old;
        const pages = [...old.pages];
        pages[0] = { ...pages[0], items: [...pages[0].items, optimistic] };
        return { ...old, pages };
      },
    );
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    const content = text.trim();
    if (!content && files.length === 0) return;

    const clientMessageId = newClientMessageId();

    // Tin có tệp KHÔNG vẽ lạc quan: phải chờ tải tệp lên xong mới biết khoá
    // lưu trữ, và vẽ trước sẽ là một bong bóng rỗng không có ảnh.
    if (files.length === 0) {
      addOptimistic(clientMessageId, content);
    }

    setText("");
    const attached = files;
    setFiles([]);
    onCancelReply();

    try {
      let attachments: unknown[] | undefined;

      if (attached.length > 0) {
        setUploading(true);
        attachments = await Promise.all(
          attached.map(async (f) => {
            const { storage_key, upload_url } = await api.requestUpload(
              conversationId,
              f,
            );
            await api.uploadToStorage(upload_url, f);
            return {
              storage_key,
              file_name: f.name,
              content_type: f.type || "application/octet-stream",
              size_bytes: f.size,
            };
          }),
        );
      }

      await send.mutateAsync({
        content,
        kind: attached.some((f) => f.type.startsWith("image/"))
          ? "image"
          : attached.length > 0
            ? "file"
            : "text",
        replyToId: replyTo?.id,
        clientMessageId,
        attachments,
      });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Không gửi được tin nhắn");
      markFailed(clientMessageId);
    } finally {
      setUploading(false);
    }
  }

  function markFailed(clientMessageId: string) {
    qc.setQueriesData<{ pages: MessagePage[] }>(
      { queryKey: [...chatKeys.all, "messages", conversationId] },
      (old) =>
        old && {
          ...old,
          pages: old.pages.map((p) => ({
            ...p,
            items: p.items.map((m) =>
              m.client_message_id === clientMessageId
                ? { ...m, pending: false, failed: true }
                : m,
            ),
          })),
        },
    );
  }

  return (
    <form
      onSubmit={onSubmit}
      className="border-t border-neutral-200 p-3 dark:border-neutral-800"
    >
      {replyTo && (
        <div className="mb-2 flex items-start gap-2 rounded border-l-2 border-blue-500 bg-neutral-50 px-2 py-1 text-xs dark:bg-neutral-800">
          <div className="min-w-0 flex-1">
            <div className="font-medium">Trả lời {replyTo.sender_name}</div>
            <div className="truncate text-neutral-500">{replyTo.content}</div>
          </div>
          <button type="button" onClick={onCancelReply} className="text-neutral-500">
            ✕
          </button>
        </div>
      )}

      {files.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1.5">
          {files.map((f, i) => (
            <span
              key={`${f.name}-${i}`}
              className="flex items-center gap-1 rounded bg-neutral-100 px-2 py-1 text-xs dark:bg-neutral-800"
            >
              {f.name}
              <button
                type="button"
                onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                className="text-neutral-500"
              >
                ✕
              </button>
            </span>
          ))}
        </div>
      )}

      {error && <div className="mb-2 text-xs text-red-600">{error}</div>}

      <div className="flex items-end gap-2">
        <button
          type="button"
          onClick={() => fileInput.current?.click()}
          title="Đính kèm tệp"
          className="shrink-0 rounded border border-neutral-300 px-2.5 py-1.5 text-sm transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
        >
          📎
        </button>
        <input
          ref={fileInput}
          type="file"
          multiple
          hidden
          onChange={(e) => {
            setFiles(Array.from(e.target.files ?? []));
            e.target.value = "";
          }}
        />

        <textarea
          value={text}
          onChange={(e) => onType(e.target.value)}
          onKeyDown={(e) => {
            // Enter gửi, Shift+Enter xuống dòng — quy ước quen thuộc của mọi
            // ứng dụng chat, và đi ngược nó là thứ người dùng vấp ngay.
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              void onSubmit(e);
            }
          }}
          rows={1}
          placeholder="Nhập tin nhắn..."
          className="min-h-[38px] max-h-32 min-w-0 flex-1 resize-y rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-900"
        />

        <button
          type="submit"
          disabled={uploading || (!text.trim() && files.length === 0)}
          className="shrink-0 rounded bg-blue-600 px-4 py-2 text-sm text-white transition hover:bg-blue-700 disabled:opacity-50"
        >
          {uploading ? "Đang tải..." : "Gửi"}
        </button>
      </div>
    </form>
  );
}
