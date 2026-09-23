"use client";

import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { ConversationList } from "@/features/chat/ConversationList";
import { MessagePane } from "@/features/chat/MessagePane";
import { NewConversationDialog } from "@/features/chat/NewConversationDialog";

/**
 * Trang chat đầy đủ: danh sách bên trái, khung tin nhắn bên phải.
 *
 * Hội thoại đang mở giữ trong state chứ không đưa lên URL để tránh một lần
 * điều hướng cho mỗi lần đổi hội thoại — với chat, đó là thao tác người dùng
 * làm nhiều nhất.
 *
 * PHẢI bọc AppShell.
 *
 * Trước đây trang này không bọc, và hỏng theo kiểu chỉ lộ ra khi mở
 * TRỰC TIẾP: điều hướng từ một trang khác sang /chat thì mọi thứ chạy đúng, vì
 * AppShell của trang trước đã mở WebSocket và client là một singleton.
 * Nhưng mở thẳng địa chỉ /chat, bấm F5, hay vào bằng dấu trang thì
 * AppShell không hề gắn: không thanh điều hướng, không chuông thông báo,
 * không kết nối WebSocket, và không màn hình cuộc gọi.
 *
 * Các lớp `-m-6` và `h-[calc(100vh-57px)]` bên dưới chính là bằng chứng:
 * chúng trừ đi phần đệm và chiều cao thanh tiêu đề của AppShell, nên chỉ
 * có nghĩa khi nằm bên trong nó.
 */
export default function ChatPage() {
  const [activeId, setActiveId] = useState<string | null>(null);
  const [showNew, setShowNew] = useState(false);

  return (
    <AppShell>
      <div className="-m-6 flex h-[calc(100vh-57px)]">
        <div className="w-72 shrink-0 border-r border-neutral-200 dark:border-neutral-800">
          <ConversationList
            activeId={activeId}
            onSelect={setActiveId}
            onNew={() => setShowNew(true)}
          />
        </div>

        {activeId ? (
          <MessagePane key={activeId} conversationId={activeId} />
        ) : (
          <div className="flex flex-1 items-center justify-center text-sm text-neutral-500">
            Chọn một cuộc trò chuyện để bắt đầu
          </div>
        )}

        {showNew && (
          <NewConversationDialog
            onClose={() => setShowNew(false)}
            onCreated={setActiveId}
          />
        )}
      </div>
    </AppShell>
  );
}
