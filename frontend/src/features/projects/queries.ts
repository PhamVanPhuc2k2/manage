import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import {
  addMember,
  createProject,
  deleteProject,
  getProject,
  getProjectProgress,
  getWorkload,
  listMembers,
  listProjects,
  removeMember,
  updateMemberRole,
  updateProject,
} from "./api";
import type { ProjectFilters, ProjectRole } from "./types";

export const projectKeys = {
  all: ["projects"] as const,
  lists: () => [...projectKeys.all, "list"] as const,
  list: (f: ProjectFilters) => [...projectKeys.lists(), f] as const,
  details: () => [...projectKeys.all, "detail"] as const,
  detail: (id: string) => [...projectKeys.details(), id] as const,
  members: (id: string) => [...projectKeys.detail(id), "members"] as const,
  progress: (id: string) => [...projectKeys.detail(id), "progress"] as const,
  workload: (id?: string) => [...projectKeys.all, "workload", id ?? "all"] as const,
};

export function useProjects(filters: ProjectFilters) {
  return useQuery({
    queryKey: projectKeys.list(filters),
    queryFn: () => listProjects(filters),
    placeholderData: keepPreviousData,
  });
}

export function useProject(id: string | undefined) {
  return useQuery({
    queryKey: projectKeys.detail(id ?? ""),
    queryFn: () => getProject(id!),
    enabled: Boolean(id),
  });
}

export function useProjectProgress(id: string | undefined) {
  return useQuery({
    queryKey: projectKeys.progress(id ?? ""),
    queryFn: () => getProjectProgress(id!),
    enabled: Boolean(id),
  });
}

export function useCreateProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: createProject,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

export function useUpdateProject(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) => updateProject(id, body),
    onSuccess: (updated) => {
      qc.setQueryData(projectKeys.detail(id), updated);
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

export function useDeleteProject() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deleteProject,
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: projectKeys.detail(id) });
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

// ------------------------------------------------------------ thành viên

export function useProjectMembers(projectId: string | undefined) {
  return useQuery({
    queryKey: projectKeys.members(projectId ?? ""),
    queryFn: () => listMembers(projectId!),
    enabled: Boolean(projectId),
  });
}

/**
 * Mọi thao tác trên thành viên đều làm mới cả `detail`, không riêng `members`.
 *
 * Lý do: `member_count` nằm trong bản ghi dự án. Chỉ làm mới danh sách thành
 * viên sẽ để lại con số đếm cũ trên thẻ dự án — hai màn hình nói hai điều
 * khác nhau về cùng một dữ liệu.
 */
function useMemberMutation<TArgs, TResult>(
  projectId: string,
  fn: (args: TArgs) => Promise<TResult>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: projectKeys.detail(projectId) });
      void qc.invalidateQueries({ queryKey: projectKeys.lists() });
    },
  });
}

export function useAddMember(projectId: string) {
  return useMemberMutation(
    projectId,
    ({ employeeId, role }: { employeeId: string; role: ProjectRole }) =>
      addMember(projectId, employeeId, role),
  );
}

export function useUpdateMemberRole(projectId: string) {
  return useMemberMutation(
    projectId,
    ({ employeeId, role }: { employeeId: string; role: ProjectRole }) =>
      updateMemberRole(projectId, employeeId, role),
  );
}

export function useRemoveMember(projectId: string) {
  return useMemberMutation(projectId, (employeeId: string) =>
    removeMember(projectId, employeeId),
  );
}

export function useWorkload(projectId?: string) {
  return useQuery({
    queryKey: projectKeys.workload(projectId),
    queryFn: () => getWorkload(projectId),
  });
}
