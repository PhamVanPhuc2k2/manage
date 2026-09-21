"use client";

import { useState } from "react";

import { useDebounce } from "@/lib/use-debounce";

import { useConversations } from "./queries";
import { StatusDot } from "./StatusDot";
import type { Conversation } from "./types";

type Props = {
  activeId: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
};

export function ConversationList({ activeId, onSelect, onNew }: Props) {
  const [search, setSearch] = useState("");
  const debounced = useDebounce(search, 300);
  const { data, isLoading } = useConversations(debounced);

  const items = data ?? [];

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-2 border-b border-neutral-200 p-3 dark:border-neutral-800">
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Tìm hội thoại..."
          className="min-w-0 flex-1 rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-900"
        />
        <button
          onClick={onNew}
          title="Cuộc trò chuyện mới"
          className="shrink-0 rounded border border-neutral-300 px-2.5 py-1.5 text-sm transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
        >
          +
        </button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {isLoading && (
          <div className="px-3 py-6 text-center text-sm text-neutral-500">
            Đang tải...
          </div>
        )}
        {!isLoading && items.length === 0 && (
          <div className="px-3 py-6 text-center text-sm text-neutral-500">
            Chưa có cuộc trò chuyện nào
          </div>
        )}

        {items.map((c) => (
          <ConversationRow
            key={c.id}
            conversation={c}
            active={c.id === activeId}
            onSelect={onSelect}
          />
        ))}
      </div>
    </div>
  );
}

function ConversationRow({
  conversation: c,
  active,
  onSelect,
}: {
  conversation: Conversation;
  active: boolean;
  onSelect: (id: string) => void;
}) {
  return (
    <button
      onClick={() => onSelect(c.id)}
      className={`flex w-full items-center gap-2 border-b border-neutral-100 px-3 py-2.5 text-left transition dark:border-neutral-800 ${
        active
          ? "bg-neutral-100 dark:bg-neutral-800"
          : "hover:bg-neutral-50 dark:hover:bg-neutral-800/50"
      }`}
    >
      <Avatar name={c.name} kind={c.kind} />

      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1">
          {c.is_pinned && <span title="Đã ghim">📌</span>}
          <span className="truncate text-sm font-medium">{c.name || "(chưa đặt tên)"}</span>
          {c.kind === "direct" && <StatusDot status={c.peer_status} />}
        </div>
        <div className="text-xs text-neutral-500">
          {c.kind === "direct" ? "Tin nhắn riêng" : `${c.member_count} thành viên`}
          {c.is_muted && " · đã tắt thông báo"}
        </div>
      </div>

      {c.unread_count > 0 && (
        <span className="shrink-0 rounded-full bg-blue-600 px-1.5 py-0.5 text-[11px] font-medium text-white">
          {c.unread_count > 99 ? "99+" : c.unread_count}
        </span>
      )}
    </button>
  );
}

/**
 * Chữ cái đầu thay cho ảnh đại diện.
 *
 * Đủ để phân biệt các dòng trong danh sách mà không phải tải thêm một ảnh cho
 * mỗi hội thoại — danh sách này mở ở mọi trang qua bong bóng chat.
 */
function Avatar({ name, kind }: { name: string; kind: string }) {
  const letter = (name || "?").trim().charAt(0).toUpperCase();
  const color =
    kind === "department"
      ? "bg-emerald-600"
      : kind === "project"
        ? "bg-amber-600"
        : kind === "group"
          ? "bg-violet-600"
          : "bg-blue-600";

  return (
    <span
      className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-sm font-medium text-white ${color}`}
      aria-hidden="true"
    >
      {letter}
    </span>
  );
}
