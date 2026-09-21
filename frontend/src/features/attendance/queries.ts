import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import * as api from "./api";

export const attendanceKeys = {
  all: ["attendance"] as const,
  today: () => [...attendanceKeys.all, "today"] as const,
  days: (from: string, to: string, emp?: string, dept?: string) =>
    [...attendanceKeys.all, "days", from, to, emp ?? "", dept ?? ""] as const,
  day: (date: string, emp?: string) =>
    [...attendanceKeys.all, "day", date, emp ?? ""] as const,
  summary: (y: number, m: number, dept?: string) =>
    [...attendanceKeys.all, "summary", y, m, dept ?? ""] as const,
  team: () => [...attendanceKeys.all, "team"] as const,
  adjustments: (status?: string) =>
    [...attendanceKeys.all, "adjustments", status ?? "all"] as const,
  schedules: () => [...attendanceKeys.all, "schedules"] as const,
  holidays: (year: number) => [...attendanceKeys.all, "holidays", year] as const,
};

export const leaveKeys = {
  all: ["leaves"] as const,
  list: (p: object) => [...leaveKeys.all, "list", p] as const,
  balance: (year: number, emp?: string) =>
    [...leaveKeys.all, "balance", year, emp ?? "me"] as const,
  balances: (year: number) => [...leaveKeys.all, "balances", year] as const,
};

/**
 * Tình hình hôm nay. Làm mới mỗi phút.
 *
 * Đúng bằng chu kỳ job quét presence ở backend: làm mới dày hơn cũng không
 * thấy con số mới, chỉ tốn request.
 */
export function useToday() {
  return useQuery({
    queryKey: attendanceKeys.today(),
    queryFn: api.getToday,
    refetchInterval: 60_000,
  });
}

export function useAttendanceDays(
  from: string,
  to: string,
  employeeId?: string,
  departmentId?: string,
) {
  return useQuery({
    queryKey: attendanceKeys.days(from, to, employeeId, departmentId),
    queryFn: () => api.listDays(from, to, employeeId, departmentId),
    placeholderData: keepPreviousData,
  });
}

export function useAttendanceDay(date: string, employeeId?: string) {
  return useQuery({
    queryKey: attendanceKeys.day(date, employeeId),
    queryFn: () => api.getDay(date, employeeId),
    enabled: Boolean(date),
  });
}

export function useMonthSummary(year: number, month: number, departmentId?: string) {
  return useQuery({
    queryKey: attendanceKeys.summary(year, month, departmentId),
    queryFn: () => api.getMonthSummary(year, month, departmentId),
    placeholderData: keepPreviousData,
  });
}

/**
 * Ai đang online. Làm mới mỗi 30 giây.
 *
 * Nhanh hơn bảng công vì đây là thông tin "ngay bây giờ": người dùng mở màn
 * hình này để biết có hỏi được đồng nghiệp hay không, và một con số cũ 5
 * phút là vô dụng.
 */
export function useTeamPresence(enabled = true) {
  return useQuery({
    queryKey: attendanceKeys.team(),
    queryFn: api.getTeamPresence,
    refetchInterval: 30_000,
    enabled,
  });
}

export function useCheckIn() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      startedAt,
      endedAt,
      note,
    }: {
      startedAt: string;
      endedAt: string;
      note: string;
    }) => api.checkIn(startedAt, endedAt, note),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

// ------------------------------------------------------ điều chỉnh công

export function useAdjustments(status?: string) {
  return useQuery({
    queryKey: attendanceKeys.adjustments(status),
    queryFn: () => api.listAdjustments(status),
  });
}

export function useCreateAdjustment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      startedAt,
      endedAt,
      reason,
    }: {
      startedAt: string;
      endedAt: string;
      reason: string;
    }) => api.createAdjustment(startedAt, endedAt, reason),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

export function useDecideAdjustment() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      approve,
      note,
    }: {
      id: string;
      approve: boolean;
      note: string;
    }) => api.decideAdjustment(id, approve, note),
    onSuccess: () => {
      // Duyệt điều chỉnh ghi thêm một phiên và tổng hợp lại ngày đó, nên cả
      // bảng công cũng cũ theo.
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

// ------------------------------------------------------------ nghỉ phép

export function useLeaves(params: { status?: string; page?: number }) {
  return useQuery({
    queryKey: leaveKeys.list(params),
    queryFn: () => api.listLeaves(params),
    placeholderData: keepPreviousData,
  });
}

export function useLeaveBalance(year: number, employeeId?: string) {
  return useQuery({
    queryKey: leaveKeys.balance(year, employeeId),
    queryFn: () => api.getBalance(year, employeeId),
  });
}

export function useLeaveBalances(year: number, enabled = true) {
  return useQuery({
    queryKey: leaveKeys.balances(year),
    queryFn: () => api.listBalances(year),
    enabled,
  });
}

/**
 * Mọi thay đổi về đơn nghỉ đều làm mới CẢ quỹ phép và CẢ bảng công.
 *
 * Duyệt một đơn trừ quỹ phép và đổi trạng thái những ngày trong đơn thành
 * "nghỉ phép". Chỉ làm mới danh sách đơn sẽ để lại hai màn hình kia nói sai.
 */
function useLeaveMutation<TArgs, TResult>(fn: (a: TArgs) => Promise<TResult>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: leaveKeys.all });
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

export function useCreateLeave() {
  return useLeaveMutation(api.createLeave);
}

export function useDecideLeave() {
  return useLeaveMutation(
    ({ id, approve, note }: { id: string; approve: boolean; note: string }) =>
      api.decideLeave(id, approve, note),
  );
}

export function useCancelLeave() {
  return useLeaveMutation(api.cancelLeave);
}

export function useSetBalance() {
  return useLeaveMutation(api.setBalance);
}

// --------------------------------------------------- khung giờ, ngày lễ

export function useSchedules() {
  return useQuery({
    queryKey: attendanceKeys.schedules(),
    queryFn: api.listSchedules,
    // Khung giờ làm việc gần như không đổi trong một phiên làm việc.
    staleTime: 5 * 60_000,
  });
}

export function useCreateSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createSchedule,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

export function useDeleteSchedule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.deleteSchedule,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

export function useHolidays(year: number) {
  return useQuery({
    queryKey: attendanceKeys.holidays(year),
    queryFn: () => api.listHolidays(year),
    staleTime: 5 * 60_000,
  });
}

export function useCreateHoliday() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createHoliday,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}

export function useDeleteHoliday() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.deleteHoliday,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: attendanceKeys.all });
    },
  });
}
