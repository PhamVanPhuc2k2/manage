export type DayStatus = "present" | "absent" | "leave" | "holiday" | "weekend";
export type PresenceStatus = "online" | "idle" | "offline";
export type ApprovalStatus = "pending" | "approved" | "rejected" | "cancelled";
export type LeaveType = "annual" | "sick" | "unpaid" | "maternity" | "other";
export type DayPart = "full" | "morning" | "afternoon";

export type TodayStatus = {
  work_date: string;
  status: PresenceStatus;
  online_minutes: number;
  active_minutes: number;
  first_seen_at?: string;
  expected_minutes: number;
  schedule_name?: string;
};

export type AttendanceDay = {
  employee_id: string;
  employee_name?: string;
  department_name?: string;
  work_date: string;
  online_minutes: number;
  active_minutes: number;
  first_seen_at?: string;
  last_seen_at?: string;
  late_minutes: number;
  early_leave_minutes: number;
  shortfall_minutes: number;
  status: DayStatus;
  is_locked: boolean;
};

export type AttendanceSession = {
  id: string;
  started_at: string;
  ended_at: string;
  minutes: number;
  active_minutes: number;
  source: "presence" | "manual" | "adjustment";
  note?: string;
};

export type MonthRow = {
  employee_id: string;
  employee_name: string;
  year: number;
  month: number;
  workday_count: number;
  present_days: number;
  absent_days: number;
  leave_days: number;
  online_minutes: number;
  active_minutes: number;
  late_minutes: number;
  late_days: number;
  shortfall_minutes: number;
};

export type TeamPresence = {
  employee_id: string;
  status: PresenceStatus;
};

export type Adjustment = {
  id: string;
  employee_id: string;
  employee_name?: string;
  work_date: string;
  requested_start: string;
  requested_end: string;
  reason: string;
  status: ApprovalStatus;
  approver_name?: string;
  decided_at?: string;
  decision_note?: string;
  created_at: string;
};

export type LeaveRequest = {
  id: string;
  employee_id: string;
  employee_name?: string;
  leave_type: LeaveType;
  start_date: string;
  end_date: string;
  day_part: DayPart;
  days: number;
  reason?: string;
  status: ApprovalStatus;
  approver_name?: string;
  decided_at?: string;
  decision_note?: string;
  created_at: string;
};

export type LeaveBalance = {
  employee_id: string;
  employee_name?: string;
  year: number;
  entitled_days: number;
  carried_over_days: number;
  used_days: number;
  remaining_days: number;
};

export type WorkSchedule = {
  id: string;
  scope: "company" | "department" | "employee";
  department_id?: string;
  employee_id?: string;
  name: string;
  work_start: string;
  work_end: string;
  workdays: number[];
  break_minutes: number;
  grace_minutes: number;
  expected_minutes: number;
  effective_from: string;
};

export type Holiday = {
  id: string;
  date: string;
  name: string;
  is_paid: boolean;
};

export const DAY_STATUS_LABEL: Record<DayStatus, string> = {
  present: "Có mặt",
  absent: "Vắng",
  leave: "Nghỉ phép",
  holiday: "Ngày lễ",
  weekend: "Cuối tuần",
};

export const DAY_STATUS_CLASS: Record<DayStatus, string> = {
  present: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  absent: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  leave: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  holiday: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  weekend:
    "bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400",
};

export const PRESENCE_LABEL: Record<PresenceStatus, string> = {
  online: "Đang làm việc",
  idle: "Đang mở nhưng không thao tác",
  offline: "Ngoại tuyến",
};

export const PRESENCE_DOT: Record<PresenceStatus, string> = {
  online: "bg-green-500",
  idle: "bg-amber-400",
  offline: "bg-neutral-300 dark:bg-neutral-600",
};

export const LEAVE_TYPE_LABEL: Record<LeaveType, string> = {
  annual: "Phép năm",
  sick: "Nghỉ ốm",
  unpaid: "Không lương",
  maternity: "Thai sản",
  other: "Khác",
};

export const DAY_PART_LABEL: Record<DayPart, string> = {
  full: "Cả ngày",
  morning: "Buổi sáng",
  afternoon: "Buổi chiều",
};

export const APPROVAL_LABEL: Record<ApprovalStatus, string> = {
  pending: "Chờ duyệt",
  approved: "Đã duyệt",
  rejected: "Từ chối",
  cancelled: "Đã huỷ",
};

export const APPROVAL_CLASS: Record<ApprovalStatus, string> = {
  pending: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  approved: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  rejected: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  cancelled:
    "bg-neutral-100 text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400",
};

const WEEKDAY_SHORT = ["T2", "T3", "T4", "T5", "T6", "T7", "CN"];

/** Đổi mảng ISO weekday (1 = thứ hai) thành chuỗi đọc được. */
export function formatWorkdays(days: number[]): string {
  if (!days?.length) return "—";
  return days
    .slice()
    .sort((a, b) => a - b)
    .map((d) => WEEKDAY_SHORT[d - 1] ?? d)
    .join(", ");
}

/** Đổi phút thành "7h30" — dạng người Việt đọc quen nhất cho thời lượng. */
export function formatDuration(minutes: number): string {
  if (!minutes) return "0h";
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m} phút`;
  if (m === 0) return `${h}h`;
  return `${h}h${String(m).padStart(2, "0")}`;
}

export function formatClock(iso?: string): string {
  if (!iso) return "—";
  return new Date(iso).toLocaleTimeString("vi-VN", {
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatDate(d?: string): string {
  if (!d) return "—";
  return new Date(d).toLocaleDateString("vi-VN");
}
