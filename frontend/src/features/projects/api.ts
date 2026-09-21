import { api, type PageMeta } from "@/lib/api-client";
import type {
  Project,
  ProjectFilters,
  ProjectMember,
  ProjectProgress,
  ProjectRole,
  WorkloadRow,
} from "./types";

function buildQuery(f: ProjectFilters): string {
  const p = new URLSearchParams();
  if (f.search) p.set("search", f.search);
  if (f.status) p.set("status", f.status);
  p.set("page", String(f.page ?? 1));
  p.set("sort_by", "created_at");
  p.set("sort_order", "desc");
  return p.toString();
}

export async function listProjects(
  f: ProjectFilters,
): Promise<{ items: Project[]; meta?: PageMeta }> {
  const { data, meta } = await api.get<Project[]>(`/projects?${buildQuery(f)}`);
  return { items: data ?? [], meta };
}

export async function getProject(id: string): Promise<Project> {
  const { data } = await api.get<Project>(`/projects/${id}`);
  return data;
}

export async function createProject(body: unknown): Promise<Project> {
  const { data } = await api.post<Project>("/projects", body);
  return data;
}

export async function updateProject(id: string, body: unknown): Promise<Project> {
  const { data } = await api.put<Project>(`/projects/${id}`, body);
  return data;
}

export async function deleteProject(id: string): Promise<void> {
  await api.delete(`/projects/${id}`);
}

export async function getProjectProgress(id: string): Promise<ProjectProgress> {
  const { data } = await api.get<ProjectProgress>(`/projects/${id}/progress`);
  return data;
}

// ------------------------------------------------------------ thành viên

export async function listMembers(projectId: string): Promise<ProjectMember[]> {
  const { data } = await api.get<ProjectMember[]>(`/projects/${projectId}/members`);
  return data ?? [];
}

export async function addMember(
  projectId: string,
  employeeId: string,
  role: ProjectRole,
): Promise<ProjectMember> {
  const { data } = await api.post<ProjectMember>(`/projects/${projectId}/members`, {
    employee_id: employeeId,
    role,
  });
  return data;
}

export async function updateMemberRole(
  projectId: string,
  employeeId: string,
  role: ProjectRole,
): Promise<ProjectMember> {
  const { data } = await api.put<ProjectMember>(
    `/projects/${projectId}/members/${employeeId}`,
    { employee_id: employeeId, role },
  );
  return data;
}

export async function removeMember(
  projectId: string,
  employeeId: string,
): Promise<void> {
  await api.delete(`/projects/${projectId}/members/${employeeId}`);
}

// --------------------------------------------------------------- báo cáo

export async function getWorkload(projectId?: string): Promise<WorkloadRow[]> {
  const q = projectId ? `?project_id=${projectId}` : "";
  const { data } = await api.get<WorkloadRow[]>(`/reports/workload${q}`);
  return data ?? [];
}
