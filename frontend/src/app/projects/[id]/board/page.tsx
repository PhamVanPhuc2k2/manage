"use client";

import { use, useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { useProjectMembers } from "@/features/projects/queries";
import { KanbanBoard } from "@/features/tasks/KanbanBoard";
import { TaskDetailPanel } from "@/features/tasks/TaskDetailPanel";
import { TaskForm } from "@/features/tasks/TaskForm";
import { useBoard } from "@/features/tasks/queries";
import type { Task, TaskStatus } from "@/features/tasks/types";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth/store";
import { usePermission } from "@/lib/auth/useAuth";

export default function BoardPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { can } = usePermission();
  const myEmployeeId = useAuthStore((s) => s.user?.employee_id);

  const { data: board, isPending, error } = useBoard(id);
  const { data: members = [] } = useProjectMembers(id);

  const [openTaskId, setOpenTaskId] = useState<string | null>(null);
  const [creating, setCreating] = useState<{
    status?: TaskStatus;
    parent?: Task;
  } | null>(null);
  const [moveError, setMoveError] = useState<string | null>(null);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải bảng...</p>
      </AppShell>
    );
  }
  if (error || !board) {
    return (
      <AppShell>
        <FormError
          message={
            error instanceof ApiError ? error.message : "Không tải được bảng Kanban"
          }
        />
        <Link href="/projects" className="mt-3 inline-block text-sm underline">
          Về danh sách dự án
        </Link>
      </AppShell>
    );
  }

  const project = board.project;

  // Người chỉ có vai trò "viewer" thì không tạo/sửa việc được. Giám đốc xem
  // toàn công ty có viewer_role rỗng — backend vẫn là nơi quyết định thật.
  const canWrite =
    can("task:update") &&
    project.viewer_role !== "viewer" &&
    (Boolean(project.viewer_role) || project.owner_id === myEmployeeId);

  return (
    <AppShell>
      <div className="mb-4 flex items-center justify-between gap-4">
        <div className="min-w-0">
          <Link
            href={`/projects/${id}`}
            className="text-xs text-neutral-500 hover:underline"
          >
            ← {project.code} · {project.name}
          </Link>
          <h1 className="text-xl font-semibold">Bảng Kanban</h1>
        </div>

        {canWrite && can("task:create") && (
          <button
            onClick={() => setCreating({ status: "todo" })}
            className="shrink-0 rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Thêm công việc
          </button>
        )}
      </div>

      <FormError message={moveError} />

      <div className="flex gap-0">
        <div className="min-w-0 flex-1">
          <KanbanBoard
            board={board}
            canWrite={canWrite && can("task:create")}
            onOpenTask={setOpenTaskId}
            onAddTask={(status) => setCreating({ status })}
            onError={setMoveError}
          />
        </div>

        {openTaskId && (
          <TaskDetailPanel
            taskId={openTaskId}
            members={members}
            canWrite={canWrite}
            onClose={() => setOpenTaskId(null)}
            onAddSubtask={(parent) => setCreating({ parent })}
          />
        )}
      </div>

      {creating && (
        <TaskForm
          projectId={id}
          defaultStatus={creating.status}
          parentTask={creating.parent}
          members={members}
          onClose={() => setCreating(null)}
        />
      )}
    </AppShell>
  );
}
