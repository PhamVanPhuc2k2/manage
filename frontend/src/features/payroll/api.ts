import { api, type PageMeta } from "@/lib/api-client";
import type {
  AuditEntry,
  CostRow,
  PayrollPeriod,
  PayrollSettings,
  Payslip,
  SalaryStructure,
} from "./types";

// ------------------------------------------------------------ kỳ lương

export async function listPeriods(): Promise<PayrollPeriod[]> {
  const { data } = await api.get<PayrollPeriod[]>("/payroll/periods");
  return data ?? [];
}

export async function getPeriod(id: string): Promise<PayrollPeriod> {
  const { data } = await api.get<PayrollPeriod>(`/payroll/periods/${id}`);
  return data;
}

export async function createPeriod(body: unknown): Promise<PayrollPeriod> {
  const { data } = await api.post<PayrollPeriod>("/payroll/periods", body);
  return data;
}

export async function calculatePeriod(id: string): Promise<void> {
  await api.post(`/payroll/periods/${id}/calculate`);
}

export async function changePeriodStatus(
  id: string,
  status: string,
): Promise<PayrollPeriod> {
  const { data } = await api.put<PayrollPeriod>(
    `/payroll/periods/${id}/status`,
    { status },
  );
  return data;
}

// -------------------------------------------------------- phiếu lương

export async function listPayslips(
  periodId: string,
  page = 1,
): Promise<{ items: Payslip[]; meta?: PageMeta }> {
  const { data, meta } = await api.get<Payslip[]>(
    `/payroll/payslips?period_id=${periodId}&page=${page}`,
  );
  return { items: data ?? [], meta };
}

export async function getMyPayslips(): Promise<Payslip[]> {
  const { data } = await api.get<Payslip[]>("/payroll/payslips/my");
  return data ?? [];
}

export async function getPayslip(id: string): Promise<Payslip> {
  const { data } = await api.get<Payslip>(`/payroll/payslips/${id}`);
  return data;
}

export async function updatePayslip(id: string, body: unknown): Promise<Payslip> {
  const { data } = await api.put<Payslip>(`/payroll/payslips/${id}`, body);
  return data;
}

// ----------------------------------------------------- cấu hình lương

export async function getStructure(employeeId: string): Promise<SalaryStructure> {
  const { data } = await api.get<SalaryStructure>(`/employees/${employeeId}/salary`);
  return data;
}

export async function getStructureHistory(
  employeeId: string,
): Promise<SalaryStructure[]> {
  const { data } = await api.get<SalaryStructure[]>(
    `/employees/${employeeId}/salary/history`,
  );
  return data ?? [];
}

export async function setStructure(
  employeeId: string,
  body: unknown,
): Promise<SalaryStructure> {
  const { data } = await api.put<SalaryStructure>(
    `/employees/${employeeId}/salary`,
    body,
  );
  return data;
}

// ------------------------------------------------- tham số và báo cáo

export async function getSettings(): Promise<PayrollSettings> {
  const { data } = await api.get<PayrollSettings>("/payroll/settings");
  return data;
}

export async function updateSettings(body: unknown): Promise<PayrollSettings> {
  const { data } = await api.put<PayrollSettings>("/payroll/settings", body);
  return data;
}

export async function getCostByDepartment(periodId: string): Promise<CostRow[]> {
  const { data } = await api.get<CostRow[]>(
    `/payroll/periods/${periodId}/cost-by-department`,
  );
  return data ?? [];
}

export async function getCostByMonth(year: number): Promise<CostRow[]> {
  const { data } = await api.get<CostRow[]>(`/payroll/cost-by-month?year=${year}`);
  return data ?? [];
}

export async function listAudit(resource?: string): Promise<AuditEntry[]> {
  const q = resource ? `?resource=${resource}` : "";
  const { data } = await api.get<AuditEntry[]>(`/payroll/audit${q}`);
  return data ?? [];
}
