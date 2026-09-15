import { api } from "@/lib/api-client";
import type { Department } from "./types";

export async function listDepartments(): Promise<Department[]> {
  const { data } = await api.get<Department[]>("/departments");
  return data ?? [];
}

export async function getDepartmentTree(): Promise<Department[]> {
  const { data } = await api.get<Department[]>("/departments/tree");
  return data ?? [];
}

export async function createDepartment(body: unknown): Promise<Department> {
  const { data } = await api.post<Department>("/departments", body);
  return data;
}

export async function updateDepartment(id: string, body: unknown): Promise<Department> {
  const { data } = await api.put<Department>(`/departments/${id}`, body);
  return data;
}

export async function deleteDepartment(id: string): Promise<void> {
  await api.delete(`/departments/${id}`);
}
