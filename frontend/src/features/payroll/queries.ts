import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import * as api from "./api";
import type { PayrollPeriod } from "./types";

export const payrollKeys = {
  all: ["payroll"] as const,
  periods: () => [...payrollKeys.all, "periods"] as const,
  period: (id: string) => [...payrollKeys.all, "period", id] as const,
  payslips: (periodId: string, page: number) =>
    [...payrollKeys.all, "payslips", periodId, page] as const,
  myPayslips: () => [...payrollKeys.all, "my-payslips"] as const,
  payslip: (id: string) => [...payrollKeys.all, "payslip", id] as const,
  settings: () => [...payrollKeys.all, "settings"] as const,
  costByDept: (id: string) => [...payrollKeys.all, "cost-dept", id] as const,
  costByMonth: (y: number) => [...payrollKeys.all, "cost-month", y] as const,
  audit: (r?: string) => [...payrollKeys.all, "audit", r ?? "all"] as const,
  structure: (id: string) => [...payrollKeys.all, "structure", id] as const,
  structureHistory: (id: string) =>
    [...payrollKeys.all, "structure-history", id] as const,
};

/**
 * Kỳ lương đang tính thì tự làm mới mỗi 2 giây.
 *
 * Máy tính lương chạy ở worker và có thể mất vài chục giây. Không tự làm
 * mới thì người dùng phải tải lại trang để biết đã xong — và họ sẽ bấm
 * "tính lương" lần nữa vì tưởng chưa chạy.
 */
export function usePeriods() {
  return useQuery({
    queryKey: payrollKeys.periods(),
    queryFn: api.listPeriods,
    refetchInterval: (query) => {
      const data = query.state.data as PayrollPeriod[] | undefined;
      return data?.some((p) => p.status === "calculating") ? 2000 : false;
    },
  });
}

export function usePeriod(id: string | undefined) {
  return useQuery({
    queryKey: payrollKeys.period(id ?? ""),
    queryFn: () => api.getPeriod(id!),
    enabled: Boolean(id),
    refetchInterval: (query) => {
      const data = query.state.data as PayrollPeriod | undefined;
      return data?.status === "calculating" ? 2000 : false;
    },
  });
}

export function useCreatePeriod() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.createPeriod,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.all });
    },
  });
}

export function useCalculatePeriod() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.calculatePeriod,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.all });
    },
  });
}

export function useChangePeriodStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: string }) =>
      api.changePeriodStatus(id, status),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.all });
    },
  });
}

export function usePayslips(periodId: string | undefined, page = 1) {
  return useQuery({
    queryKey: payrollKeys.payslips(periodId ?? "", page),
    queryFn: () => api.listPayslips(periodId!, page),
    enabled: Boolean(periodId),
    placeholderData: keepPreviousData,
  });
}

export function useMyPayslips() {
  return useQuery({
    queryKey: payrollKeys.myPayslips(),
    queryFn: api.getMyPayslips,
  });
}

export function usePayslip(id: string | undefined) {
  return useQuery({
    queryKey: payrollKeys.payslip(id ?? ""),
    queryFn: () => api.getPayslip(id!),
    enabled: Boolean(id),
  });
}

export function useUpdatePayslip() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: unknown }) =>
      api.updatePayslip(id, body),
    // Sửa một phiếu làm đổi cả số tổng của kỳ, nên làm mới tất cả.
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.all });
    },
  });
}

export function useSettings() {
  return useQuery({
    queryKey: payrollKeys.settings(),
    queryFn: api.getSettings,
    staleTime: 5 * 60_000,
  });
}

export function useUpdateSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.updateSettings,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.settings() });
    },
  });
}

export function useStructure(employeeId: string | undefined) {
  return useQuery({
    queryKey: payrollKeys.structure(employeeId ?? ""),
    queryFn: () => api.getStructure(employeeId!),
    enabled: Boolean(employeeId),
    // Chưa cấu hình lương thì API trả 404 — đó là câu trả lời hợp lệ, không
    // phải lỗi tạm thời, nên đừng thử lại.
    retry: false,
  });
}

export function useStructureHistory(employeeId: string | undefined) {
  return useQuery({
    queryKey: payrollKeys.structureHistory(employeeId ?? ""),
    queryFn: () => api.getStructureHistory(employeeId!),
    enabled: Boolean(employeeId),
    retry: false,
  });
}

export function useSetStructure(employeeId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) => api.setStructure(employeeId, body),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: payrollKeys.all });
    },
  });
}

export function useCostByDepartment(periodId: string | undefined) {
  return useQuery({
    queryKey: payrollKeys.costByDept(periodId ?? ""),
    queryFn: () => api.getCostByDepartment(periodId!),
    enabled: Boolean(periodId),
  });
}

export function useCostByMonth(year: number) {
  return useQuery({
    queryKey: payrollKeys.costByMonth(year),
    queryFn: () => api.getCostByMonth(year),
  });
}

export function useAudit(resource?: string, enabled = true) {
  return useQuery({
    queryKey: payrollKeys.audit(resource),
    queryFn: () => api.listAudit(resource),
    enabled,
  });
}
