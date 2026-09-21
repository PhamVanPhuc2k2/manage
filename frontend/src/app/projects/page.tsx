"use client";

import { useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { ProjectForm } from "@/features/projects/ProjectForm";
import { useProjects } from "@/features/projects/queries";
import {
  PROJECT_STATUS_CLASS,
  PROJECT_STATUS_LABEL,
  type Project,
  type ProjectStatus,
} from "@/features/projects/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";
import { useDebounce } from "@/lib/use-debounce";

type View = "cards" | "table";

function StatusBadge({ status }: { status: ProjectStatus }) {
  return (
    <span
      className={`rounded px-2 py-0.5 text-xs ${PROJECT_STATUS_CLASS[status]}`}
    >
      {PROJECT_STATUS_LABEL[status]}
    </span>
  );
}

function ProgressBar({ value }: { value: number }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-neutral-200 dark:bg-neutral-800">
      <div
        className="h-full rounded-full bg-neutral-900 transition-all dark:bg-white"
        style={{ width: `${value}%` }}
      />
    </div>
  );
}

function formatDate(s?: string) {
  return s ? new Date(s).toLocaleDateString("vi-VN") : "—";
}

function ProjectCard({ p }: { p: Project }) {
  return (
    <Link
      href={`/projects/${p.id}`}
      className="block rounded-lg border border-neutral-200 p-4 transition hover:border-neutral-400 dark:border-neutral-800 dark:hover:border-neutral-600"
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <span className="font-mono text-xs text-neutral-500">{p.code}</span>
        <StatusBadge status={p.status} />
      </div>

      <h2 className="mb-1 font-medium">{p.name}</h2>
      {p.description && (
        <p className="mb-3 line-clamp-2 text-sm text-neutral-500">{p.description}</p>
      )}

      <div className="mb-2 flex items-center justify-between text-xs text-neutral-500">
        <span>
          {p.done_task_count}/{p.task_count} việc
        </span>
        <span>{p.progress}%</span>
      </div>
      <ProgressBar value={p.progress} />

      <div className="mt-3 flex items-center justify-between text-xs text-neutral-500">
        <span>{p.owner_name}</span>
        <span>{p.member_count} thành viên</span>
      </div>
    </Link>
  );
}

export default function ProjectsPage() {
  const { can } = usePermission();

  const [view, setView] = useState<View>("cards");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<ProjectStatus | "">("");
  const [page, setPage] = useState(1);
  const [formOpen, setFormOpen] = useState(false);

  // Giá trị đã trễ mới đi vào queryKey — gõ 10 ký tự chỉ sinh 1 request.
  const debouncedSearch = useDebounce(search);

  const { data, isPending, isFetching, error } = useProjects({
    search: debouncedSearch,
    status,
    page,
  });

  const items = data?.items ?? [];
  const meta = data?.meta;

  return (
    <AppShell>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Dự án</h1>
          {meta && (
            <p className="mt-1 text-sm text-neutral-500">
              {meta.total_items} dự án
              {isFetching && !isPending && " · đang cập nhật..."}
            </p>
          )}
        </div>

        {/* Ẩn nút chỉ là trang trí — backend vẫn kiểm tra quyền đầy đủ. */}
        {can("project:create") && (
          <button
            onClick={() => setFormOpen(true)}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Thêm dự án
          </button>
        )}
      </div>

      <div className="mb-4 flex flex-wrap gap-3">
        <input
          type="search"
          placeholder="Tìm theo tên hoặc mã dự án (gõ không dấu cũng được)"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
          className="min-w-[240px] flex-1 rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950"
        />

        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value as ProjectStatus | "");
            setPage(1);
          }}
          className="rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">Mọi trạng thái</option>
          {Object.entries(PROJECT_STATUS_LABEL).map(([v, label]) => (
            <option key={v} value={v}>
              {label}
            </option>
          ))}
        </select>

        <div className="flex rounded border border-neutral-300 text-sm dark:border-neutral-700">
          {(["cards", "table"] as View[]).map((v) => (
            <button
              key={v}
              onClick={() => setView(v)}
              className={`px-3 py-2 transition ${
                view === v
                  ? "bg-neutral-900 text-white dark:bg-white dark:text-neutral-900"
                  : "hover:bg-neutral-100 dark:hover:bg-neutral-800"
              }`}
            >
              {v === "cards" ? "Thẻ" : "Bảng"}
            </button>
          ))}
        </div>
      </div>

      <FormError
        message={
          error instanceof ApiError
            ? error.message
            : error
              ? "Không tải được danh sách dự án"
              : null
        }
      />

      {isPending ? (
        <p className="py-8 text-center text-sm text-neutral-500">Đang tải...</p>
      ) : items.length === 0 ? (
        <p className="py-8 text-center text-sm text-neutral-500">
          Chưa có dự án nào.
        </p>
      ) : view === "cards" ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {items.map((p) => (
            <ProjectCard key={p.id} p={p} />
          ))}
        </div>
      ) : (
        <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
          <table className="w-full text-sm">
            <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
              <tr>
                <th className="px-4 py-2 font-medium">Mã</th>
                <th className="px-4 py-2 font-medium">Tên dự án</th>
                <th className="px-4 py-2 font-medium">Chủ dự án</th>
                <th className="px-4 py-2 font-medium">Trạng thái</th>
                <th className="px-4 py-2 font-medium">Tiến độ</th>
                <th className="px-4 py-2 font-medium">Hạn</th>
              </tr>
            </thead>
            <tbody>
              {items.map((p) => (
                <tr
                  key={p.id}
                  className="border-t border-neutral-200 dark:border-neutral-800"
                >
                  <td className="px-4 py-2 font-mono text-xs">{p.code}</td>
                  <td className="px-4 py-2">
                    <Link href={`/projects/${p.id}`} className="hover:underline">
                      {p.name}
                    </Link>
                  </td>
                  <td className="px-4 py-2">{p.owner_name}</td>
                  <td className="px-4 py-2">
                    <StatusBadge status={p.status} />
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex items-center gap-2">
                      <div className="w-20">
                        <ProgressBar value={p.progress} />
                      </div>
                      <span className="text-xs text-neutral-500">{p.progress}%</span>
                    </div>
                  </td>
                  <td className="px-4 py-2 text-neutral-500">
                    {formatDate(p.due_date)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

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

      {formOpen && <ProjectForm onClose={() => setFormOpen(false)} />}
    </AppShell>
  );
}
