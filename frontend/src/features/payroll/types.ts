export type PayrollStatus =
  | "draft"
  | "calculating"
  | "locked"
  | "paid"
  | "cancelled";

export type ComponentKind = "allowance" | "bonus" | "deduction";

export type PayrollPeriod = {
  id: string;
  year: number;
  month: number;
  name: string;
  period_start: string;
  period_end: string;
  status: PayrollStatus;
  total_gross: number;
  total_net: number;
  total_tax: number;
  total_insurance: number;
  employee_count: number;
  calculated_at?: string;
  locked_at?: string;
  paid_at?: string;
  created_at: string;
};

export type PayslipItem = {
  kind: ComponentKind;
  code: string;
  name: string;
  amount: number;
  taxable: boolean;
};

export type Payslip = {
  id: string;
  period_id: string;
  period_name: string;
  period_year: number;
  period_month: number;
  employee_id: string;
  employee_name: string;
  employee_code: string;
  department_name?: string;
  position_name?: string;

  standard_workdays: number;
  actual_workdays: number;
  leave_days: number;
  absent_days: number;

  base_salary: number;
  allowances: number;
  bonuses: number;
  gross_salary: number;

  insurance_base: number;
  insurance_employee: number;
  insurance_employer: number;

  taxable_income: number;
  personal_deduction: number;
  dependent_deduction: number;
  assessable_income: number;
  income_tax: number;

  other_deductions: number;
  net_salary: number;
  dependents: number;
  note?: string;

  bank_account?: string;
  bank_name?: string;

  items: PayslipItem[];
};

export type SalaryComponent = {
  kind: ComponentKind;
  code: string;
  name: string;
  amount: number;
  taxable: boolean;
  prorated: boolean;
};

export type SalaryStructure = {
  id: string;
  employee_id: string;
  employee_name?: string;
  employee_code?: string;
  department_name?: string;
  base_salary: number;
  insurance_salary?: number | null;
  dependents: number;
  bank_account?: string;
  bank_name?: string;
  effective_from: string;
  effective_to?: string | null;
  note?: string;
  components: SalaryComponent[];
};

export type TaxBracket = {
  ordinal: number;
  from_amount: number;
  to_amount?: number | null;
  rate: number;
};

export type PayrollSettings = {
  personal_deduction: number;
  dependent_deduction: number;
  social_rate: number;
  health_rate: number;
  unemployment_rate: number;
  employer_social_rate: number;
  employer_health_rate: number;
  employer_unemployment_rate: number;
  social_cap: number;
  unemployment_cap: number;
  standard_workdays: number;
  tax_brackets: TaxBracket[];
};

export type CostRow = {
  key: string;
  label: string;
  employee_count: number;
  total_gross: number;
  total_net: number;
  total_tax: number;
  insurance_employee: number;
  insurance_employer: number;
  total_cost: number;
};

export type AuditEntry = {
  id: string;
  actor_name?: string;
  action: string;
  resource: string;
  detail?: Record<string, unknown>;
  ip?: string;
  created_at: string;
};

export const PAYROLL_STATUS_LABEL: Record<PayrollStatus, string> = {
  draft: "Nháp",
  calculating: "Đang tính",
  locked: "Đã khoá",
  paid: "Đã trả",
  cancelled: "Đã huỷ",
};

export const PAYROLL_STATUS_CLASS: Record<PayrollStatus, string> = {
  draft:
    "bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300",
  calculating:
    "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  locked: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
  paid: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  cancelled: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
};

export const COMPONENT_KIND_LABEL: Record<ComponentKind, string> = {
  allowance: "Phụ cấp",
  bonus: "Thưởng",
  deduction: "Khấu trừ",
};

/**
 * In số tiền theo cách người Việt đọc: 1.234.567 ₫
 *
 * Dùng Intl chứ không tự ghép chuỗi: nó tự xử lý dấu phân cách, số âm và
 * cả trường hợp số rất lớn — ba chỗ dễ sai khi viết tay.
 */
export function formatVND(amount: number): string {
  return new Intl.NumberFormat("vi-VN", {
    style: "currency",
    currency: "VND",
    maximumFractionDigits: 0,
  }).format(amount);
}

/** Rút gọn cho biểu đồ và thẻ số liệu: 1,2 tỷ · 350 tr */
export function formatVNDShort(amount: number): string {
  const abs = Math.abs(amount);
  if (abs >= 1_000_000_000) {
    return `${(amount / 1_000_000_000).toFixed(1).replace(".", ",")} tỷ`;
  }
  if (abs >= 1_000_000) {
    return `${Math.round(amount / 1_000_000)} tr`;
  }
  if (abs >= 1_000) {
    return `${Math.round(amount / 1_000)} k`;
  }
  return String(amount);
}

export function formatPercent(rate: number): string {
  return `${(rate * 100).toFixed(2).replace(/\.?0+$/, "")}%`;
}

export function monthLabel(p: { year: number; month: number }): string {
  return `Tháng ${String(p.month).padStart(2, "0")}/${p.year}`;
}
