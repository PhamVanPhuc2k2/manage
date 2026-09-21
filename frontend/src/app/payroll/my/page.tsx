"use client";

import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { useMyPayslips, usePayslip } from "@/features/payroll/queries";
import {
  COMPONENT_KIND_LABEL,
  formatVND,
  monthLabel,
  type Payslip,
} from "@/features/payroll/types";
import { ApiError } from "@/lib/api-client";

const API_BASE = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost/api/v1";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

export default function MyPayslipsPage() {
  const { data: slips = [], isPending, error } = useMyPayslips();
  const [openId, setOpenId] = useState<string | null>(null);

  return (
    <AppShell>
      <h1 className="mb-1 text-xl font-semibold">Phiếu lương của tôi</h1>
      <p className="mb-6 max-w-2xl text-sm text-neutral-500">
        Chỉ hiện phiếu của những kỳ đã chốt. Thấy số liệu chưa đúng, liên hệ bộ
        phận nhân sự trước ngày 10 của tháng kế tiếp.
      </p>

      <FormError message={errMsg(error, "Không tải được phiếu lương")} />

      {isPending && <p className="text-sm text-neutral-500">Đang tải...</p>}

      {!isPending && slips.length === 0 && (
        <p className="rounded border border-neutral-200 px-4 py-8 text-center text-sm text-neutral-500 dark:border-neutral-800">
          Chưa có phiếu lương nào.
        </p>
      )}

      <div className="space-y-3">
        {slips.map((s) => (
          <div
            key={s.id}
            className="rounded border border-neutral-200 dark:border-neutral-800"
          >
            <button
              onClick={() => setOpenId(openId === s.id ? null : s.id)}
              className="flex w-full items-center justify-between gap-4 px-4 py-3 text-left transition hover:bg-neutral-50 dark:hover:bg-neutral-900"
            >
              <div>
                <div className="font-medium">
                  {monthLabel({ year: s.period_year, month: s.period_month })}
                </div>
                <div className="text-xs text-neutral-500">
                  {s.actual_workdays}/{s.standard_workdays} ngày công
                </div>
              </div>

              <div className="text-right">
                <div className="font-semibold tabular-nums">
                  {formatVND(s.net_salary)}
                </div>
                <div className="text-xs text-neutral-500">thực nhận</div>
              </div>
            </button>

            {openId === s.id && <PayslipDetail id={s.id} />}
          </div>
        ))}
      </div>
    </AppShell>
  );
}

function PayslipDetail({ id }: { id: string }) {
  const { data: s, isPending } = usePayslip(id);

  if (isPending) {
    return (
      <div className="border-t border-neutral-200 px-4 py-3 text-sm text-neutral-500 dark:border-neutral-800">
        Đang tải chi tiết...
      </div>
    );
  }
  if (!s) return null;

  return (
    <div className="border-t border-neutral-200 px-4 py-3 dark:border-neutral-800">
      <Breakdown s={s} />

      <a
        href={`${API_BASE}/payroll/payslips/${s.id}/document`}
        target="_blank"
        rel="noopener noreferrer"
        className="mt-4 inline-block rounded border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700"
      >
        Mở phiếu để in / lưu PDF
      </a>
    </div>
  );
}

/**
 * Bảng phân tích phiếu lương.
 *
 * Hiện ĐẦY ĐỦ cơ sở tính thuế chứ không chỉ con số cuối. Chỉ đưa số thuế mà
 * không giải thích thì mọi thắc mắc đều phải hỏi kế toán, và kế toán sẽ phải
 * trả lời cùng một câu hỏi cho từng người.
 */
function Breakdown({ s }: { s: Payslip }) {
  const income = s.items.filter((i) => i.kind !== "deduction");
  const deductions = s.items.filter((i) => i.kind === "deduction");

  return (
    <table className="w-full text-sm">
      <tbody>
        <Section title="THU NHẬP" />
        {income.map((i) => (
          <Row
            key={i.code}
            label={i.name + (i.taxable ? "" : " (không chịu thuế)")}
            value={formatVND(i.amount)}
            indent
          />
        ))}
        <Row label="Tổng thu nhập" value={formatVND(s.gross_salary)} bold />

        <Section title="KHẤU TRỪ" />
        <Row
          label={`Bảo hiểm bắt buộc (trên ${formatVND(s.insurance_base)})`}
          value={formatVND(s.insurance_employee)}
          indent
        />
        <Row label="Thuế thu nhập cá nhân" value={formatVND(s.income_tax)} indent />
        {deductions.map((i) => (
          <Row
            key={i.code}
            label={`${COMPONENT_KIND_LABEL[i.kind]}: ${i.name}`}
            value={formatVND(i.amount)}
            indent
          />
        ))}

        <Section title="CƠ SỞ TÍNH THUẾ" />
        <Row label="Thu nhập chịu thuế" value={formatVND(s.taxable_income)} indent />
        <Row
          label="Giảm trừ bản thân"
          value={`-${formatVND(s.personal_deduction)}`}
          indent
        />
        {s.dependent_deduction > 0 && (
          <Row
            label={`Giảm trừ ${s.dependents} người phụ thuộc`}
            value={`-${formatVND(s.dependent_deduction)}`}
            indent
          />
        )}
        <Row
          label="Thu nhập tính thuế"
          value={formatVND(s.assessable_income)}
          indent
        />

        <tr className="border-t-2 border-neutral-900 dark:border-neutral-100">
          <td className="py-2 font-bold">THỰC NHẬN</td>
          <td className="py-2 text-right text-base font-bold tabular-nums">
            {formatVND(s.net_salary)}
          </td>
        </tr>
      </tbody>
    </table>
  );
}

function Section({ title }: { title: string }) {
  return (
    <tr>
      <td
        colSpan={2}
        className="border-b border-neutral-200 pb-1 pt-4 text-xs font-semibold text-neutral-600 dark:border-neutral-800 dark:text-neutral-400"
      >
        {title}
      </td>
    </tr>
  );
}

function Row({
  label,
  value,
  indent,
  bold,
}: {
  label: string;
  value: string;
  indent?: boolean;
  bold?: boolean;
}) {
  return (
    <tr>
      <td
        className={`py-1 ${indent ? "pl-3 text-neutral-600 dark:text-neutral-400" : ""} ${
          bold ? "font-medium" : ""
        }`}
      >
        {label}
      </td>
      <td className={`py-1 text-right tabular-nums ${bold ? "font-medium" : ""}`}>
        {value}
      </td>
    </tr>
  );
}
