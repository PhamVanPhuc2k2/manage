"use client";

import { useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  EXPORT_STATUS_LABEL,
  downloadExport,
  useAttendanceExports,
  useRequestExport,
  type AttendanceExport,
} from "@/features/attendance/exports";
import { useDepartments } from "@/features/departments/queries";
import { ApiError } from "@/lib/api-client";

const formatTime = (s: string) => new Date(s).toLocaleString("vi-VN");

export default function AttendanceExportPage() {
  const now = new Date();
  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth() + 1);
  const [departmentId, setDepartmentId] = useState("");

  const { data: departments = [] } = useDepartments();
  const { data: exports = [], isPending } = useAttendanceExports();
  const request = useRequestExport();
  const [downloadError, setDownloadError] = useState<string | null>(null);

  const download = async (e: AttendanceExport) => {
    setDownloadError(null);
    try {
      await downloadExport(e);
    } catch (err) {
      setDownloadError(
        err instanceof ApiError ? err.message : "Không tải được tệp",
      );
    }
  };

  // Chỉ cho chọn tới tháng hiện tại: máy chủ cũng chặn, nhưng không nên để
  // người dùng chọn một thứ chắc chắn bị từ chối.
  const years = [
    now.getFullYear(),
    now.getFullYear() - 1,
    now.getFullYear() - 2,
  ];
  const maxMonth = year === now.getFullYear() ? now.getMonth() + 1 : 12;

  return (
    <AppShell>
      <div className="mb-6">
        <Link
          href="/attendance/team"
          className="text-sm text-neutral-500 hover:underline"
        >
          ← Chấm công phòng ban
        </Link>
        <h1 className="mt-2 text-xl font-semibold">Xuất báo cáo chấm công</h1>
        <p className="mt-1 text-sm text-neutral-500">
          Tệp Excel gồm trang Tổng hợp (mỗi người một dòng) và trang Chi tiết
          (từng ngày). Chỉ có những người bạn được xem công.
        </p>
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          request.mutate({ year, month, departmentId });
        }}
        className="mb-8 flex flex-wrap items-end gap-3 rounded border border-neutral-200 p-4 dark:border-neutral-800"
      >
        <label className="text-sm">
          <span className="mb-1 block text-xs text-neutral-500">Tháng</span>
          <select
            value={month}
            onChange={(e) => setMonth(Number(e.target.value))}
            className="rounded border border-neutral-300 px-3 py-2 dark:border-neutral-700 dark:bg-neutral-950"
          >
            {Array.from({ length: maxMonth }, (_, i) => i + 1).map((m) => (
              <option key={m} value={m}>
                Tháng {m}
              </option>
            ))}
          </select>
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-xs text-neutral-500">Năm</span>
          <select
            value={year}
            onChange={(e) => {
              const y = Number(e.target.value);
              setYear(y);
              if (y === now.getFullYear())
                setMonth((m) => Math.min(m, now.getMonth() + 1));
            }}
            className="rounded border border-neutral-300 px-3 py-2 dark:border-neutral-700 dark:bg-neutral-950"
          >
            {years.map((y) => (
              <option key={y} value={y}>
                {y}
              </option>
            ))}
          </select>
        </label>
        <label className="text-sm">
          <span className="mb-1 block text-xs text-neutral-500">Phòng ban</span>
          <select
            value={departmentId}
            onChange={(e) => setDepartmentId(e.target.value)}
            className="rounded border border-neutral-300 px-3 py-2 dark:border-neutral-700 dark:bg-neutral-950"
          >
            <option value="">Tất cả phòng bạn được xem</option>
            {departments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </label>
        <button
          type="submit"
          disabled={request.isPending}
          className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-40 dark:bg-white dark:text-neutral-900"
        >
          {request.isPending ? "Đang gửi..." : "Xuất Excel"}
        </button>
        <div className="w-full">
          <FormError
            message={
              request.error
                ? request.error instanceof ApiError
                  ? request.error.message
                  : "Không gửi được yêu cầu"
                : null
            }
          />
        </div>
      </form>

      <h2 className="mb-3 font-medium">Các lần xuất gần đây</h2>
      <FormError message={downloadError} />
      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Kỳ</th>
              <th className="px-4 py-2 font-medium">Phạm vi</th>
              <th className="px-4 py-2 font-medium">Người xuất</th>
              <th className="px-4 py-2 font-medium">Lúc</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
              <th className="px-4 py-2 font-medium" />
            </tr>
          </thead>
          <tbody>
            {isPending && (
              <tr>
                <td
                  colSpan={6}
                  className="px-4 py-6 text-center text-neutral-500"
                >
                  Đang tải...
                </td>
              </tr>
            )}
            {!isPending && exports.length === 0 && (
              <tr>
                <td
                  colSpan={6}
                  className="px-4 py-6 text-center text-neutral-500"
                >
                  Chưa xuất lần nào
                </td>
              </tr>
            )}
            {exports.map((e) => (
              <tr
                key={e.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2 font-medium">
                  {String(e.month).padStart(2, "0")}/{e.year}
                </td>
                <td className="px-4 py-2">{e.department_name || "Tất cả"}</td>
                <td className="px-4 py-2">{e.requester_name || "—"}</td>
                <td className="px-4 py-2 text-neutral-500">
                  {formatTime(e.created_at)}
                </td>
                <td className="px-4 py-2">
                  {EXPORT_STATUS_LABEL[e.status]}
                  {e.status === "done" && ` · ${e.employee_count} người`}
                  {e.error && (
                    <span className="block text-xs text-red-600 dark:text-red-400">
                      {e.error}
                    </span>
                  )}
                </td>
                <td className="px-4 py-2 text-right">
                  {e.has_file ? (
                    <button
                      onClick={() => void download(e)}
                      className="rounded border border-neutral-300 px-3 py-1 text-sm dark:border-neutral-700"
                    >
                      Tải về
                    </button>
                  ) : e.status === "done" ? (
                    // Tệp chỉ giữ 7 ngày — nói rõ thay vì để một ô trống khó hiểu.
                    <span className="text-xs text-neutral-500">
                      Tệp đã hết hạn
                    </span>
                  ) : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}
