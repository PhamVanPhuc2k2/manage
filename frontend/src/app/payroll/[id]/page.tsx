"use client";

import { use, useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useChangePeriodStatus,
  useCostByDepartment,
  useCalculatePeriod,
  usePayslips,
  usePeriod,
  useUpdatePayslip,
} from "@/features/payroll/queries";
import {
  PAYROLL_STATUS_CLASS,
  PAYROLL_STATUS_LABEL,
  formatVND,
  monthLabel,
  type Payslip,
} from "@/features/payroll/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost/api/v1";

export default function PayrollPeriodPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  const { can } = usePermission();

  const { data: period, isPending, error } = usePeriod(id);
  const { data: slipData } = usePayslips(id);
  const { data: costRows = [] } = useCostByDepartment(id);

  const calculate = useCalculatePeriod();
  const changeStatus = useChangePeriodStatus();
  const [editing, setEditing] = useState<Payslip | null>(null);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải...</p>
      </AppShell>
    );
  }
  if (error || !period) {
    return (
      <AppShell>
        <FormError message={errMsg(error, "Không tải được kỳ lương")} />
        <Link href="/payroll" className="mt-3 inline-block text-sm underline">
          Về danh sách kỳ lương
        </Link>
      </AppShell>
    );
  }

  const slips = slipData?.items ?? [];
  const calculating = period.status === "calculating";
  const editable = period.status === "draft";

  const actionError =
    errMsg(calculate.error, "Không chạy được máy tính lương") ??
    errMsg(changeStatus.error, "Không đổi được trạng thái kỳ lương");

  return (
    <AppShell>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <Link href="/payroll" className="text-xs text-neutral-500 hover:underline">
            ← Kỳ lương
          </Link>
          <h1 className="mt-1 flex items-center gap-3 text-xl font-semibold">
            {monthLabel(period)}
            <span
              className={`rounded px-2 py-0.5 text-xs font-normal ${PAYROLL_STATUS_CLASS[period.status]}`}
            >
              {PAYROLL_STATUS_LABEL[period.status]}
            </span>
          </h1>
        </div>

        <div className="flex flex-wrap gap-2">
          {can("payroll:manage") && editable && (
            <button
              onClick={() => calculate.mutate(id)}
              disabled={calculate.isPending}
              className="rounded border border-neutral-300 px-4 py-2 text-sm disabled:opacity-50 dark:border-neutral-700"
            >
              {slips.length > 0 ? "Tính lại" : "Chạy tính lương"}
            </button>
          )}

          {/* Khoá kỳ và xác nhận đã trả cần quyền RIÊNG — người chạy tính
              lương không được tự chốt kỳ mình vừa chạy. */}
          {can("payroll:approve") && period.status === "draft" && (
            <button
              onClick={() => changeStatus.mutate({ id, status: "locked" })}
              className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
            >
              Khoá kỳ
            </button>
          )}
          {can("payroll:approve") && period.status === "locked" && (
            <>
              <button
                onClick={() => changeStatus.mutate({ id, status: "draft" })}
                className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
              >
                Mở lại
              </button>
              <button
                onClick={() => {
                  if (confirm("Xác nhận đã trả lương kỳ này? Sau đó không sửa lại được."))
                    changeStatus.mutate({ id, status: "paid" });
                }}
                className="rounded bg-green-700 px-4 py-2 text-sm font-medium text-white"
              >
                Xác nhận đã trả
              </button>
            </>
          )}
        </div>
      </div>

      <FormError message={actionError} />

      {calculating && (
        <div className="mb-4 rounded border border-blue-300 bg-blue-50 px-3 py-2 text-sm text-blue-800 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300">
          Đang tính lương ở tiến trình nền. Trang tự cập nhật khi xong — không
          cần bấm lại.
        </div>
      )}

      {period.status === "paid" && (
        <div className="mb-4 rounded border border-neutral-300 bg-neutral-50 px-3 py-2 text-sm text-neutral-700 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300">
          Kỳ lương đã chốt và đã trả. Mọi điều chỉnh sau thời điểm này phải
          thực hiện ở kỳ kế tiếp.
        </div>
      )}

      {/* --- Số tổng --- */}
      <div className="mb-6 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <Stat label="Số người" value={String(period.employee_count)} />
        <Stat label="Tổng lương gộp" value={formatVND(period.total_gross)} />
        <Stat label="Tổng thực nhận" value={formatVND(period.total_net)} strong />
        <Stat label="Thuế TNCN" value={formatVND(period.total_tax)} />
      </div>

      {/* --- Chi phí theo phòng ban --- */}
      {costRows.length > 0 && (
        <div className="mb-8">
          <h2 className="mb-3 text-sm font-medium">Chi phí theo phòng ban</h2>
          <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
            <table className="w-full text-sm">
              <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
                <tr>
                  <th className="px-4 py-2 font-medium">Phòng ban</th>
                  <th className="px-4 py-2 font-medium">Số người</th>
                  <th className="px-4 py-2 text-right font-medium">Lương gộp</th>
                  <th className="px-4 py-2 text-right font-medium">BH công ty đóng</th>
                  <th className="px-4 py-2 text-right font-medium">Chi phí thật</th>
                </tr>
              </thead>
              <tbody>
                {costRows.map((c) => (
                  <tr
                    key={c.key}
                    className="border-t border-neutral-200 dark:border-neutral-800"
                  >
                    <td className="px-4 py-2">{c.label}</td>
                    <td className="px-4 py-2 tabular-nums">{c.employee_count}</td>
                    <td className="px-4 py-2 text-right tabular-nums">
                      {formatVND(c.total_gross)}
                    </td>
                    <td className="px-4 py-2 text-right tabular-nums text-neutral-500">
                      {formatVND(c.insurance_employer)}
                    </td>
                    <td className="px-4 py-2 text-right font-medium tabular-nums">
                      {formatVND(c.total_cost)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* --- Bảng lương chi tiết --- */}
      <h2 className="mb-3 text-sm font-medium">Bảng lương chi tiết</h2>

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-3 py-2 font-medium">Nhân viên</th>
              <th className="px-3 py-2 font-medium">Công</th>
              <th className="px-3 py-2 text-right font-medium">Lương gộp</th>
              <th className="px-3 py-2 text-right font-medium">Bảo hiểm</th>
              <th className="px-3 py-2 text-right font-medium">Thuế</th>
              <th className="px-3 py-2 text-right font-medium">Thực nhận</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody>
            {slips.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-8 text-center text-neutral-500">
                  {calculating
                    ? "Đang tính..."
                    : "Chưa có phiếu lương. Bấm “Chạy tính lương” để bắt đầu."}
                </td>
              </tr>
            )}
            {slips.map((s) => (
              <tr
                key={s.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-3 py-2">
                  <div>{s.employee_name}</div>
                  <div className="text-xs text-neutral-500">
                    {s.employee_code}
                    {s.department_name && ` · ${s.department_name}`}
                  </div>
                </td>
                <td className="px-3 py-2 tabular-nums">
                  {s.actual_workdays}/{s.standard_workdays}
                </td>
                <td className="px-3 py-2 text-right tabular-nums">
                  {formatVND(s.gross_salary)}
                </td>
                <td className="px-3 py-2 text-right tabular-nums text-neutral-500">
                  {formatVND(s.insurance_employee)}
                </td>
                <td className="px-3 py-2 text-right tabular-nums text-neutral-500">
                  {formatVND(s.income_tax)}
                </td>
                <td className="px-3 py-2 text-right font-medium tabular-nums">
                  {formatVND(s.net_salary)}
                </td>
                <td className="px-3 py-2 text-right">
                  <div className="flex justify-end gap-2">
                    <a
                      href={`${API_BASE}/payroll/payslips/${s.id}/document`}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-xs underline hover:no-underline"
                    >
                      Phiếu
                    </a>
                    {can("payroll:manage") && editable && (
                      <button
                        onClick={() => setEditing(s)}
                        className="text-xs underline hover:no-underline"
                      >
                        Sửa
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {editing && (
        <EditPayslipDialog slip={editing} onClose={() => setEditing(null)} />
      )}
    </AppShell>
  );
}

function Stat({
  label,
  value,
  strong,
}: {
  label: string;
  value: string;
  strong?: boolean;
}) {
  return (
    <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="text-xs text-neutral-500">{label}</div>
      <div
        className={`mt-1 tabular-nums ${strong ? "text-xl font-semibold" : "text-lg"}`}
      >
        {value}
      </div>
    </div>
  );
}

/**
 * Sửa phiếu lương khi kỳ còn nháp.
 *
 * Chỉ cho sửa thưởng, khấu trừ khác và ghi chú — những con số CÓ THỂ có
 * ngoại lệ thật. Thuế và bảo hiểm không sửa tay được: chúng là kết quả tính
 * từ quy định, và sửa tay sẽ tạo ra phiếu lương không giải thích được.
 */
function EditPayslipDialog({
  slip,
  onClose,
}: {
  slip: Payslip;
  onClose: () => void;
}) {
  const update = useUpdatePayslip();
  const [bonuses, setBonuses] = useState(String(slip.bonuses));
  const [deductions, setDeductions] = useState(String(slip.other_deductions));
  const [note, setNote] = useState(slip.note ?? "");

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-6 shadow-lg dark:border-neutral-800 dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-1 text-lg font-semibold">Sửa phiếu lương</h2>
        <p className="mb-4 text-sm text-neutral-500">{slip.employee_name}</p>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            update.mutate(
              {
                id: slip.id,
                body: {
                  bonuses: Number(bonuses) || 0,
                  other_deductions: Number(deductions) || 0,
                  note,
                },
              },
              { onSuccess: onClose },
            );
          }}
          className="space-y-4"
        >
          <label className="block text-sm">
            <span className="mb-1 block font-medium">Thưởng</span>
            <input
              type="number"
              min="0"
              step="1000"
              value={bonuses}
              onChange={(e) => setBonuses(e.target.value)}
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm tabular-nums dark:border-neutral-700 dark:bg-neutral-950"
            />
            <span className="mt-1 block text-xs text-neutral-500">
              Thuế được tính lại theo biểu luỹ tiến sau khi đổi thưởng.
            </span>
          </label>

          <label className="block text-sm">
            <span className="mb-1 block font-medium">Khấu trừ khác</span>
            <input
              type="number"
              min="0"
              step="1000"
              value={deductions}
              onChange={(e) => setDeductions(e.target.value)}
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm tabular-nums dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>

          <label className="block text-sm">
            <span className="mb-1 block font-medium">Ghi chú</span>
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="Lý do điều chỉnh"
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>

          <FormError message={errMsg(update.error, "Không sửa được phiếu lương")} />

          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
            >
              Huỷ
            </button>
            <button
              type="submit"
              disabled={update.isPending}
              className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
            >
              {update.isPending ? "Đang lưu..." : "Lưu"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
