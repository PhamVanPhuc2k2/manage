"use client";

import { use, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { ProjectForm } from "@/features/projects/ProjectForm";
import {
  useAddMember,
  useDeleteProject,
  useProject,
  useProjectMembers,
  useProjectProgress,
  useRemoveMember,
  useUpdateMemberRole,
} from "@/features/projects/queries";
import {
  PROJECT_ROLE_LABEL,
  PROJECT_STATUS_CLASS,
  PROJECT_STATUS_LABEL,
  type ProjectRole,
} from "@/features/projects/types";
import { useEmployees } from "@/features/employees/queries";
import { useTasks } from "@/features/tasks/queries";
import {
  TASK_STATUS_LABEL,
  formatMinutes,
  type TaskStatus,
} from "@/features/tasks/types";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth/store";
import { usePermission } from "@/lib/auth/useAuth";
import { ProjectTimeline } from "./ProjectTimeline";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

export default function ProjectDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { can } = usePermission();
  const myEmployeeId = useAuthStore((s) => s.user?.employee_id);
  const router = useRouter();

  const { data: project, isPending, error } = useProject(id);
  const { data: progress } = useProjectProgress(id);
  const { data: members = [] } = useProjectMembers(id);
  // 10 việc mới nhất. Bảng Kanban là nơi xem đầy đủ, ở đây chỉ cần ảnh chụp
  // nhanh để biết dự án đang động đậy chỗ nào.
  const { data: recent } = useTasks({ project_id: id, page: 1 }, Boolean(id));

  const remove = useDeleteProject();
  const [editing, setEditing] = useState(false);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải...</p>
      </AppShell>
    );
  }
  if (error || !project) {
    return (
      <AppShell>
        <FormError message={errMsg(error, "Không tải được dự án")} />
        <Link href="/projects" className="mt-3 inline-block text-sm underline">
          Về danh sách dự án
        </Link>
      </AppShell>
    );
  }

  // Giám đốc xem được mọi dự án nhưng không phải thành viên nào — khi đó
  // viewer_role rỗng. Quyền sửa thật vẫn do backend quyết định.
  const canManage =
    project.viewer_role === "owner" || project.owner_id === myEmployeeId;

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="mb-1 flex items-center gap-3">
            <span className="font-mono text-sm text-neutral-500">{project.code}</span>
            <span
              className={`rounded px-2 py-0.5 text-xs ${PROJECT_STATUS_CLASS[project.status]}`}
            >
              {PROJECT_STATUS_LABEL[project.status]}
            </span>
            {project.viewer_role && (
              <span className="text-xs text-neutral-500">
                Bạn là {PROJECT_ROLE_LABEL[project.viewer_role].toLowerCase()}
              </span>
            )}
          </div>
          <h1 className="text-xl font-semibold">{project.name}</h1>
          {project.description && (
            <p className="mt-1 text-sm text-neutral-500">{project.description}</p>
          )}
        </div>

        <div className="flex shrink-0 gap-2">
          <Link
            href={`/projects/${id}/board`}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Bảng Kanban
          </Link>
          {can("project:update") && canManage && (
            <button
              onClick={() => setEditing(true)}
              className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
            >
              Sửa
            </button>
          )}
        </div>
      </div>

      <FormError message={errMsg(remove.error, "Không xoá được dự án")} />

      {/* --- Số liệu tổng quan --- */}
      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Tiến độ" value={`${project.progress}%`} />
        <Stat
          label="Công việc"
          value={`${project.done_task_count}/${project.task_count}`}
        />
        <Stat
          label="Quá hạn"
          value={String(progress?.overdue_tasks ?? 0)}
          danger={Boolean(progress?.overdue_tasks)}
        />
        <Stat
          label="Đã ghi nhận"
          value={formatMinutes(progress?.spent_minutes ?? 0)}
          hint={
            progress?.estimate_hours
              ? `ước lượng ${progress.estimate_hours}h`
              : undefined
          }
        />
      </div>

      {/* --- Phân bố theo cột --- */}
      {progress && progress.total_tasks > 0 && (
        <div className="mb-6 rounded border border-neutral-200 p-4 dark:border-neutral-800">
          <h2 className="mb-3 text-sm font-medium">Phân bố công việc</h2>
          <div className="flex h-3 overflow-hidden rounded-full">
            {(Object.keys(TASK_STATUS_LABEL) as TaskStatus[]).map((s) => {
              const n = progress.by_status[s] ?? 0;
              if (!n) return null;
              const pct = (n / progress.total_tasks) * 100;
              return (
                <div
                  key={s}
                  title={`${TASK_STATUS_LABEL[s]}: ${n}`}
                  style={{ width: `${pct}%` }}
                  className={
                    s === "done"
                      ? "bg-green-500"
                      : s === "review"
                        ? "bg-amber-500"
                        : s === "in_progress"
                          ? "bg-blue-500"
                          : "bg-neutral-400"
                  }
                />
              );
            })}
          </div>
          <div className="mt-2 flex flex-wrap gap-4 text-xs text-neutral-500">
            {(Object.keys(TASK_STATUS_LABEL) as TaskStatus[]).map((s) => (
              <span key={s}>
                {TASK_STATUS_LABEL[s]}: {progress.by_status[s] ?? 0}
              </span>
            ))}
          </div>
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        {/* --- Timeline --- */}
        <div className="lg:col-span-2">
          <ProjectTimeline projectId={id} project={project} />

          <h2 className="mb-3 mt-6 text-sm font-medium">Công việc gần đây</h2>
          <div className="rounded border border-neutral-200 dark:border-neutral-800">
            {(recent?.items ?? []).slice(0, 10).map((t) => (
              <div
                key={t.id}
                className="flex items-center justify-between gap-3 border-b border-neutral-200 px-4 py-2 text-sm last:border-b-0 dark:border-neutral-800"
              >
                <div className="min-w-0">
                  <span className="mr-2 font-mono text-xs text-neutral-500">
                    {t.code}
                  </span>
                  <span className="truncate">{t.title}</span>
                </div>
                <span className="shrink-0 text-xs text-neutral-500">
                  {TASK_STATUS_LABEL[t.status]}
                </span>
              </div>
            ))}
            {(recent?.items ?? []).length === 0 && (
              <p className="px-4 py-6 text-center text-sm text-neutral-400">
                Chưa có công việc nào.
              </p>
            )}
          </div>
        </div>

        {/* --- Thành viên --- */}
        <MembersPanel
          projectId={id}
          canManage={can("project:update") && canManage}
          members={members}
          myEmployeeId={myEmployeeId}
        />
      </div>

      {/* --- Xoá dự án --- */}
      {can("project:delete") && canManage && (
        <div className="mt-8 rounded border border-red-200 p-4 dark:border-red-900">
          <h2 className="mb-1 text-sm font-medium text-red-700 dark:text-red-400">
            Xoá dự án
          </h2>
          <p className="mb-3 text-xs text-neutral-500">
            Dự án còn việc chưa xong thì phải chuyển sang tạm dừng hoặc huỷ trước.
          </p>
          <button
            onClick={() => {
              if (confirm(`Xoá dự án "${project.name}"?`)) {
                // router.push chứ không phải window.location: giữ nguyên
                // cache TanStack Query và không tải lại cả ứng dụng.
                remove.mutate(id, {
                  onSuccess: () => router.push("/projects"),
                });
              }
            }}
            disabled={remove.isPending}
            className="rounded border border-red-300 px-3 py-1.5 text-sm text-red-700 disabled:opacity-50 dark:border-red-800 dark:text-red-400"
          >
            {remove.isPending ? "Đang xoá..." : "Xoá dự án"}
          </button>
        </div>
      )}

      {editing && (
        <ProjectForm project={project} onClose={() => setEditing(false)} />
      )}
    </AppShell>
  );
}

function Stat({
  label,
  value,
  hint,
  danger,
}: {
  label: string;
  value: string;
  hint?: string;
  danger?: boolean;
}) {
  return (
    <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="text-xs text-neutral-500">{label}</div>
      <div
        className={`mt-1 text-2xl font-semibold ${danger ? "text-red-600 dark:text-red-400" : ""}`}
      >
        {value}
      </div>
      {hint && <div className="mt-0.5 text-xs text-neutral-500">{hint}</div>}
    </div>
  );
}

function MembersPanel({
  projectId,
  canManage,
  members,
  myEmployeeId,
}: {
  projectId: string;
  canManage: boolean;
  members: { employee_id: string; employee_name: string; role: ProjectRole; position_name?: string }[];
  myEmployeeId?: string;
}) {
  const add = useAddMember(projectId);
  const updateRole = useUpdateMemberRole(projectId);
  const removeMember = useRemoveMember(projectId);

  const [picking, setPicking] = useState(false);
  const { data: employeeData } = useEmployees({ page: 1, pageSize: 100 });

  const inProject = new Set(members.map((m) => m.employee_id));
  const candidates = (employeeData?.items ?? []).filter((e) => !inProject.has(e.id));

  const mutationError =
    errMsg(add.error, "Không thêm được thành viên") ??
    errMsg(updateRole.error, "Không đổi được vai trò") ??
    errMsg(removeMember.error, "Không gỡ được thành viên");

  return (
    <div>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium">Thành viên ({members.length})</h2>
        {canManage && (
          <button
            onClick={() => setPicking((v) => !v)}
            className="text-xs underline hover:no-underline"
          >
            {picking ? "Đóng" : "Thêm"}
          </button>
        )}
      </div>

      <FormError message={mutationError} />

      {picking && (
        <select
          defaultValue=""
          onChange={(e) => {
            if (!e.target.value) return;
            add.mutate(
              { employeeId: e.target.value, role: "member" },
              { onSuccess: () => setPicking(false) },
            );
          }}
          className="mb-3 w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">— Chọn nhân viên —</option>
          {candidates.map((e) => (
            <option key={e.id} value={e.id}>
              {e.full_name}
            </option>
          ))}
        </select>
      )}

      <div className="rounded border border-neutral-200 dark:border-neutral-800">
        {members.map((m) => (
          <div
            key={m.employee_id}
            className="flex items-center justify-between gap-2 border-b border-neutral-200 px-3 py-2 text-sm last:border-b-0 dark:border-neutral-800"
          >
            <div className="min-w-0">
              <div className="truncate">{m.employee_name}</div>
              {m.position_name && (
                <div className="text-xs text-neutral-500">{m.position_name}</div>
              )}
            </div>

            <div className="flex shrink-0 items-center gap-2">
              {canManage ? (
                <select
                  value={m.role}
                  onChange={(e) =>
                    updateRole.mutate({
                      employeeId: m.employee_id,
                      role: e.target.value as ProjectRole,
                    })
                  }
                  className="rounded border border-neutral-300 px-1 py-0.5 text-xs dark:border-neutral-700 dark:bg-neutral-950"
                >
                  {Object.entries(PROJECT_ROLE_LABEL).map(([v, label]) => (
                    <option key={v} value={v}>
                      {label}
                    </option>
                  ))}
                </select>
              ) : (
                <span className="text-xs text-neutral-500">
                  {PROJECT_ROLE_LABEL[m.role]}
                </span>
              )}

              {/* Ai cũng tự rời dự án được; gỡ người khác thì phải là chủ. */}
              {(canManage || m.employee_id === myEmployeeId) && (
                <button
                  onClick={() => {
                    const self = m.employee_id === myEmployeeId;
                    if (confirm(self ? "Rời dự án này?" : `Gỡ ${m.employee_name}?`)) {
                      removeMember.mutate(m.employee_id);
                    }
                  }}
                  className="text-xs text-neutral-400 underline hover:text-red-600"
                >
                  {m.employee_id === myEmployeeId ? "Rời" : "Gỡ"}
                </button>
              )}
            </div>
          </div>
        ))}

        {members.length === 0 && (
          <p className="px-3 py-6 text-center text-sm text-neutral-400">
            Chưa có thành viên.
          </p>
        )}
      </div>
    </div>
  );
}
