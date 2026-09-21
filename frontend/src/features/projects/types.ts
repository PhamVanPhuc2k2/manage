export type ProjectStatus =
  | "planning"
  | "active"
  | "on_hold"
  | "completed"
  | "cancelled";

/** Vai trò TRONG MỘT DỰ ÁN — khác hẳn vai trò hệ thống (admin, manager...). */
export type ProjectRole = "owner" | "member" | "viewer";

export type Project = {
  id: string;
  code: string;
  name: string;
  description?: string;
  status: ProjectStatus;
  owner_id: string;
  owner_name?: string;
  department_id?: string;
  department_name?: string;
  start_date?: string;
  due_date?: string;
  completed_at?: string;
  member_count: number;
  task_count: number;
  done_task_count: number;
  progress: number;
  /** Vai trò của chính người đang xem. Rỗng khi họ xem nhờ quyền toàn công ty. */
  viewer_role?: ProjectRole;
  created_at: string;
};

export type ProjectMember = {
  employee_id: string;
  employee_name: string;
  employee_code?: string;
  email?: string;
  position_name?: string;
  department_name?: string;
  role: ProjectRole;
  added_at: string;
};

export type ProjectProgress = {
  project_id: string;
  project_code: string;
  project_name: string;
  status: ProjectStatus;
  due_date?: string;
  total_tasks: number;
  by_status: Record<string, number>;
  overdue_tasks: number;
  spent_minutes: number;
  estimate_hours: number;
  progress: number;
};

export type WorkloadRow = {
  employee_id: string;
  employee_name: string;
  open_tasks: number;
  overdue_tasks: number;
  done_tasks: number;
  estimate_hours: number;
  spent_minutes: number;
};

export type ProjectFilters = {
  search?: string;
  status?: ProjectStatus | "";
  page?: number;
};

export const PROJECT_STATUS_LABEL: Record<ProjectStatus, string> = {
  planning: "Lên kế hoạch",
  active: "Đang chạy",
  on_hold: "Tạm dừng",
  completed: "Hoàn thành",
  cancelled: "Đã huỷ",
};

/**
 * Lớp CSS cho nhãn trạng thái.
 *
 * Gom bảng màu vào một chỗ để trạng thái "Đang chạy" trông giống nhau ở mọi
 * màn hình — người dùng nhận ra bằng màu trước khi kịp đọc chữ.
 */
export const PROJECT_STATUS_CLASS: Record<ProjectStatus, string> = {
  planning:
    "bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300",
  active: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  on_hold: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  completed:
    "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  cancelled: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
};

export const PROJECT_ROLE_LABEL: Record<ProjectRole, string> = {
  owner: "Chủ dự án",
  member: "Thành viên",
  viewer: "Chỉ xem",
};
