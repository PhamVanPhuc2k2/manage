"use client";

import { useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useCostByMonth,
  useCreatePeriod,
  usePeriods,
} from "@/features/payroll/queries";
import {
  PAYROLL_STATUS_CLASS,
  PAYROLL_STATUS_LABEL,
  formatVND,
  formatVNDShort,
  monthLabel,
} from "@/features/payroll/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

/**
 * Biểu đồ cột chi phí nhân sự theo tháng.
 *
 * Vẽ bằng div thay vì thêm thư viện biểu đồ: ở đây chỉ cần 12 cột có chiều
 * cao tỷ lệ với con số. Một thư viện biểu đồ đầy đủ mang theo trục, chú
 * giải, tooltip, hoạt ảnh và vài trăm KB cho những thứ màn hình này không
 * dùng tới.
 */
function CostChart({ year }: { year: number }) {
  const { data: rows = [], isPending } = useCostByMonth(year);

  if (isPending) {
    return <p className="text-sm text-neutral-500">Đang tải biểu đồ...</p>;
  }
  if (rows.length === 0) {
    return (
      <p className="rounded border border-neutral-200 px-4 py-6 text-center text-sm text-neutral-400 dark:border-neutral-800">
        Chưa có kỳ lương nào trong năm {year}.
      </p>
    );
  }

  const max = Math.max(...rows.map((r) => r.total_cost), 1);

  return (
    <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="flex h-40 items-end gap-2">
        {rows.map((r) => {
          const pct = (r.total_cost / max) * 100;
          return (
            <div key={r.key} className="flex flex-1 flex-col items-center gap-1">
              <span className="text-[11px] text-neutral-500">
                {formatVNDShort(r.total_cost)}
              </span>
              <div
                title={`${r.label}: ${formatVND(r.total_cost)} (${r.employee_count} người)`}
                style={{ height: `${Math.max(pct, 2)}%` }}
                className="w-full rounded-t bg-neutral-900 transition-all dark:bg-white"
              />
              <span className="text-[11px] text-neutral-500">
                {r.key.slice(5)}
              </span>
            </div>
          );
        })}
      </div>

      <p className="mt-3 border-t border-neutral-200 pt-2 text-xs text-neutral-500 dark:border-neutral-800">
        Chi phí thật của công ty — gồm cả phần bảo hiểm công ty đóng, khoản
        không xuất hiện trên phiếu lương của ai nhưng vẫn ra khỏi tài khoản
        mỗi tháng.
      </p>
    </div>
  );
}

export default function PayrollPage() {
  const { can } = usePermission();
  const now = new Date();

  const [year, setYear] = useState(now.getFullYear());
  const [formOpen, setFormOpen] = useState(false);

  const { data: periods = [], isPending, error } = usePeriods();
  const create = useCreatePeriod();

  // Tháng trước — hệ thống từ chối tạo kỳ cho tháng chưa kết thúc.
  const prev = new Date(now.getFullYear(), now.getMonth() - 1, 1);
  const [newYear, setNewYear] = useState(prev.getFullYear());
  const [newMonth, setNewMonth] = useState(prev.getMonth() + 1);

  if (!can("payroll:read_all")) {
    return (
      <AppShell>
        <h1 className="mb-2 text-xl font-semibold">Lương</h1>
        <p className="mb-4 text-sm text-neutral-500">
          Bạn chỉ xem được phiếu lương của chính mình.
        </p>
        <Link href="/payroll/my" className="text-sm underline">
          Xem phiếu lương của tôi
        </Link>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-semibold">Kỳ lương</h1>

        {can("payroll:manage") && (
          <button
            onClick={() => setFormOpen(true)}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Tạo kỳ lương
          </button>
        )}
      </div>

      <FormError
        message={
          errMsg(error, "Không tải được danh sách kỳ lương") ??
          errMsg(create.error, "Không tạo được kỳ lương")
        }
      />

      {/* --- Biểu đồ chi phí --- */}
      <div className="mb-8">
        <div className="mb-3 flex items-center gap-3">
          <h2 className="text-sm font-medium">Chi phí nhân sự</h2>
          <select
            value={year}
            onChange={(e) => setYear(Number(e.target.value))}
            className="rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
          >
            {[now.getFullYear(), now.getFullYear() - 1, now.getFullYear() - 2].map(
              (y) => (
                <option key={y} value={y}>
                  {y}
                </option>
              ),
            )}
          </select>
        </div>
        <CostChart year={year} />
      </div>

      {/* --- Danh sách kỳ --- */}
      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Kỳ</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
              <th className="px-4 py-2 font-medium">Số người</th>
              <th className="px-4 py-2 text-right font-medium">Tổng gộp</th>
              <th className="px-4 py-2 text-right font-medium">Tổng thực nhận</th>
              <th className="px-4 py-2 text-right font-medium">Thuế TNCN</th>
            </tr>
          </thead>
          <tbody>
            {isPending && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!isPending && periods.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có kỳ lương nào.
                </td>
              </tr>
            )}
            {periods.map((p) => (
              <tr
                key={p.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2">
                  <Link
                    href={`/payroll/${p.id}`}
                    className="font-medium hover:underline"
                  >
                    {monthLabel(p)}
                  </Link>
                </td>
                <td className="px-4 py-2">
                  <span
                    className={`rounded px-2 py-0.5 text-xs ${PAYROLL_STATUS_CLASS[p.status]}`}
                  >
                    {PAYROLL_STATUS_LABEL[p.status]}
                  </span>
                </td>
                <td className="px-4 py-2 tabular-nums">{p.employee_count}</td>
                <td className="px-4 py-2 text-right tabular-nums">
                  {formatVND(p.total_gross)}
                </td>
                <td className="px-4 py-2 text-right font-medium tabular-nums">
                  {formatVND(p.total_net)}
                </td>
                <td className="px-4 py-2 text-right tabular-nums text-neutral-500">
                  {formatVND(p.total_tax)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {formOpen && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
          onClick={() => setFormOpen(false)}
        >
          <div
            className="w-full max-w-sm rounded-lg border border-neutral-200 bg-white p-6 shadow-lg dark:border-neutral-800 dark:bg-neutral-900"
            onClick={(e) => e.stopPropagation()}
          >
            <h2 className="mb-4 text-lg font-semibold">Tạo kỳ lương</h2>

            <form
              onSubmit={(e) => {
                e.preventDefault();
                create.mutate(
                  { year: newYear, month: newMonth },
                  { onSuccess: () => setFormOpen(false) },
                );
              }}
              className="space-y-4"
            >
              <div className="grid grid-cols-2 gap-3">
                <label className="block text-sm">
                  <span className="mb-1 block font-medium">Tháng</span>
                  <select
                    value={newMonth}
                    onChange={(e) => setNewMonth(Number(e.target.value))}
                    className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
                  >
                    {Array.from({ length: 12 }, (_, i) => i + 1).map((m) => (
                      <option key={m} value={m}>
                        Tháng {m}
                      </option>
                    ))}
                  </select>
                </label>
                <label className="block text-sm">
                  <span className="mb-1 block font-medium">Năm</span>
                  <select
                    value={newYear}
                    onChange={(e) => setNewYear(Number(e.target.value))}
                    className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
                  >
                    {[now.getFullYear(), now.getFullYear() - 1].map((y) => (
                      <option key={y} value={y}>
                        {y}
                      </option>
                    ))}
                  </select>
                </label>
              </div>

              <p className="text-xs text-neutral-500">
                Chỉ tạo được kỳ cho tháng đã kết thúc — dữ liệu công của tháng
                đang chạy còn thay đổi mỗi ngày.
              </p>

              <FormError message={errMsg(create.error, "Không tạo được kỳ lương")} />

              <div className="flex justify-end gap-2">
                <button
                  type="button"
                  onClick={() => setFormOpen(false)}
                  className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
                >
                  Huỷ
                </button>
                <button
                  type="submit"
                  disabled={create.isPending}
                  className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
                >
                  {create.isPending ? "Đang tạo..." : "Tạo"}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </AppShell>
  );
}
