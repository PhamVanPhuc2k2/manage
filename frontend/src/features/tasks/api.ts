import { api, type PageMeta } from "@/lib/api-client";
import type {
  Board,
  Task,
  TaskActivity,
  TaskAttachment,
  TaskComment,
  TaskFilters,
  TaskStatus,
  Timelog,
} from "./types";

function buildQuery(f: TaskFilters): string {
  const p = new URLSearchParams();
  if (f.project_id) p.set("project_id", f.project_id);
  if (f.status) p.set("status", f.status);
  if (f.priority) p.set("priority", f.priority);
  if (f.assignee_id) p.set("assignee_id", f.assignee_id);
  if (f.search) p.set("search", f.search);
  if (f.unfinished) p.set("unfinished", "true");
  p.set("page", String(f.page ?? 1));
  return p.toString();
}

export async function listTasks(
  f: TaskFilters,
): Promise<{ items: Task[]; meta?: PageMeta }> {
  const { data, meta } = await api.get<Task[]>(`/tasks?${buildQuery(f)}`);
  return { items: data ?? [], meta };
}

export async function listMyTasks(
  f: TaskFilters,
): Promise<{ items: Task[]; meta?: PageMeta }> {
  const { data, meta } = await api.get<Task[]>(`/tasks/my?${buildQuery(f)}`);
  return { items: data ?? [], meta };
}

export async function listOverdueTasks(
  projectId?: string,
): Promise<{ items: Task[]; meta?: PageMeta }> {
  const q = projectId ? `?project_id=${projectId}` : "";
  const { data, meta } = await api.get<Task[]>(`/tasks/overdue${q}`);
  return { items: data ?? [], meta };
}

export async function getBoard(projectId: string): Promise<Board> {
  const { data } = await api.get<Board>(`/projects/${projectId}/board`);
  return data;
}

export async function getTask(id: string): Promise<Task> {
  const { data } = await api.get<Task>(`/tasks/${id}`);
  return data;
}

export async function listSubtasks(id: string): Promise<Task[]> {
  const { data } = await api.get<Task[]>(`/tasks/${id}/subtasks`);
  return data ?? [];
}

export async function createTask(body: unknown): Promise<Task> {
  const { data } = await api.post<Task>("/tasks", body);
  return data;
}

export async function updateTask(id: string, body: unknown): Promise<Task> {
  const { data } = await api.put<Task>(`/tasks/${id}`, body);
  return data;
}

/**
 * Kéo-thả trên Kanban.
 *
 * afterTaskId là task mà task này sẽ nằm NGAY SAU; null nghĩa là lên đầu cột.
 * Gửi "nằm sau cái gì" thay vì "vị trí thứ mấy" vì chỉ số sẽ sai ngay khi có
 * người khác chèn task trong lúc mình đang kéo.
 */
export async function moveTask(
  id: string,
  status: TaskStatus,
  afterTaskId: string | null,
): Promise<Task> {
  const { data } = await api.patch<Task>(`/tasks/${id}/move`, {
    status,
    after_task_id: afterTaskId,
  });
  return data;
}

export async function deleteTask(id: string): Promise<void> {
  await api.delete(`/tasks/${id}`);
}

// ------------------------------------------------------------ bình luận

export async function listComments(taskId: string): Promise<TaskComment[]> {
  const { data } = await api.get<TaskComment[]>(`/tasks/${taskId}/comments`);
  return data ?? [];
}

export async function createComment(
  taskId: string,
  content: string,
): Promise<TaskComment> {
  const { data } = await api.post<TaskComment>(`/tasks/${taskId}/comments`, {
    content,
  });
  return data;
}

export async function updateComment(
  commentId: string,
  content: string,
): Promise<TaskComment> {
  const { data } = await api.put<TaskComment>(`/tasks/comments/${commentId}`, {
    content,
  });
  return data;
}

export async function deleteComment(commentId: string): Promise<void> {
  await api.delete(`/tasks/comments/${commentId}`);
}

// -------------------------------------------------------- tệp đính kèm

export async function listAttachments(taskId: string): Promise<TaskAttachment[]> {
  const { data } = await api.get<TaskAttachment[]>(`/tasks/${taskId}/attachments`);
  return data ?? [];
}

type UploadTicket = { upload_url: string; key: string; expires_in: number };

export async function requestAttachmentUpload(
  taskId: string,
  fileName: string,
  contentType: string,
): Promise<UploadTicket> {
  const { data } = await api.post<UploadTicket>(
    `/tasks/${taskId}/attachments/upload-url`,
    { file_name: fileName, content_type: contentType },
  );
  return data;
}

export async function confirmAttachment(
  taskId: string,
  key: string,
  fileName: string,
): Promise<TaskAttachment> {
  const { data } = await api.post<TaskAttachment>(
    `/tasks/${taskId}/attachments/confirm`,
    { key, file_name: fileName },
  );
  return data;
}

export async function deleteAttachment(attachmentId: string): Promise<void> {
  await api.delete(`/tasks/attachments/${attachmentId}`);
}

// ---------------------------------------------------- nhật ký, thời gian

export async function listActivities(taskId: string): Promise<TaskActivity[]> {
  const { data } = await api.get<TaskActivity[]>(`/tasks/${taskId}/activities`);
  return data ?? [];
}

export async function listTimelogs(taskId: string): Promise<Timelog[]> {
  const { data } = await api.get<Timelog[]>(`/tasks/${taskId}/timelogs`);
  return data ?? [];
}

export async function logTime(
  taskId: string,
  spentMinutes: number,
  note: string,
  loggedOn?: string,
): Promise<Timelog> {
  const { data } = await api.post<Timelog>(`/tasks/${taskId}/timelogs`, {
    spent_minutes: spentMinutes,
    note,
    logged_on: loggedOn ?? null,
  });
  return data;
}

export async function deleteTimelog(timelogId: string): Promise<void> {
  await api.delete(`/tasks/timelogs/${timelogId}`);
}
