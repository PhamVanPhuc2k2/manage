export type TaskStatus = "todo" | "in_progress" | "review" | "done";
export type TaskPriority = "low" | "medium" | "high" | "urgent";

export type Task = {
  id: string;
  code: string;
  seq: number;
  project_id: string;
  project_code?: string;
  project_name?: string;
  parent_task_id?: string;

  title: string;
  description?: string;
  status: TaskStatus;
  priority: TaskPriority;
  assignee_id?: string;
  assignee_name?: string;
  reporter_id: string;
  reporter_name?: string;
  due_date?: string;
  estimate_hours?: number;
  sort_order: number;

  started_at?: string;
  completed_at?: string;
  overdue: boolean;

  comment_count: number;
  attachment_count: number;
  subtask_count: number;
  done_subtasks: number;
  spent_minutes: number;

  created_at: string;
  updated_at: string;
};

export type BoardColumn = {
  status: TaskStatus;
  tasks: Task[];
};

export type Board = {
  project: import("../projects/types").Project;
  columns: BoardColumn[];
};

export type TaskComment = {
  id: string;
  task_id: string;
  author_id: string;
  author_name?: string;
  content: string;
  mentioned_ids: string[];
  edited_at?: string;
  created_at: string;
};

export type TaskAttachment = {
  id: string;
  task_id: string;
  file_name: string;
  content_type: string;
  size_bytes: number;
  uploader_name?: string;
  download_url?: string;
  created_at: string;
};

export type TaskActivity = {
  id: string;
  actor_name?: string;
  action: string;
  field?: string;
  old_value?: string;
  new_value?: string;
  created_at: string;
};

export type Timelog = {
  id: string;
  task_id: string;
  employee_id: string;
  employee_name?: string;
  spent_minutes: number;
  note?: string;
  logged_on: string;
  created_at: string;
};

export type TaskFilters = {
  project_id?: string;
  status?: TaskStatus | "";
  priority?: TaskPriority | "";
  assignee_id?: string;
  search?: string;
  unfinished?: boolean;
  page?: number;
};

/** Thứ tự cột Kanban, từ trái sang phải. Khớp với BoardColumns() ở backend. */
export const BOARD_ORDER: TaskStatus[] = [
  "todo",
  "in_progress",
  "review",
  "done",
];

export const TASK_STATUS_LABEL: Record<TaskStatus, string> = {
  todo: "Cần làm",
  in_progress: "Đang làm",
  review: "Chờ duyệt",
  done: "Hoàn thành",
};

export const TASK_PRIORITY_LABEL: Record<TaskPriority, string> = {
  low: "Thấp",
  medium: "Trung bình",
  high: "Cao",
  urgent: "Khẩn cấp",
};

export const TASK_PRIORITY_CLASS: Record<TaskPriority, string> = {
  low: "bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400",
  medium: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  high: "bg-orange-100 text-orange-800 dark:bg-orange-950 dark:text-orange-300",
  urgent: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
};

/**
 * Bước chuyển trạng thái hợp lệ. PHẢI khớp với allowedTransitions ở backend.
 *
 * Bản sao này chỉ để giao diện không mời người dùng làm việc chắc chắn hỏng
 * (làm mờ vùng thả không hợp lệ). Backend vẫn là nơi quyết định — ai cũng gọi
 * API bằng curl được.
 */
const ALLOWED_TRANSITIONS: Record<TaskStatus, TaskStatus[]> = {
  todo: ["in_progress"],
  in_progress: ["todo", "review"],
  review: ["in_progress", "done"],
  done: ["review"],
};

export function canTransition(from: TaskStatus, to: TaskStatus): boolean {
  return from === to || ALLOWED_TRANSITIONS[from].includes(to);
}

export function formatMinutes(m: number): string {
  if (!m) return "0h";
  const h = Math.floor(m / 60);
  const rem = m % 60;
  if (h === 0) return `${rem}p`;
  if (rem === 0) return `${h}h`;
  return `${h}h${rem}p`;
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
