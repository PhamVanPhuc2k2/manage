"use client";

import { useRef, useState } from "react";

import { FormError } from "@/components/form";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth/store";
import type { ProjectMember } from "../projects/types";
import {
  useActivities,
  useAttachments,
  useComments,
  useCreateComment,
  useDeleteAttachment,
  useDeleteComment,
  useDeleteTimelog,
  useLogTime,
  useSubtasks,
  useTask,
  useTimelogs,
  useUpdateTask,
  useUploadAttachment,
} from "./queries";
import {
  TASK_PRIORITY_LABEL,
  TASK_STATUS_LABEL,
  formatBytes,
  formatMinutes,
  type Task,
  type TaskPriority,
  type TaskStatus,
} from "./types";

const TABS = ["comments", "attachments", "timelogs", "history"] as const;
type Tab = (typeof TABS)[number];

const TAB_LABEL: Record<Tab, string> = {
  comments: "Bình luận",
  attachments: "Tệp đính kèm",
  timelogs: "Thời gian",
  history: "Lịch sử",
};

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

/**
 * Hiện nội dung bình luận, đổi cú pháp @[tên](uuid) thành tên đọc được.
 *
 * Cú pháp thô rất khó đọc, nhưng KHÔNG thể lưu sẵn dạng đã hiển thị: id là
 * thứ duy nhất phân biệt hai người trùng tên, và tên có thể đổi sau này.
 */
function renderMentions(content: string) {
  const parts = content.split(/(@\[[^\]]{1,100}\]\([0-9a-fA-F-]{36}\))/g);
  return parts.map((part, i) => {
    const m = /^@\[([^\]]{1,100})\]\([0-9a-fA-F-]{36}\)$/.exec(part);
    if (!m) return <span key={i}>{part}</span>;
    return (
      <span
        key={i}
        className="rounded bg-blue-100 px-1 font-medium text-blue-800 dark:bg-blue-950 dark:text-blue-300"
      >
        @{m[1]}
      </span>
    );
  });
}

/* ------------------------------------------------------------------ *
 * Ô soạn bình luận, có gợi ý @mention
 * ------------------------------------------------------------------ */

function CommentComposer({
  taskId,
  members,
}: {
  taskId: string;
  members: ProjectMember[];
}) {
  const create = useCreateComment(taskId);
  const [text, setText] = useState("");
  const [picking, setPicking] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);

  function insertMention(m: ProjectMember) {
    // Nhúng sẵn id vào nội dung. Backend chỉ đọc id ra, không phải đoán xem
    // "@Hà" là ai — tên trùng nhau là chuyện thường trong công ty.
    setText((t) => t.replace(/@(\S*)$/, "") + `@[${m.employee_name}](${m.employee_id}) `);
    setPicking(false);
    ref.current?.focus();
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (!text.trim()) return;
        create.mutate(text.trim(), { onSuccess: () => setText("") });
      }}
      className="space-y-2"
    >
      <div className="relative">
        <textarea
          ref={ref}
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setPicking(/@\S*$/.test(e.target.value));
          }}
          rows={3}
          placeholder="Viết bình luận. Gõ @ để nhắc tên thành viên dự án."
          className="w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950 dark:focus:border-neutral-400"
        />

        {picking && members.length > 0 && (
          <ul className="absolute bottom-full z-10 mb-1 max-h-48 w-64 overflow-y-auto rounded border border-neutral-200 bg-white shadow-lg dark:border-neutral-700 dark:bg-neutral-900">
            {members.map((m) => (
              <li key={m.employee_id}>
                <button
                  type="button"
                  onClick={() => insertMention(m)}
                  className="block w-full px-3 py-2 text-left text-sm hover:bg-neutral-100 dark:hover:bg-neutral-800"
                >
                  {m.employee_name}
                  {m.position_name && (
                    <span className="ml-2 text-xs text-neutral-500">
                      {m.position_name}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <FormError message={errMsg(create.error, "Không gửi được bình luận")} />

      <button
        type="submit"
        disabled={create.isPending || !text.trim()}
        className="rounded bg-neutral-900 px-4 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
      >
        {create.isPending ? "Đang gửi..." : "Gửi"}
      </button>
    </form>
  );
}

/* ------------------------------------------------------------------ *
 * Panel
 * ------------------------------------------------------------------ */

export function TaskDetailPanel({
  taskId,
  members,
  canWrite,
  onClose,
  onAddSubtask,
}: {
  taskId: string;
  members: ProjectMember[];
  canWrite: boolean;
  onClose: () => void;
  onAddSubtask?: (parent: Task) => void;
}) {
  const [tab, setTab] = useState<Tab>("comments");
  const myEmployeeId = useAuthStore((s) => s.user?.employee_id);

  const { data: task, isPending, error } = useTask(taskId);
  const { data: subtasks = [] } = useSubtasks(taskId);
  const update = useUpdateTask(taskId);

  if (isPending) {
    return (
      <aside className="w-[420px] shrink-0 border-l border-neutral-200 p-6 text-sm text-neutral-500 dark:border-neutral-800">
        Đang tải...
      </aside>
    );
  }
  if (error || !task) {
    return (
      <aside className="w-[420px] shrink-0 border-l border-neutral-200 p-6 dark:border-neutral-800">
        <FormError message={errMsg(error, "Không tải được công việc")} />
        <button onClick={onClose} className="mt-3 text-sm underline">
          Đóng
        </button>
      </aside>
    );
  }

  /** Gửi lại TOÀN BỘ trường vì API dùng PUT — thiếu trường nào là xoá trường đó. */
  function patch(changes: Partial<Task>) {
    const next = { ...task!, ...changes };
    update.mutate({
      title: next.title,
      description: next.description ?? "",
      status: next.status,
      priority: next.priority,
      assignee_id: next.assignee_id ?? null,
      due_date: next.due_date ?? null,
      estimate_hours: next.estimate_hours ?? null,
    });
  }

  return (
    <aside className="flex w-[420px] shrink-0 flex-col overflow-y-auto border-l border-neutral-200 dark:border-neutral-800">
      <div className="border-b border-neutral-200 p-4 dark:border-neutral-800">
        <div className="mb-2 flex items-start justify-between gap-2">
          <span className="font-mono text-xs text-neutral-500">{task.code}</span>
          <button
            onClick={onClose}
            aria-label="Đóng"
            className="text-neutral-400 hover:text-neutral-900 dark:hover:text-neutral-100"
          >
            ✕
          </button>
        </div>

        <h2 className="text-base font-semibold">{task.title}</h2>

        {task.description && (
          <p className="mt-2 whitespace-pre-wrap text-sm text-neutral-600 dark:text-neutral-400">
            {task.description}
          </p>
        )}

        <dl className="mt-4 grid grid-cols-[100px_1fr] gap-y-2 text-sm">
          <dt className="text-neutral-500">Trạng thái</dt>
          <dd>
            <select
              value={task.status}
              disabled={!canWrite || update.isPending}
              onChange={(e) => patch({ status: e.target.value as TaskStatus })}
              className="w-full rounded border border-neutral-300 px-2 py-1 text-sm disabled:opacity-60 dark:border-neutral-700 dark:bg-neutral-950"
            >
              {Object.entries(TASK_STATUS_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </dd>

          <dt className="text-neutral-500">Ưu tiên</dt>
          <dd>
            <select
              value={task.priority}
              disabled={!canWrite || update.isPending}
              onChange={(e) => patch({ priority: e.target.value as TaskPriority })}
              className="w-full rounded border border-neutral-300 px-2 py-1 text-sm disabled:opacity-60 dark:border-neutral-700 dark:bg-neutral-950"
            >
              {Object.entries(TASK_PRIORITY_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </dd>

          <dt className="text-neutral-500">Người làm</dt>
          <dd>
            <select
              value={task.assignee_id ?? ""}
              disabled={!canWrite || update.isPending}
              onChange={(e) => patch({ assignee_id: e.target.value || undefined })}
              className="w-full rounded border border-neutral-300 px-2 py-1 text-sm disabled:opacity-60 dark:border-neutral-700 dark:bg-neutral-950"
            >
              <option value="">Chưa giao</option>
              {members.map((m) => (
                <option key={m.employee_id} value={m.employee_id}>
                  {m.employee_name}
                </option>
              ))}
            </select>
          </dd>

          <dt className="text-neutral-500">Người tạo</dt>
          <dd className="py-1">{task.reporter_name || "—"}</dd>

          <dt className="text-neutral-500">Hạn</dt>
          <dd className={`py-1 ${task.overdue ? "font-medium text-red-600" : ""}`}>
            {task.due_date
              ? new Date(task.due_date).toLocaleDateString("vi-VN")
              : "—"}
            {task.overdue && " · quá hạn"}
          </dd>

          <dt className="text-neutral-500">Ước lượng</dt>
          <dd className="py-1">
            {task.estimate_hours ? `${task.estimate_hours}h` : "—"}
            {task.spent_minutes > 0 && (
              <span className="text-neutral-500">
                {" "}
                · đã ghi {formatMinutes(task.spent_minutes)}
              </span>
            )}
          </dd>
        </dl>

        <FormError message={errMsg(update.error, "Không cập nhật được công việc")} />
      </div>

      {/* --- Việc con --- */}
      <div className="border-b border-neutral-200 p-4 dark:border-neutral-800">
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-sm font-medium">
            Việc con{" "}
            {task.subtask_count > 0 && (
              <span className="text-xs text-neutral-500">
                {task.done_subtasks}/{task.subtask_count}
              </span>
            )}
          </h3>
          {/* Chỉ task GỐC mới thêm được việc con — hệ thống giới hạn 2 cấp. */}
          {canWrite && !task.parent_task_id && onAddSubtask && (
            <button
              onClick={() => onAddSubtask(task)}
              className="text-xs underline hover:no-underline"
            >
              Thêm
            </button>
          )}
        </div>

        {subtasks.length === 0 ? (
          <p className="text-xs text-neutral-400">
            {task.parent_task_id
              ? "Đây đã là việc con, không lồng thêm được."
              : "Chưa có việc con."}
          </p>
        ) : (
          <ul className="space-y-1 text-sm">
            {subtasks.map((s) => (
              <li key={s.id} className="flex items-center gap-2">
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    s.status === "done" ? "bg-green-500" : "bg-neutral-300"
                  }`}
                />
                <span className={s.status === "done" ? "text-neutral-400 line-through" : ""}>
                  {s.title}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* --- Tab --- */}
      <div className="flex border-b border-neutral-200 text-sm dark:border-neutral-800">
        {TABS.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            className={`flex-1 px-2 py-2 transition ${
              tab === t
                ? "border-b-2 border-neutral-900 font-medium dark:border-white"
                : "text-neutral-500 hover:text-neutral-900 dark:hover:text-neutral-100"
            }`}
          >
            {TAB_LABEL[t]}
          </button>
        ))}
      </div>

      <div className="flex-1 p-4">
        {tab === "comments" && (
          <CommentsTab
            taskId={taskId}
            members={members}
            canWrite={canWrite}
            myEmployeeId={myEmployeeId}
          />
        )}
        {tab === "attachments" && (
          <AttachmentsTab taskId={taskId} canWrite={canWrite} />
        )}
        {tab === "timelogs" && (
          <TimelogsTab
            taskId={taskId}
            canWrite={canWrite}
            myEmployeeId={myEmployeeId}
          />
        )}
        {tab === "history" && <HistoryTab taskId={taskId} />}
      </div>
    </aside>
  );
}

/* ------------------------------------------------------------------ *
 * Các tab
 * ------------------------------------------------------------------ */

function CommentsTab({
  taskId,
  members,
  canWrite,
  myEmployeeId,
}: {
  taskId: string;
  members: ProjectMember[];
  canWrite: boolean;
  myEmployeeId?: string;
}) {
  const { data: comments = [], isPending } = useComments(taskId);
  const remove = useDeleteComment(taskId);

  return (
    <div className="space-y-4">
      {isPending && <p className="text-sm text-neutral-500">Đang tải...</p>}

      {comments.map((c) => (
        <div key={c.id} className="text-sm">
          <div className="mb-1 flex items-baseline justify-between gap-2">
            <span className="font-medium">{c.author_name}</span>
            <span className="shrink-0 text-xs text-neutral-500">
              {new Date(c.created_at).toLocaleString("vi-VN")}
              {c.edited_at && " · đã sửa"}
            </span>
          </div>
          <p className="whitespace-pre-wrap text-neutral-700 dark:text-neutral-300">
            {renderMentions(c.content)}
          </p>
          {c.author_id === myEmployeeId && (
            <button
              onClick={() => remove.mutate(c.id)}
              className="mt-1 text-xs text-neutral-400 underline hover:text-red-600"
            >
              Xoá
            </button>
          )}
        </div>
      ))}

      {!isPending && comments.length === 0 && (
        <p className="text-sm text-neutral-400">Chưa có bình luận nào.</p>
      )}

      {canWrite && <CommentComposer taskId={taskId} members={members} />}
    </div>
  );
}

function AttachmentsTab({
  taskId,
  canWrite,
}: {
  taskId: string;
  canWrite: boolean;
}) {
  const { data: files = [], isPending } = useAttachments(taskId);
  const upload = useUploadAttachment(taskId);
  const remove = useDeleteAttachment(taskId);

  return (
    <div className="space-y-3">
      {isPending && <p className="text-sm text-neutral-500">Đang tải...</p>}

      {files.map((f) => (
        <div key={f.id} className="flex items-center justify-between gap-2 text-sm">
          <div className="min-w-0">
            {f.download_url ? (
              <a
                href={f.download_url}
                target="_blank"
                rel="noopener noreferrer"
                className="block truncate hover:underline"
              >
                {f.file_name}
              </a>
            ) : (
              <span className="block truncate text-neutral-500">{f.file_name}</span>
            )}
            <span className="text-xs text-neutral-500">
              {formatBytes(f.size_bytes)}
              {f.uploader_name && ` · ${f.uploader_name}`}
            </span>
          </div>
          {canWrite && (
            <button
              onClick={() => remove.mutate(f.id)}
              className="shrink-0 text-xs text-neutral-400 underline hover:text-red-600"
            >
              Xoá
            </button>
          )}
        </div>
      ))}

      {!isPending && files.length === 0 && (
        <p className="text-sm text-neutral-400">Chưa có tệp nào.</p>
      )}

      {canWrite && (
        <div>
          <input
            type="file"
            disabled={upload.isPending}
            onChange={(e) => {
              const file = e.target.files?.[0];
              if (file) upload.mutate(file);
              e.target.value = ""; // cho phép chọn lại đúng tệp đó
            }}
            className="block w-full text-xs file:mr-2 file:rounded file:border file:border-neutral-300 file:bg-transparent file:px-2 file:py-1 file:text-xs dark:file:border-neutral-700"
          />
          {upload.isPending && (
            <p className="mt-1 text-xs text-neutral-500">Đang tải lên...</p>
          )}
          <FormError message={errMsg(upload.error, "Không tải được tệp lên")} />
        </div>
      )}
    </div>
  );
}

function TimelogsTab({
  taskId,
  canWrite,
  myEmployeeId,
}: {
  taskId: string;
  canWrite: boolean;
  myEmployeeId?: string;
}) {
  const { data: logs = [], isPending } = useTimelogs(taskId);
  const log = useLogTime(taskId);
  const remove = useDeleteTimelog(taskId);

  const [hours, setHours] = useState("");
  const [note, setNote] = useState("");

  const total = logs.reduce((s, l) => s + l.spent_minutes, 0);

  return (
    <div className="space-y-3">
      {isPending && <p className="text-sm text-neutral-500">Đang tải...</p>}

      {total > 0 && (
        <p className="text-sm font-medium">Tổng: {formatMinutes(total)}</p>
      )}

      {logs.map((l) => (
        <div key={l.id} className="flex items-start justify-between gap-2 text-sm">
          <div>
            <span className="font-medium">{formatMinutes(l.spent_minutes)}</span>
            <span className="ml-2 text-xs text-neutral-500">
              {l.employee_name} · {new Date(l.logged_on).toLocaleDateString("vi-VN")}
            </span>
            {l.note && (
              <p className="text-xs text-neutral-600 dark:text-neutral-400">{l.note}</p>
            )}
          </div>
          {l.employee_id === myEmployeeId && (
            <button
              onClick={() => remove.mutate(l.id)}
              className="shrink-0 text-xs text-neutral-400 underline hover:text-red-600"
            >
              Xoá
            </button>
          )}
        </div>
      ))}

      {!isPending && logs.length === 0 && (
        <p className="text-sm text-neutral-400">Chưa ghi nhận thời gian nào.</p>
      )}

      {canWrite && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const h = parseFloat(hours);
            if (!h || h <= 0) return;
            // Nhập theo GIỜ cho tự nhiên, gửi lên theo PHÚT vì đó là đơn vị
            // lưu trữ — "1.5 giờ" nhập tay dễ thành 1.05, 90 phút thì không.
            log.mutate(
              { spentMinutes: Math.round(h * 60), note },
              { onSuccess: () => { setHours(""); setNote(""); } },
            );
          }}
          className="space-y-2 border-t border-neutral-200 pt-3 dark:border-neutral-800"
        >
          <div className="flex gap-2">
            <input
              type="number"
              step="0.25"
              min="0"
              value={hours}
              onChange={(e) => setHours(e.target.value)}
              placeholder="Số giờ"
              className="w-24 rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Ghi chú (tuỳ chọn)"
              className="flex-1 rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </div>
          <FormError message={errMsg(log.error, "Không ghi được thời gian")} />
          <button
            type="submit"
            disabled={log.isPending}
            className="rounded border border-neutral-300 px-3 py-1 text-sm disabled:opacity-50 dark:border-neutral-700"
          >
            Ghi nhận
          </button>
        </form>
      )}
    </div>
  );
}

const ACTION_LABEL: Record<string, string> = {
  created: "đã tạo công việc",
  status_changed: "đổi trạng thái",
  assigned: "giao việc",
  field_changed: "sửa thông tin",
  commented: "đã bình luận",
  attachment_added: "đính kèm tệp",
  attachment_removed: "gỡ tệp",
  time_logged: "ghi nhận thời gian",
  reordered: "đổi vị trí",
  deleted: "đã xoá công việc",
};

function HistoryTab({ taskId }: { taskId: string }) {
  const { data: rows = [], isPending } = useActivities(taskId);

  return (
    <ol className="space-y-3 text-sm">
      {isPending && <p className="text-neutral-500">Đang tải...</p>}

      {rows.map((a) => (
        <li key={a.id}>
          <span className="font-medium">{a.actor_name || "Hệ thống"}</span>{" "}
          <span className="text-neutral-600 dark:text-neutral-400">
            {ACTION_LABEL[a.action] ?? a.action}
          </span>
          {a.action === "status_changed" && a.old_value && a.new_value && (
            <span className="text-neutral-600 dark:text-neutral-400">
              : {TASK_STATUS_LABEL[a.old_value as TaskStatus] ?? a.old_value} →{" "}
              {TASK_STATUS_LABEL[a.new_value as TaskStatus] ?? a.new_value}
            </span>
          )}
          {a.action === "time_logged" && a.new_value && (
            <span className="text-neutral-600 dark:text-neutral-400"> {a.new_value}</span>
          )}
          <div className="text-xs text-neutral-500">
            {new Date(a.created_at).toLocaleString("vi-VN")}
          </div>
        </li>
      ))}

      {!isPending && rows.length === 0 && (
        <p className="text-neutral-400">Chưa có thay đổi nào.</p>
      )}
    </ol>
  );
}
