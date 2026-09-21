export type NotificationType =
  | "task_assigned"
  | "task_status_changed"
  | "task_mentioned"
  | "task_due_soon"
  | "leave_requested"
  | "leave_decided"
  | "adjustment_requested"
  | "adjustment_decided"
  | "payslip_ready"
  | "new_message"
  | "system";

export type Notification = {
  id: string;
  type: NotificationType;
  title: string;
  body?: string;
  link?: string;
  actor_id?: string;
  actor_name?: string;
  resource?: string;
  resource_id?: string;
  read: boolean;
  read_at?: string;
  created_at: string;
};

export type NotificationPage = {
  items: Notification[];
  /** Mốc thời gian để tải trang cũ hơn. null khi đã hết. */
  next_before: string | null;
};

export type NotificationSummary = { unread: number };

export type NotificationPref = {
  type: NotificationType;
  label: string;
  enabled: boolean;
  /** Loại bắt buộc nhận, giao diện phải khoá công tắc lại. */
  locked: boolean;
};
