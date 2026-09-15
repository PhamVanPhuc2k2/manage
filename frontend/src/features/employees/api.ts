import { api, type PageMeta } from "@/lib/api-client";
import type { Employee, EmployeeFilters } from "./types";

/**
 * Dựng query string, bỏ qua mọi giá trị rỗng.
 *
 * Gửi `status=` rỗng lên backend sẽ bị từ chối với lỗi "status không hợp lệ",
 * nên phải lọc ở đây chứ không đẩy trách nhiệm sang nơi gọi.
 */
function buildParams(f: EmployeeFilters): string {
  const p = new URLSearchParams();
  p.set("page", String(f.page ?? 1));
  p.set("page_size", String(f.pageSize ?? 20));

  if (f.search?.trim()) p.set("search", f.search.trim());
  if (f.status) p.set("status", f.status);
  if (f.departmentId) p.set("department_id", f.departmentId);
  if (f.positionId) p.set("position_id", f.positionId);

  return p.toString();
}

export async function listEmployees(
  filters: EmployeeFilters,
): Promise<{ items: Employee[]; meta?: PageMeta }> {
  const res = await api.get<Employee[]>(`/employees?${buildParams(filters)}`);
  return { items: res.data ?? [], meta: res.meta };
}

export async function getEmployee(id: string): Promise<Employee> {
  const { data } = await api.get<Employee>(`/employees/${id}`);
  return data;
}

export async function createEmployee(body: unknown): Promise<Employee> {
  const { data } = await api.post<Employee>("/employees", body);
  return data;
}

export async function updateEmployee(id: string, body: unknown): Promise<Employee> {
  const { data } = await api.put<Employee>(`/employees/${id}`, body);
  return data;
}

export async function deactivateEmployee(id: string): Promise<void> {
  await api.delete(`/employees/${id}`);
}
