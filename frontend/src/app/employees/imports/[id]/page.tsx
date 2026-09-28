"use client";

import { useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";

import { AppShell } from "@/components/AppShell";
import { useCrumbLabel } from "@/components/Breadcrumb";
import { IMPORT_STATUS_LABEL, useImport } from "@/features/employees/imports";
import { ApiError } from "@/lib/api-client";

export default function EmployeeImportResultPage() {
  const { id } = useParams<{ id: string }>();
  const { data: imp, isPending, error } = useImport(id);
  useCrumbLabel(`/employees/imports/${id}`, imp?.file_name);

  // Mặc định chỉ hiện dòng cần xử lý: người nhập 300 dòng mà có 4 dòng lỗi
  // cần thấy ngay 4 dòng đó, không phải cuộn qua 296 dòng xanh.
  const [onlyProblems, setOnlyProblems] = useState(true);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải...</p>
      </AppShell>
    );
  }
  if (error || !imp) {
    return (
      <AppShell>
        <p className="text-sm text-red-600">
          {error instanceof ApiError
            ? error.message
            : "Không tải được lượt nhập"}
        </p>
      </AppShell>
    );
  }

  const running = imp.status === "queued" || imp.status === "processing";
  const results = imp.results ?? [];
  const problems = results.filter((r) => r.error || r.warning);
  const shown = onlyProblems ? problems : results;
  const percent = imp.total_rows
    ? Math.round((imp.processed / imp.total_rows) * 100)
    : 0;

  return (
    <AppShell>
      <div className="mb-6">
        <Link
          href="/employees/imports"
          className="text-sm text-neutral-500 hover:underline"
        >
          ← Nhập nhân viên từ tệp
        </Link>
        <h1 className="mt-2 text-xl font-semibold">{imp.file_name}</h1>
        <p className="mt-1 flex items-center gap-1 text-sm text-neutral-500">
          <span
            className={`rounded px-1.5 py-0.5 text-xs font-medium ${
              imp.status === "done"
                ? "bg-emerald-100 text-emerald-700 dark:bg-emerald-950 dark:text-emerald-300"
                : imp.status === "failed"
                  ? "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-300"
                  : "bg-neutral-100 text-neutral-700 dark:bg-neutral-800 dark:text-neutral-300"
            }`}
          >
            {IMPORT_STATUS_LABEL[imp.status]}
          </span>
          {imp.create_accounts && " · kèm tạo tài khoản"}
          {imp.creator_name && ` · ${imp.creator_name}`}
        </p>
      </div>

      {imp.error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {imp.error}
        </div>
      )}

      {running && (
        <div className="mb-6">
          <div className="mb-1 text-sm">
            Đang nhập {imp.processed}/{imp.total_rows} dòng — có thể rời trang
            này, bạn sẽ nhận thông báo khi xong.
          </div>
          <div
            role="progressbar"
            aria-valuenow={percent}
            aria-valuemin={0}
            aria-valuemax={100}
            className="h-2 overflow-hidden rounded bg-neutral-200 dark:bg-neutral-800"
          >
            <div
              className="h-full bg-emerald-500 transition-[width]"
              style={{ width: `${percent}%` }}
            />
          </div>
        </div>
      )}

      <div className="mb-6 grid grid-cols-3 gap-3 text-sm sm:max-w-md">
        <Stat label="Tổng số dòng" value={imp.total_rows} />
        <Stat label="Thành công" value={imp.succeeded} tone="ok" />
        <Stat
          label="Lỗi"
          value={imp.failed}
          tone={imp.failed > 0 ? "bad" : undefined}
        />
      </div>

      <div className="mb-3 flex items-center justify-between">
        <h2 className="font-medium">Chi tiết từng dòng</h2>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={onlyProblems}
            onChange={(e) => setOnlyProblems(e.target.checked)}
          />
          Chỉ hiện dòng lỗi hoặc cảnh báo
        </label>
      </div>

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Dòng</th>
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Họ tên</th>
              <th className="px-4 py-2 font-medium">Kết quả</th>
            </tr>
          </thead>
          <tbody>
            {shown.length === 0 && (
              <tr>
                <td
                  colSpan={4}
                  className="px-4 py-6 text-center text-neutral-500"
                >
                  {running
                    ? "Chưa có dòng nào cần xem"
                    : onlyProblems
                      ? "Không có dòng lỗi nào"
                      : "Không có dòng nào"}
                </td>
              </tr>
            )}
            {shown.map((r) => (
              <tr
                key={r.line}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2 font-mono text-xs">{r.line}</td>
                <td className="px-4 py-2 font-mono text-xs">
                  {r.employee_code || "—"}
                </td>
                <td className="px-4 py-2">
                  {r.employee_id ? (
                    <Link
                      href={`/employees/${r.employee_id}`}
                      className="underline-offset-4 hover:underline"
                    >
                      {r.full_name}
                    </Link>
                  ) : (
                    r.full_name || "—"
                  )}
                </td>
                <td className="px-4 py-2">
                  {r.error ? (
                    <span className="text-red-600 dark:text-red-400">
                      {r.error}
                    </span>
                  ) : r.warning ? (
                    <span className="text-amber-600 dark:text-amber-400">
                      {r.warning}
                    </span>
                  ) : (
                    <span className="text-emerald-600 dark:text-emerald-400">
                      Đã tạo{r.account_created && " · có tài khoản"}
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}

function Stat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "ok" | "bad";
}) {
  const color =
    tone === "ok"
      ? "text-emerald-600 dark:text-emerald-400"
      : tone === "bad"
        ? "text-red-600 dark:text-red-400"
        : "";
  return (
    <div className="rounded border border-neutral-200 px-3 py-2 dark:border-neutral-800">
      <div className="text-xs text-neutral-500">{label}</div>
      <div className={`text-lg font-semibold ${color}`}>{value}</div>
    </div>
  );
}
