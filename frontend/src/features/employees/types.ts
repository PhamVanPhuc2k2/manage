export type Employee = {
  id: string;
  employee_code: string;
  full_name: string;
  email: string;
  phone?: string;
  date_of_birth?: string;
  gender?: string;
  address?: string;
  department_id?: string;
  department_name?: string;
  position_id?: string;
  position_name?: string;
  manager_id?: string;
  manager_name?: string;
  work_mode: WorkMode;
  status: EmployeeStatus;
  joined_at: string;
  resigned_at?: string;
  has_account: boolean;
  avatar_url?: string;
  created_at: string;
};

export type WorkMode = "onsite" | "remote" | "hybrid";
export type EmployeeStatus = "probation" | "official" | "resigned";

export const STATUS_LABEL: Record<EmployeeStatus, string> = {
  probation: "Thử việc",
  official: "Chính thức",
  resigned: "Đã nghỉ",
};

export const WORK_MODE_LABEL: Record<WorkMode, string> = {
  onsite: "Tại văn phòng",
  remote: "Từ xa",
  hybrid: "Kết hợp",
};

export type EmployeeFilters = {
  search?: string;
  status?: EmployeeStatus | "";
  departmentId?: string;
  positionId?: string;
  page?: number;
  pageSize?: number;
};
