"use client";

import { usePathname } from "next/navigation";
import { useState } from "react";

import { ConversationList } from "./ConversationList";
import { MessagePane } from "./MessagePane";
import { NewConversationDialog } from "./NewConversationDialog";
import { useChatUnread } from "./queries";

/**
 * Bong bóng chat nổi ở góc màn hình, có mặt trên MỌI trang.
 *
 * Ẩn trên chính trang /chat: ở đó đã có khung chat đầy đủ, và một bong bóng
 * mở ra cùng nội dung chỉ che mất phần màn hình đang dùng.
 */
export function ChatBubble() {
  const [open, setOpen] = useState(false);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [showNew, setShowNew] = useState(false);
  const pathname = usePathname();

  const { data: unread } = useChatUnread();

  if (pathname.startsWith("/chat")) return null;

  return (
    <>
      {open && (
        <div className="fixed bottom-20 right-5 z-40 flex h-[520px] w-[720px] max-w-[calc(100vw-2.5rem)] overflow-hidden rounded-lg border border-neutral-200 bg-white shadow-2xl dark:border-neutral-700 dark:bg-neutral-900">
          <div className="w-60 shrink-0 border-r border-neutral-200 dark:border-neutral-800">
            <ConversationList
              activeId={activeId}
              onSelect={setActiveId}
              onNew={() => setShowNew(true)}
            />
          </div>

          {activeId ? (
            <MessagePane key={activeId} conversationId={activeId} />
          ) : (
            <div className="flex flex-1 items-center justify-center px-4 text-center text-sm text-neutral-500">
              Chọn một cuộc trò chuyện
            </div>
          )}
        </div>
      )}

      <button
        onClick={() => setOpen((v) => !v)}
        aria-label={open ? "Đóng chat" : "Mở chat"}
        className="fixed bottom-5 right-5 z-40 flex h-12 w-12 items-center justify-center rounded-full bg-blue-600 text-white shadow-lg transition hover:bg-blue-700"
      >
        {open ? "✕" : <ChatIcon />}
        {!open && !!unread && unread > 0 && (
          <span className="absolute -right-0.5 -top-0.5 flex h-5 min-w-5 items-center justify-center rounded-full bg-red-600 px-1 text-[11px] font-medium">
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {showNew && (
        <NewConversationDialog
          onClose={() => setShowNew(false)}
          onCreated={setActiveId}
        />
      )}
    </>
  );
}

function ChatIcon() {
  return (
    <svg
      width="20"
      height="20"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z" />
    </svg>
  );
}
