"use client";

import { useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { useMyTasks, useTasks } from "@/features/tasks/queries";
import {
  TASK_PRIORITY_CLASS,
  TASK_PRIORITY_LABEL,
  TASK_STATUS_LABEL,
  type Task,
  type TaskPriority,
  type TaskStatus,
} from "@/features/tasks/types";
import { ApiError } from "@/lib/api-client";
import { useDebounce } from "@/lib/use-debounce";

type Tab = "mine" | "all";

function TaskRow({ t }: { t: Task }) {
  return (
    <tr className="border-t border-neutral-200 dark:border-neutral-800">
      <td className="px-4 py-2 font-mono text-xs text-neutral-500">{t.code}</td>
      <td className="px-4 py-2">
        {/* Mở ở bảng Kanban của dự án: đó là nơi công việc có ngữ cảnh đầy
            đủ (cột, việc xung quanh), thay vì một trang chi tiết đứng một mình. */}
        <Link href={`/projects/${t.project_id}/board`} className="hover:underline">
          {t.title}
        </Link>
        {t.parent_task_id && (
          <span className="ml-2 text-xs text-neutral-400">(việc con)</span>
        )}
      </td>
      <td className="px-4 py-2 text-neutral-500">{t.project_name}</td>
      <td className="px-4 py-2">{t.assignee_name || "—"}</td>
      <td className="px-4 py-2">{TASK_STATUS_LABEL[t.status]}</td>
      <td className="px-4 py-2">
        <span
          className={`rounded px-1.5 py-0.5 text-xs ${TASK_PRIORITY_CLASS[t.priority]}`}
        >
          {TASK_PRIORITY_LABEL[t.priority]}
        </span>
      </td>
      <td
        className={`px-4 py-2 text-sm ${t.overdue ? "font-medium text-red-600" : "text-neutral-500"}`}
      >
        {t.due_date ? new Date(t.due_date).toLocaleDateString("vi-VN") : "—"}
      </td>
    </tr>
  );
}

export default function TasksPage() {
  const [tab, setTab] = useState<Tab>("mine");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<TaskStatus | "">("");
  const [priority, setPriority] = useState<TaskPriority | "">("");
  const [unfinished, setUnfinished] = useState(true);
  const [page, setPage] = useState(1);

  const debouncedSearch = useDebounce(search);
  const filters = {
    search: debouncedSearch,
    status,
    priority,
    unfinished,
    page,
  };

  // Gọi cả hai hook nhưng chỉ bật đúng một — hook không được gọi có điều
  // kiện trong React, nên `enabled` là cách đúng để tắt nhánh không dùng.
  const mine = useMyTasks(tab === "mine" ? filters : { ...filters, page: 1 });
  const all = useTasks(filters, tab === "all");

  const active = tab === "mine" ? mine : all;
  const items = active.data?.items ?? [];
  const meta = active.data?.meta;

  return (
    <AppShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold">Công việc</h1>
        {meta && (
          <p className="mt-1 text-sm text-neutral-500">
            {meta.total_items} việc
            {active.isFetching && !active.isPending && " · đang cập nhật..."}
          </p>
        )}
      </div>

      <div className="mb-4 flex rounded border border-neutral-300 text-sm dark:border-neutral-700">
        {(
          [
            ["mine", "Việc của tôi"],
            ["all", "Tất cả việc"],
          ] as [Tab, string][]
        ).map(([v, label]) => (
          <button
            key={v}
            onClick={() => {
              setTab(v);
              setPage(1);
            }}
            className={`px-4 py-2 transition ${
              tab === v
                ? "bg-neutral-900 text-white dark:bg-white dark:text-neutral-900"
                : "hover:bg-neutral-100 dark:hover:bg-neutral-800"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <input
          type="search"
          placeholder="Tìm theo tiêu đề (gõ không dấu cũng được)"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
          className="min-w-[220px] flex-1 rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950"
        />

        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as TaskStatus | "");
            setPage(1);
          }}
          className="rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">Mọi trạng thái</option>
          {Object.entries(TASK_STATUS_LABEL).map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </select>

        <select
          value={priority}
          onChange={(e) => {
            setPriority(e.target.value as TaskPriority | "");
            setPage(1);
          }}
          className="rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">Mọi độ ưu tiên</option>
          {Object.entries(TASK_PRIORITY_LABEL).map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </select>

        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={unfinished}
            onChange={(e) => {
              setUnfinished(e.target.checked);
              setPage(1);
            }}
          />
          Ẩn việc đã xong
        </label>
      </div>

      <FormError
        message={
          active.error instanceof ApiError
            ? active.error.message
            : active.error
              ? "Không tải được danh sách công việc"
              : null
        }
      />

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Tiêu đề</th>
              <th className="px-4 py-2 font-medium">Dự án</th>
              <th className="px-4 py-2 font-medium">Người làm</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
              <th className="px-4 py-2 font-medium">Ưu tiên</th>
              <th className="px-4 py-2 font-medium">Hạn</th>
            </tr>
          </thead>
          <tbody>
            {active.isPending && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!active.isPending && items.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-500">
                  {tab === "mine"
                    ? "Bạn chưa được giao việc nào."
                    : "Không có công việc nào khớp bộ lọc."}
                </td>
              </tr>
            )}
            {items.map((t) => (
              <TaskRow key={t.id} t={t} />
            ))}
          </tbody>
        </table>
      </div>

      {meta && meta.total_pages > 1 && (
        <div className="mt-4 flex items-center justify-center gap-3 text-sm">
          <button
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
            className="rounded border border-neutral-300 px-3 py-1 disabled:opacity-40 dark:border-neutral-700"
          >
            Trước
          </button>
          <span className="text-neutral-500">
            Trang {meta.page} / {meta.total_pages}
          </span>
          <button
            disabled={page >= meta.total_pages}
            onClick={() => setPage((p) => p + 1)}
            className="rounded border border-neutral-300 px-3 py-1 disabled:opacity-40 dark:border-neutral-700"
          >
            Sau
          </button>
        </div>
      )}
    </AppShell>
  );
}
