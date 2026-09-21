"use client";

import { useMemo, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { useEmployees } from "@/features/employees/queries";
import {
  useSetStructure,
  useSettings,
  useStructure,
  useStructureHistory,
} from "@/features/payroll/queries";
import {
  COMPONENT_KIND_LABEL,
  formatPercent,
  formatVND,
  type ComponentKind,
  type SalaryComponent,
} from "@/features/payroll/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

export default function SalarySettingsPage() {
  const { can } = usePermission();
  const { data: employeeData } = useEmployees({ page: 1, pageSize: 100 });
  const employees = useMemo(() => employeeData?.items ?? [], [employeeData]);

  const [employeeId, setEmployeeId] = useState("");

  if (!can("salary:read")) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">
          Bạn không có quyền xem cấu hình lương.
        </p>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <h1 className="mb-1 text-xl font-semibold">Cấu hình lương</h1>
      <p className="mb-6 max-w-2xl text-sm text-neutral-500">
        Mỗi lần thay đổi tạo một bản ghi mới có hiệu lực từ một ngày — lịch sử
        lương được giữ nguyên để tính lại kỳ cũ vẫn ra đúng con số cũ.
      </p>

      <TaxSettings />

      <div className="mb-3 mt-8 flex flex-wrap items-center gap-3">
        <h2 className="text-sm font-medium">Lương theo nhân viên</h2>
        <select
          value={employeeId}
          onChange={(e) => setEmployeeId(e.target.value)}
          className="min-w-[220px] rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          <option value="">— Chọn nhân viên —</option>
          {employees.map((e) => (
            <option key={e.id} value={e.id}>
              {e.full_name} ({e.employee_code})
            </option>
          ))}
        </select>
      </div>

      {employeeId ? (
        <EmployeeSalary employeeId={employeeId} canEdit={can("salary:manage")} />
      ) : (
        <p className="rounded border border-neutral-200 px-4 py-8 text-center text-sm text-neutral-500 dark:border-neutral-800">
          Chọn một nhân viên để xem và sửa cấu hình lương.
        </p>
      )}
    </AppShell>
  );
}

/** Tham số tính lương và biểu thuế luỹ tiến — chỉ hiển thị. */
function TaxSettings() {
  const { data: s, isPending, error } = useSettings();

  if (isPending) return <p className="text-sm text-neutral-500">Đang tải...</p>;
  if (error || !s) {
    return <FormError message={errMsg(error, "Không tải được tham số tính lương")} />;
  }

  return (
    <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <h2 className="mb-3 text-sm font-medium">Tham số tính lương</h2>

      <div className="grid gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
        <Field label="Giảm trừ bản thân" value={formatVND(s.personal_deduction)} />
        <Field
          label="Giảm trừ mỗi người phụ thuộc"
          value={formatVND(s.dependent_deduction)}
        />
        <Field label="Ngày công chuẩn / tháng" value={String(s.standard_workdays)} />
        <Field
          label="Bảo hiểm nhân viên đóng"
          value={formatPercent(
            s.social_rate + s.health_rate + s.unemployment_rate,
          )}
        />
        <Field
          label="Bảo hiểm công ty đóng"
          value={formatPercent(
            s.employer_social_rate +
              s.employer_health_rate +
              s.employer_unemployment_rate,
          )}
        />
        <Field label="Trần đóng BHXH/BHYT" value={formatVND(s.social_cap)} />
      </div>

      <h3 className="mb-2 mt-5 text-xs font-semibold text-neutral-600 dark:text-neutral-400">
        BIỂU THUẾ LUỸ TIẾN TỪNG PHẦN
      </h3>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="text-left text-xs text-neutral-500">
            <tr>
              <th className="py-1 font-medium">Bậc</th>
              <th className="py-1 font-medium">Thu nhập tính thuế</th>
              <th className="py-1 text-right font-medium">Thuế suất</th>
            </tr>
          </thead>
          <tbody>
            {s.tax_brackets.map((b) => (
              <tr key={b.ordinal} className="border-t border-neutral-200 dark:border-neutral-800">
                <td className="py-1">{b.ordinal}</td>
                <td className="py-1 tabular-nums">
                  {formatVND(b.from_amount)}
                  {b.to_amount ? ` – ${formatVND(b.to_amount)}` : " trở lên"}
                </td>
                <td className="py-1 text-right tabular-nums">
                  {formatPercent(b.rate)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <p className="mt-3 text-xs text-neutral-500">
        Luỹ tiến <strong>từng phần</strong>: mỗi bậc chỉ áp thuế suất của nó cho
        phần thu nhập nằm trong bậc đó, không áp cho toàn bộ.
      </p>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-4">
      <span className="text-neutral-500">{label}</span>
      <span className="tabular-nums">{value}</span>
    </div>
  );
}

function EmployeeSalary({
  employeeId,
  canEdit,
}: {
  employeeId: string;
  canEdit: boolean;
}) {
  const { data: current, error } = useStructure(employeeId);
  const { data: history = [] } = useStructureHistory(employeeId);
  const save = useSetStructure(employeeId);

  const [open, setOpen] = useState(false);

  const notConfigured =
    error instanceof ApiError && error.status === 404;

  return (
    <div className="space-y-4">
      {notConfigured && (
        <p className="rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300">
          Nhân viên này chưa có cấu hình lương — máy tính lương sẽ bỏ qua họ.
        </p>
      )}

      {current && (
        <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
          <div className="mb-3 flex items-start justify-between">
            <div>
              <div className="text-2xl font-semibold tabular-nums">
                {formatVND(current.base_salary)}
              </div>
              <div className="text-xs text-neutral-500">
                Hiệu lực từ {new Date(current.effective_from).toLocaleDateString("vi-VN")}
                {current.dependents > 0 && ` · ${current.dependents} người phụ thuộc`}
              </div>
            </div>
            {canEdit && (
              <button
                onClick={() => setOpen(true)}
                className="rounded border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700"
              >
                Cập nhật
              </button>
            )}
          </div>

          {current.bank_account && (
            <p className="text-sm text-neutral-500">
              Tài khoản: {current.bank_account} · {current.bank_name}
            </p>
          )}

          {current.components.length > 0 && (
            <table className="mt-3 w-full text-sm">
              <tbody>
                {current.components.map((c) => (
                  <tr key={c.code} className="border-t border-neutral-200 dark:border-neutral-800">
                    <td className="py-1">
                      {c.name}
                      <span className="ml-2 text-xs text-neutral-500">
                        {COMPONENT_KIND_LABEL[c.kind]}
                        {!c.taxable && " · không chịu thuế"}
                        {c.prorated && " · chia theo công"}
                      </span>
                    </td>
                    <td className="py-1 text-right tabular-nums">
                      {formatVND(c.amount)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {!current && canEdit && (
        <button
          onClick={() => setOpen(true)}
          className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
        >
          Thiết lập lương
        </button>
      )}

      {history.length > 1 && (
        <div>
          <h3 className="mb-2 text-sm font-medium">Lịch sử lương</h3>
          <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
            <table className="w-full text-sm">
              <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
                <tr>
                  <th className="px-4 py-2 font-medium">Từ ngày</th>
                  <th className="px-4 py-2 font-medium">Đến ngày</th>
                  <th className="px-4 py-2 text-right font-medium">Lương cơ bản</th>
                  <th className="px-4 py-2 font-medium">Ghi chú</th>
                </tr>
              </thead>
              <tbody>
                {history.map((h) => (
                  <tr key={h.id} className="border-t border-neutral-200 dark:border-neutral-800">
                    <td className="px-4 py-2">
                      {new Date(h.effective_from).toLocaleDateString("vi-VN")}
                    </td>
                    <td className="px-4 py-2 text-neutral-500">
                      {h.effective_to
                        ? new Date(h.effective_to).toLocaleDateString("vi-VN")
                        : "đang áp dụng"}
                    </td>
                    <td className="px-4 py-2 text-right tabular-nums">
                      {formatVND(h.base_salary)}
                    </td>
                    <td className="px-4 py-2 text-neutral-500">{h.note || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {open && (
        <SalaryDialog
          employeeId={employeeId}
          current={current}
          onClose={() => setOpen(false)}
          save={save}
        />
      )}
    </div>
  );
}

function SalaryDialog({
  current,
  onClose,
  save,
}: {
  employeeId: string;
  current?: { base_salary: number; dependents: number; components: SalaryComponent[] };
  onClose: () => void;
  save: ReturnType<typeof useSetStructure>;
}) {
  const [base, setBase] = useState(String(current?.base_salary ?? ""));
  const [dependents, setDependents] = useState(String(current?.dependents ?? 0));
  const [effectiveFrom, setEffectiveFrom] = useState(
    new Date().toISOString().slice(0, 10),
  );
  const [note, setNote] = useState("");
  const [components, setComponents] = useState<SalaryComponent[]>(
    current?.components ?? [],
  );

  function addComponent() {
    setComponents((c) => [
      ...c,
      { kind: "allowance", code: "", name: "", amount: 0, taxable: true, prorated: false },
    ]);
  }

  function update(i: number, patch: Partial<SalaryComponent>) {
    setComponents((c) => c.map((x, j) => (j === i ? { ...x, ...patch } : x)));
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="my-8 w-full max-w-lg rounded-lg border border-neutral-200 bg-white p-6 shadow-lg dark:border-neutral-800 dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="mb-4 text-lg font-semibold">Cập nhật lương</h2>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate(
              {
                base_salary: Number(base) || 0,
                dependents: Number(dependents) || 0,
                effective_from: effectiveFrom,
                note,
                components: components.filter((c) => c.code && c.name),
              },
              { onSuccess: onClose },
            );
          }}
          className="space-y-4"
        >
          <div className="grid grid-cols-2 gap-3">
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Lương cơ bản</span>
              <input
                type="number"
                min="0"
                step="100000"
                value={base}
                onChange={(e) => setBase(e.target.value)}
                required
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm tabular-nums dark:border-neutral-700 dark:bg-neutral-950"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Người phụ thuộc</span>
              <input
                type="number"
                min="0"
                max="20"
                value={dependents}
                onChange={(e) => setDependents(e.target.value)}
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm tabular-nums dark:border-neutral-700 dark:bg-neutral-950"
              />
            </label>
          </div>

          <label className="block text-sm">
            <span className="mb-1 block font-medium">Hiệu lực từ</span>
            <input
              type="date"
              value={effectiveFrom}
              onChange={(e) => setEffectiveFrom(e.target.value)}
              required
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>

          <div>
            <div className="mb-2 flex items-center justify-between">
              <span className="text-sm font-medium">Phụ cấp, thưởng, khấu trừ</span>
              <button
                type="button"
                onClick={addComponent}
                className="text-xs underline hover:no-underline"
              >
                Thêm dòng
              </button>
            </div>

            <div className="space-y-2">
              {components.map((c, i) => (
                <div key={i} className="grid grid-cols-12 gap-1 text-xs">
                  <select
                    value={c.kind}
                    onChange={(e) => update(i, { kind: e.target.value as ComponentKind })}
                    className="col-span-3 rounded border border-neutral-300 px-1 py-1 dark:border-neutral-700 dark:bg-neutral-950"
                  >
                    {Object.entries(COMPONENT_KIND_LABEL).map(([v, label]) => (
                      <option key={v} value={v}>
                        {label}
                      </option>
                    ))}
                  </select>
                  <input
                    value={c.code}
                    onChange={(e) => update(i, { code: e.target.value.toUpperCase() })}
                    placeholder="MÃ"
                    className="col-span-2 rounded border border-neutral-300 px-1 py-1 dark:border-neutral-700 dark:bg-neutral-950"
                  />
                  <input
                    value={c.name}
                    onChange={(e) => update(i, { name: e.target.value })}
                    placeholder="Tên khoản"
                    className="col-span-4 rounded border border-neutral-300 px-1 py-1 dark:border-neutral-700 dark:bg-neutral-950"
                  />
                  <input
                    type="number"
                    value={c.amount}
                    onChange={(e) => update(i, { amount: Number(e.target.value) })}
                    className="col-span-3 rounded border border-neutral-300 px-1 py-1 tabular-nums dark:border-neutral-700 dark:bg-neutral-950"
                  />

                  <label className="col-span-5 flex items-center gap-1 text-neutral-500">
                    <input
                      type="checkbox"
                      checked={c.taxable}
                      onChange={(e) => update(i, { taxable: e.target.checked })}
                    />
                    chịu thuế
                  </label>
                  <label className="col-span-5 flex items-center gap-1 text-neutral-500">
                    <input
                      type="checkbox"
                      checked={c.prorated}
                      onChange={(e) => update(i, { prorated: e.target.checked })}
                    />
                    chia theo công
                  </label>
                  <button
                    type="button"
                    onClick={() => setComponents((x) => x.filter((_, j) => j !== i))}
                    className="col-span-2 text-neutral-400 hover:text-red-600"
                  >
                    Xoá
                  </button>
                </div>
              ))}
            </div>
          </div>

          <label className="block text-sm">
            <span className="mb-1 block font-medium">Ghi chú</span>
            <input
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="VD: tăng lương định kỳ"
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>

          <p className="text-xs text-neutral-500">
            Không nhập lại thông tin ngân hàng thì hệ thống giữ nguyên thông tin
            cũ.
          </p>

          <FormError message={errMsg(save.error, "Không lưu được cấu hình lương")} />

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
              disabled={save.isPending}
              className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
            >
              {save.isPending ? "Đang lưu..." : "Lưu"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
