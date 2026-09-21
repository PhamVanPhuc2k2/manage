export type ConversationKind = "direct" | "group" | "department" | "project";
export type MessageKind = "text" | "file" | "image" | "system";
export type PresenceStatus = "online" | "idle" | "offline";

export type Conversation = {
  id: string;
  kind: ConversationKind;
  name: string;
  member_count: number;
  unread_count: number;
  is_pinned: boolean;
  is_muted: boolean;
  is_admin: boolean;
  /** Nhóm do hệ thống quản lý: không sửa được thành viên. */
  managed: boolean;
  peer_id?: string;
  peer_status?: PresenceStatus;
  last_message_at?: string;
  created_at: string;
};

export type ChatMember = {
  employee_id: string;
  employee_name: string;
  employee_code?: string;
  is_admin: boolean;
  status: PresenceStatus;
};

export type Attachment = {
  id: string;
  file_name: string;
  content_type: string;
  size_bytes: number;
  width?: number;
  height?: number;
  url?: string;
};

export type Message = {
  id: string;
  conversation_id: string;
  sender_id?: string;
  sender_name?: string;
  kind: MessageKind;
  content: string;
  reply_to_id?: string;
  reply_to_sender?: string;
  reply_to_content?: string;
  client_message_id?: string;
  attachments?: Attachment[];
  deleted: boolean;
  edited_at?: string;
  created_at: string;

  /**
   * Cờ CHỈ CÓ Ở CLIENT: tin đang chờ server xác nhận.
   *
   * Tin lạc quan được vẽ ngay khi bấm gửi để khung chat phản hồi tức thì;
   * khi bản thật về qua WebSocket, nó được thay bằng cách khớp
   * client_message_id.
   */
  pending?: boolean;
  failed?: boolean;
};

export type MessagePage = {
  items: Message[];
  next_before: string | null;
};

export type TypingEvent = {
  conversation_id: string;
  employee_id: string;
  employee_name: string;
};

export type ReadEvent = {
  conversation_id: string;
  employee_id: string;
  message_id: string;
};
