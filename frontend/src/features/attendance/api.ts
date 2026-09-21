import { api, type PageMeta } from "@/lib/api-client";
import type {
  Adjustment,
  AttendanceDay,
  AttendanceSession,
  Holiday,
  LeaveBalance,
  LeaveRequest,
  MonthRow,
  TeamPresence,
  TodayStatus,
  WorkSchedule,
} from "./types";

// ------------------------------------------------------------ chấm công

export async function getToday(): Promise<TodayStatus> {
  const { data } = await api.get<TodayStatus>("/attendance/today");
  return data;
}

export async function listDays(
  from: string,
  to: string,
  employeeId?: string,
  departmentId?: string,
): Promise<AttendanceDay[]> {
  const p = new URLSearchParams({ from, to });
  if (employeeId) p.set("employee_id", employeeId);
  if (departmentId) p.set("department_id", departmentId);
  const { data } = await api.get<AttendanceDay[]>(`/attendance/days?${p}`);
  return data ?? [];
}

export async function getDay(
  date: string,
  employeeId?: string,
): Promise<{ day: AttendanceDay; sessions: AttendanceSession[] }> {
  const p = new URLSearchParams({ date });
  if (employeeId) p.set("employee_id", employeeId);
  const { data } = await api.get<{
    day: AttendanceDay;
    sessions: AttendanceSession[];
  }>(`/attendance/day?${p}`);
  return data;
}

export async function getMonthSummary(
  year: number,
  month: number,
  departmentId?: string,
): Promise<MonthRow[]> {
  const p = new URLSearchParams({ year: String(year), month: String(month) });
  if (departmentId) p.set("department_id", departmentId);
  const { data } = await api.get<MonthRow[]>(`/attendance/summary?${p}`);
  return data ?? [];
}

export async function getTeamPresence(): Promise<TeamPresence[]> {
  const { data } = await api.get<TeamPresence[]>("/attendance/team");
  return data ?? [];
}

export async function checkIn(
  startedAt: string,
  endedAt: string,
  note: string,
): Promise<AttendanceSession> {
  const { data } = await api.post<AttendanceSession>("/attendance/check-in", {
    started_at: startedAt,
    ended_at: endedAt,
    note,
  });
  return data;
}

// ------------------------------------------------------ điều chỉnh công

export async function listAdjustments(status?: string): Promise<Adjustment[]> {
  const q = status ? `?status=${status}` : "";
  const { data } = await api.get<Adjustment[]>(`/attendance/adjustments${q}`);
  return data ?? [];
}

export async function createAdjustment(
  startedAt: string,
  endedAt: string,
  reason: string,
): Promise<Adjustment> {
  const { data } = await api.post<Adjustment>("/attendance/adjustments", {
    started_at: startedAt,
    ended_at: endedAt,
    reason,
  });
  return data;
}

export async function decideAdjustment(
  id: string,
  approve: boolean,
  note: string,
): Promise<Adjustment> {
  const { data } = await api.put<Adjustment>(
    `/attendance/adjustments/${id}/decision`,
    { approve, note },
  );
  return data;
}

export async function lockPeriod(
  from: string,
  to: string,
  locked: boolean,
): Promise<{ affected_days: number }> {
  const { data } = await api.post<{ affected_days: number }>("/attendance/lock", {
    from,
    to,
    locked,
  });
  return data;
}

// ------------------------------------------------------------ nghỉ phép

export async function listLeaves(params: {
  status?: string;
  employee_id?: string;
  page?: number;
}): Promise<{ items: LeaveRequest[]; meta?: PageMeta }> {
  const p = new URLSearchParams();
  if (params.status) p.set("status", params.status);
  if (params.employee_id) p.set("employee_id", params.employee_id);
  p.set("page", String(params.page ?? 1));
  const { data, meta } = await api.get<LeaveRequest[]>(`/leaves?${p}`);
  return { items: data ?? [], meta };
}

export async function createLeave(body: unknown): Promise<LeaveRequest> {
  const { data } = await api.post<LeaveRequest>("/leaves", body);
  return data;
}

export async function decideLeave(
  id: string,
  approve: boolean,
  note: string,
): Promise<LeaveRequest> {
  const { data } = await api.put<LeaveRequest>(`/leaves/${id}/decision`, {
    approve,
    note,
  });
  return data;
}

export async function cancelLeave(id: string): Promise<void> {
  await api.delete(`/leaves/${id}`);
}

export async function getBalance(
  year: number,
  employeeId?: string,
): Promise<LeaveBalance> {
  const p = new URLSearchParams({ year: String(year) });
  if (employeeId) p.set("employee_id", employeeId);
  const { data } = await api.get<LeaveBalance>(`/leaves/balance?${p}`);
  return data;
}

export async function listBalances(year: number): Promise<LeaveBalance[]> {
  const { data } = await api.get<LeaveBalance[]>(`/leaves/balances?year=${year}`);
  return data ?? [];
}

export async function setBalance(body: unknown): Promise<LeaveBalance> {
  const { data } = await api.put<LeaveBalance>("/leaves/balances", body);
  return data;
}

// --------------------------------------------------- khung giờ, ngày lễ

export async function listSchedules(): Promise<WorkSchedule[]> {
  const { data } = await api.get<WorkSchedule[]>("/work-schedules");
  return data ?? [];
}

export async function createSchedule(body: unknown): Promise<WorkSchedule> {
  const { data } = await api.post<WorkSchedule>("/work-schedules", body);
  return data;
}

export async function deleteSchedule(id: string): Promise<void> {
  await api.delete(`/work-schedules/${id}`);
}

export async function listHolidays(year: number): Promise<Holiday[]> {
  const { data } = await api.get<Holiday[]>(`/holidays?year=${year}`);
  return data ?? [];
}

export async function createHoliday(body: unknown): Promise<Holiday> {
  const { data } = await api.post<Holiday>("/holidays", body);
  return data;
}

export async function deleteHoliday(id: string): Promise<void> {
  await api.delete(`/holidays/${id}`);
}
