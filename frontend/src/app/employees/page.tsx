"use client";

import { useCallback, useEffect, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { api, ApiError, type PageMeta } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/AuthProvider";

type Employee = {
  id: string;
  employee_code: string;
  full_name: string;
  email: string;
  phone?: string;
  department_name?: string;
  position_name?: string;
  work_mode: string;
  status: string;
  joined_at: string;
  has_account: boolean;
};

const STATUS_LABEL: Record<string, string> = {
  probation: "Thử việc",
  official: "Chính thức",
  resigned: "Đã nghỉ",
};

const WORK_MODE_LABEL: Record<string, string> = {
  onsite: "Tại văn phòng",
  remote: "Từ xa",
  hybrid: "Kết hợp",
};

export default function EmployeesPage() {
  const { can } = usePermission();

  const [items, setItems] = useState<Employee[]>([]);
  const [meta, setMeta] = useState<PageMeta | null>(null);
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState("");
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);

    const params = new URLSearchParams({ page: String(page), page_size: "20" });
    if (search.trim()) params.set("search", search.trim());
    if (status) params.set("status", status);

    try {
      const res = await api.get<Employee[]>(`/employees?${params}`);
      setItems(res.data ?? []);
      setMeta(res.meta ?? null);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : "Không tải được danh sách");
    } finally {
      setLoading(false);
    }
  }, [page, search, status]);

  // Chờ 300ms sau lần gõ cuối mới gọi API, tránh bắn một request mỗi ký tự.
  useEffect(() => {
    const t = setTimeout(load, 300);
    return () => clearTimeout(t);
  }, [load]);

  return (
    <AppShell>
      <div className="mb-6 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Nhân viên</h1>
          {meta && (
            <p className="mt-1 text-sm text-neutral-500">
              {meta.total_items} người
            </p>
          )}
        </div>

        {/* Ẩn nút chỉ là trang trí — backend vẫn kiểm tra quyền đầy đủ. */}
        {can("employee:create") && (
          <button className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900">
            Thêm nhân viên
          </button>
        )}
      </div>

      <div className="mb-4 flex gap-3">
        <input
          type="search"
          placeholder="Tìm theo tên, mã hoặc email (gõ không dấu cũng được)"
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
            setPage(1);
          }}
          className="flex-1 rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950"
        />
        <select
          value={status}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(1);
          }}
          className="rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">Mọi trạng thái</option>
          <option value="probation">Thử việc</option>
          <option value="official">Chính thức</option>
          <option value="resigned">Đã nghỉ</option>
        </select>
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Họ tên</th>
              <th className="px-4 py-2 font-medium">Email</th>
              <th className="px-4 py-2 font-medium">Phòng ban</th>
              <th className="px-4 py-2 font-medium">Chức vụ</th>
              <th className="px-4 py-2 font-medium">Hình thức</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!loading && items.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-500">
                  Không có nhân viên nào
                </td>
              </tr>
            )}
            {!loading &&
              items.map((e) => (
                <tr
                  key={e.id}
                  className="border-t border-neutral-200 dark:border-neutral-800"
                >
                  <td className="px-4 py-2 font-mono text-xs">{e.employee_code}</td>
                  <td className="px-4 py-2">{e.full_name}</td>
                  <td className="px-4 py-2 text-neutral-500">{e.email}</td>
                  <td className="px-4 py-2">{e.department_name || "—"}</td>
                  <td className="px-4 py-2">{e.position_name || "—"}</td>
                  <td className="px-4 py-2">
                    {WORK_MODE_LABEL[e.work_mode] ?? e.work_mode}
                  </td>
                  <td className="px-4 py-2">
                    {STATUS_LABEL[e.status] ?? e.status}
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>

      {meta && meta.total_pages > 1 && (
        <div className="mt-4 flex items-center justify-between text-sm">
          <span className="text-neutral-500">
            Trang {meta.page} / {meta.total_pages}
          </span>
          <div className="flex gap-2">
            <button
              disabled={page <= 1}
              onClick={() => setPage((p) => p - 1)}
              className="rounded border border-neutral-300 px-3 py-1 disabled:opacity-40 dark:border-neutral-700"
            >
              Trước
            </button>
            <button
              disabled={page >= meta.total_pages}
              onClick={() => setPage((p) => p + 1)}
              className="rounded border border-neutral-300 px-3 py-1 disabled:opacity-40 dark:border-neutral-700"
            >
              Sau
            </button>
          </div>
        </div>
      )}
    </AppShell>
  );
}
