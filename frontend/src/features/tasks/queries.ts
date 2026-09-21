import {
  keepPreviousData,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";

import { projectKeys } from "../projects/queries";
import {
  confirmAttachment,
  createComment,
  createTask,
  deleteAttachment,
  deleteComment,
  deleteTask,
  deleteTimelog,
  getBoard,
  getTask,
  listActivities,
  listAttachments,
  listComments,
  listMyTasks,
  listOverdueTasks,
  listSubtasks,
  listTasks,
  listTimelogs,
  logTime,
  moveTask,
  requestAttachmentUpload,
  updateComment,
  updateTask,
} from "./api";
import type { Board, Task, TaskFilters, TaskStatus } from "./types";

export const taskKeys = {
  all: ["tasks"] as const,
  lists: () => [...taskKeys.all, "list"] as const,
  list: (f: TaskFilters) => [...taskKeys.lists(), f] as const,
  my: (f: TaskFilters) => [...taskKeys.all, "my", f] as const,
  overdue: (id?: string) => [...taskKeys.all, "overdue", id ?? "all"] as const,
  board: (projectId: string) => [...taskKeys.all, "board", projectId] as const,
  details: () => [...taskKeys.all, "detail"] as const,
  detail: (id: string) => [...taskKeys.details(), id] as const,
  comments: (id: string) => [...taskKeys.detail(id), "comments"] as const,
  attachments: (id: string) => [...taskKeys.detail(id), "attachments"] as const,
  activities: (id: string) => [...taskKeys.detail(id), "activities"] as const,
  timelogs: (id: string) => [...taskKeys.detail(id), "timelogs"] as const,
  subtasks: (id: string) => [...taskKeys.detail(id), "subtasks"] as const,
};

export function useTasks(filters: TaskFilters, enabled = true) {
  return useQuery({
    queryKey: taskKeys.list(filters),
    queryFn: () => listTasks(filters),
    placeholderData: keepPreviousData,
    enabled,
  });
}

export function useMyTasks(filters: TaskFilters) {
  return useQuery({
    queryKey: taskKeys.my(filters),
    queryFn: () => listMyTasks(filters),
    placeholderData: keepPreviousData,
  });
}

export function useOverdueTasks(projectId?: string) {
  return useQuery({
    queryKey: taskKeys.overdue(projectId),
    queryFn: () => listOverdueTasks(projectId),
  });
}

export function useBoard(projectId: string | undefined) {
  return useQuery({
    queryKey: taskKeys.board(projectId ?? ""),
    queryFn: () => getBoard(projectId!),
    enabled: Boolean(projectId),
  });
}

export function useTask(id: string | undefined) {
  return useQuery({
    queryKey: taskKeys.detail(id ?? ""),
    queryFn: () => getTask(id!),
    enabled: Boolean(id),
  });
}

export function useSubtasks(id: string | undefined) {
  return useQuery({
    queryKey: taskKeys.subtasks(id ?? ""),
    queryFn: () => listSubtasks(id!),
    enabled: Boolean(id),
  });
}

/**
 * Làm mới mọi thứ bị ảnh hưởng khi một công việc thay đổi.
 *
 * Một thay đổi trên task đụng tới nhiều màn hình: bảng Kanban, danh sách
 * việc, "việc của tôi", và cả số đếm task trên thẻ dự án. Gom vào một hàm để
 * không phải nhớ đủ bốn chỗ ở mỗi mutation — quên một chỗ là giao diện hiện
 * hai con số mâu thuẫn.
 */
function invalidateTaskViews(
  qc: ReturnType<typeof useQueryClient>,
  projectId?: string,
) {
  void qc.invalidateQueries({ queryKey: taskKeys.lists() });
  void qc.invalidateQueries({ queryKey: [...taskKeys.all, "my"] });
  void qc.invalidateQueries({ queryKey: [...taskKeys.all, "overdue"] });
  if (projectId) {
    void qc.invalidateQueries({ queryKey: taskKeys.board(projectId) });
    void qc.invalidateQueries({ queryKey: projectKeys.detail(projectId) });
  }
  void qc.invalidateQueries({ queryKey: projectKeys.lists() });
}

export function useCreateTask(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: createTask,
    onSuccess: (t) => invalidateTaskViews(qc, projectId ?? t.project_id),
  });
}

export function useUpdateTask(id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: unknown) => updateTask(id, body),
    onSuccess: (updated) => {
      qc.setQueryData(taskKeys.detail(id), updated);
      invalidateTaskViews(qc, updated.project_id);
    },
  });
}

export function useDeleteTask(projectId?: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deleteTask,
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: taskKeys.detail(id) });
      invalidateTaskViews(qc, projectId);
    },
  });
}

/**
 * Kéo-thả trên Kanban, có cập nhật lạc quan.
 *
 * Lạc quan là bắt buộc ở đây, không phải để đẹp: người dùng vừa thả chuột thì
 * thẻ phải nằm yên ở chỗ mới ngay. Chờ một vòng mạng rồi mới vẽ lại sẽ khiến
 * thẻ nhảy ngược về chỗ cũ một nhịp — cảm giác như thao tác bị hỏng.
 *
 * Khi API trả lỗi (ví dụ bước chuyển trạng thái không hợp lệ), onError trả
 * nguyên ảnh chụp bảng trước đó về, nên thẻ tự quay lại chỗ cũ.
 */
export function useMoveTask(projectId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: ({
      taskId,
      status,
      afterTaskId,
    }: {
      taskId: string;
      status: TaskStatus;
      afterTaskId: string | null;
    }) => moveTask(taskId, status, afterTaskId),

    onMutate: async ({ taskId, status, afterTaskId }) => {
      const key = taskKeys.board(projectId);

      // Huỷ mọi lần tải đang chạy: một response cũ về sau khi ta đã vẽ lạc
      // quan sẽ ghi đè và làm thẻ nhảy về chỗ cũ.
      await qc.cancelQueries({ queryKey: key });

      const previous = qc.getQueryData<Board>(key);
      if (!previous) return { previous };

      const moving = previous.columns
        .flatMap((c) => c.tasks)
        .find((t) => t.id === taskId);
      if (!moving) return { previous };

      const next: Board = {
        ...previous,
        columns: previous.columns.map((col) => {
          // Gỡ khỏi mọi cột trước, kể cả cột đích — kéo trong cùng một cột
          // thì task vừa bị gỡ vừa được chèn lại.
          const without = col.tasks.filter((t) => t.id !== taskId);
          if (col.status !== status) return { ...col, tasks: without };

          const moved: Task = { ...moving, status };
          if (afterTaskId === null) return { ...col, tasks: [moved, ...without] };

          const at = without.findIndex((t) => t.id === afterTaskId);
          if (at < 0) return { ...col, tasks: [...without, moved] };
          return {
            ...col,
            tasks: [...without.slice(0, at + 1), moved, ...without.slice(at + 1)],
          };
        }),
      };

      qc.setQueryData(key, next);
      return { previous };
    },

    onError: (_err, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(taskKeys.board(projectId), ctx.previous);
      }
    },

    // Chạy cả khi thành công lẫn thất bại: chỉ bản từ server mới có sort_order
    // thật và các số đếm đã cập nhật.
    onSettled: () => invalidateTaskViews(qc, projectId),
  });
}

// ------------------------------------------------------------ bình luận

export function useComments(taskId: string | undefined) {
  return useQuery({
    queryKey: taskKeys.comments(taskId ?? ""),
    queryFn: () => listComments(taskId!),
    enabled: Boolean(taskId),
  });
}

function useCommentMutation<TArgs, TResult>(
  taskId: string,
  fn: (args: TArgs) => Promise<TResult>,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: taskKeys.comments(taskId) });
      // comment_count nằm trong bản ghi task, nên chi tiết và bảng cũng cũ.
      void qc.invalidateQueries({ queryKey: taskKeys.detail(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.activities(taskId) });
      void qc.invalidateQueries({ queryKey: [...taskKeys.all, "board"] });
    },
  });
}

export function useCreateComment(taskId: string) {
  return useCommentMutation(taskId, (content: string) =>
    createComment(taskId, content),
  );
}

export function useUpdateComment(taskId: string) {
  return useCommentMutation(
    taskId,
    ({ commentId, content }: { commentId: string; content: string }) =>
      updateComment(commentId, content),
  );
}

export function useDeleteComment(taskId: string) {
  return useCommentMutation(taskId, (commentId: string) =>
    deleteComment(commentId),
  );
}

// -------------------------------------------------------- tệp đính kèm

export function useAttachments(taskId: string | undefined) {
  return useQuery({
    queryKey: taskKeys.attachments(taskId ?? ""),
    queryFn: () => listAttachments(taskId!),
    enabled: Boolean(taskId),
  });
}

/**
 * Tải tệp đính kèm — ba bước trong một mutation.
 *
 * Gộp lại để nơi gọi chỉ cần truyền File, không phải tự điều phối ba lời gọi
 * và tự xử lý trường hợp bước giữa lỗi. Giống hệt luồng ảnh đại diện.
 */
export function useUploadAttachment(taskId: string) {
  const qc = useQueryClient();

  return useMutation({
    mutationFn: async (file: File) => {
      const type = file.type || "application/octet-stream";

      // 1. Xin URL có chữ ký
      const ticket = await requestAttachmentUpload(taskId, file.name, type);

      // 2. PUT thẳng lên Cloudflare R2.
      //
      // Dùng fetch trần chứ KHÔNG dùng api-client: đây là request tới R2,
      // không phải tới backend của mình. Gắn Authorization header của mình
      // vào sẽ làm chữ ký của R2 sai.
      const res = await fetch(ticket.upload_url, {
        method: "PUT",
        body: file,
        headers: { "Content-Type": type },
      });
      if (!res.ok) {
        throw new Error(`Tải tệp lên thất bại (${res.status})`);
      }

      // 3. Xác nhận — server kiểm tra nội dung thật rồi mới ghi database
      return confirmAttachment(taskId, ticket.key, file.name);
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: taskKeys.attachments(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.detail(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.activities(taskId) });
    },
  });
}

export function useDeleteAttachment(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deleteAttachment,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: taskKeys.attachments(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.detail(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.activities(taskId) });
    },
  });
}

// ---------------------------------------------------- nhật ký, thời gian

export function useActivities(taskId: string | undefined) {
  return useQuery({
    queryKey: taskKeys.activities(taskId ?? ""),
    queryFn: () => listActivities(taskId!),
    enabled: Boolean(taskId),
  });
}

export function useTimelogs(taskId: string | undefined) {
  return useQuery({
    queryKey: taskKeys.timelogs(taskId ?? ""),
    queryFn: () => listTimelogs(taskId!),
    enabled: Boolean(taskId),
  });
}

export function useLogTime(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      spentMinutes,
      note,
      loggedOn,
    }: {
      spentMinutes: number;
      note: string;
      loggedOn?: string;
    }) => logTime(taskId, spentMinutes, note, loggedOn),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: taskKeys.timelogs(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.detail(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.activities(taskId) });
    },
  });
}

export function useDeleteTimelog(taskId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: deleteTimelog,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: taskKeys.timelogs(taskId) });
      void qc.invalidateQueries({ queryKey: taskKeys.detail(taskId) });
    },
  });
}
