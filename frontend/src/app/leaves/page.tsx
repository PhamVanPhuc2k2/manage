"use client";

import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useCancelLeave,
  useCreateLeave,
  useDecideLeave,
  useLeaveBalance,
  useLeaves,
} from "@/features/attendance/queries";
import {
  APPROVAL_CLASS,
  APPROVAL_LABEL,
  DAY_PART_LABEL,
  LEAVE_TYPE_LABEL,
  formatDate,
  type DayPart,
  type LeaveRequest,
  type LeaveType,
} from "@/features/attendance/types";
import { ApiError } from "@/lib/api-client";
import { useAuthStore } from "@/lib/auth/store";
import { usePermission } from "@/lib/auth/useAuth";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

type Tab = "mine" | "pending";

export default function LeavesPage() {
  const { can } = usePermission();
  const myEmployeeId = useAuthStore((s) => s.user?.employee_id);
  const year = new Date().getFullYear();

  const canApprove = can("leave:approve");
  const [tab, setTab] = useState<Tab>("mine");
  const [formOpen, setFormOpen] = useState(false);

  const { data: balance } = useLeaveBalance(year);
  const { data, isPending, error } = useLeaves(
    tab === "pending" ? { status: "pending" } : {},
  );

  const cancel = useCancelLeave();
  const decide = useDecideLeave();

  // Tab "của tôi" lọc ở client vì API đã giới hạn theo phạm vi của người gọi;
  // thêm một tham số employee_id nữa chỉ lặp lại điều backend đã làm.
  const items = (data?.items ?? []).filter((l) =>
    tab === "mine" ? l.employee_id === myEmployeeId : true,
  );

  return (
    <AppShell>
      <div className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold">Nghỉ phép</h1>
          {balance && (
            <p className="mt-1 text-sm text-neutral-500">
              Phép năm {year}: còn{" "}
              <span className="font-medium text-neutral-900 dark:text-neutral-100">
                {balance.remaining_days} ngày
              </span>{" "}
              trên tổng {balance.entitled_days + balance.carried_over_days}
              {balance.carried_over_days > 0 &&
                ` (gồm ${balance.carried_over_days} ngày tồn năm trước)`}
            </p>
          )}
        </div>

        {can("leave:create") && (
          <button
            onClick={() => setFormOpen(true)}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Tạo đơn
          </button>
        )}
      </div>

      {canApprove && (
        <div className="mb-4 flex rounded border border-neutral-300 text-sm dark:border-neutral-700">
          {(
            [
              ["mine", "Đơn của tôi"],
              ["pending", "Chờ tôi duyệt"],
            ] as [Tab, string][]
          ).map(([v, label]) => (
            <button
              key={v}
              onClick={() => setTab(v)}
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
      )}

      <FormError
        message={
          errMsg(error, "Không tải được danh sách đơn") ??
          errMsg(cancel.error, "Không huỷ được đơn") ??
          errMsg(decide.error, "Không xử lý được đơn")
        }
      />

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              {tab === "pending" && <th className="px-4 py-2 font-medium">Người gửi</th>}
              <th className="px-4 py-2 font-medium">Loại</th>
              <th className="px-4 py-2 font-medium">Từ</th>
              <th className="px-4 py-2 font-medium">Đến</th>
              <th className="px-4 py-2 font-medium">Số ngày</th>
              <th className="px-4 py-2 font-medium">Lý do</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {isPending && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!isPending && items.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-neutral-500">
                  {tab === "mine" ? "Bạn chưa có đơn nào." : "Không có đơn chờ duyệt."}
                </td>
              </tr>
            )}
            {items.map((l) => (
              <LeaveRow
                key={l.id}
                l={l}
                showOwner={tab === "pending"}
                isMine={l.employee_id === myEmployeeId}
                canApprove={canApprove}
                onCancel={() => {
                  if (confirm("Huỷ đơn nghỉ phép này?")) cancel.mutate(l.id);
                }}
                onDecide={(approve) => {
                  const note = approve
                    ? ""
                    : (prompt("Lý do từ chối (không bắt buộc):") ?? "");
                  decide.mutate({ id: l.id, approve, note });
                }}
              />
            ))}
          </tbody>
        </table>
      </div>

      {formOpen && <LeaveForm onClose={() => setFormOpen(false)} />}
    </AppShell>
  );
}

function LeaveRow({
  l,
  showOwner,
  isMine,
  canApprove,
  onCancel,
  onDecide,
}: {
  l: LeaveRequest;
  showOwner: boolean;
  isMine: boolean;
  canApprove: boolean;
  onCancel: () => void;
  onDecide: (approve: boolean) => void;
}) {
  return (
    <tr className="border-t border-neutral-200 dark:border-neutral-800">
      {showOwner && <td className="px-4 py-2">{l.employee_name}</td>}
      <td className="px-4 py-2">{LEAVE_TYPE_LABEL[l.leave_type]}</td>
      <td className="px-4 py-2">{formatDate(l.start_date)}</td>
      <td className="px-4 py-2">{formatDate(l.end_date)}</td>
      <td className="px-4 py-2 tabular-nums">
        {l.days}
        {l.day_part !== "full" && (
          <span className="ml-1 text-xs text-neutral-500">
            ({DAY_PART_LABEL[l.day_part]})
          </span>
        )}
      </td>
      <td className="max-w-[16rem] truncate px-4 py-2 text-neutral-500">
        {l.reason || "—"}
      </td>
      <td className="px-4 py-2">
        <span className={`rounded px-2 py-0.5 text-xs ${APPROVAL_CLASS[l.status]}`}>
          {APPROVAL_LABEL[l.status]}
        </span>
        {l.decision_note && (
          <div className="mt-0.5 text-xs text-neutral-500">{l.decision_note}</div>
        )}
      </td>
      <td className="px-4 py-2 text-right">
        {/* Không ai tự duyệt đơn của mình — backend chặn, ở đây chỉ ẩn nút
            cho gọn giao diện. */}
        {l.status === "pending" && canApprove && !isMine && (
          <span className="flex justify-end gap-2">
            <button
              onClick={() => onDecide(true)}
              className="text-xs text-green-700 underline hover:no-underline dark:text-green-400"
            >
              Duyệt
            </button>
            <button
              onClick={() => onDecide(false)}
              className="text-xs text-red-600 underline hover:no-underline"
            >
              Từ chối
            </button>
          </span>
        )}
        {isMine && (l.status === "pending" || l.status === "approved") && (
          <button
            onClick={onCancel}
            className="text-xs text-neutral-400 underline hover:text-red-600"
          >
            Huỷ
          </button>
        )}
      </td>
    </tr>
  );
}

function LeaveForm({ onClose }: { onClose: () => void }) {
  const create = useCreateLeave();

  const [type, setType] = useState<LeaveType>("annual");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [part, setPart] = useState<DayPart>("full");
  const [reason, setReason] = useState("");

  // Nghỉ nửa ngày chỉ có nghĩa khi đơn gói gọn trong một ngày — cùng ràng
  // buộc mà backend và database đều kiểm tra.
  const sameDay = Boolean(start) && start === end;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-6 shadow-lg dark:border-neutral-800 dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Tạo đơn nghỉ phép</h2>
          <button onClick={onClose} aria-label="Đóng" className="text-neutral-400">
            ✕
          </button>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              {
                leave_type: type,
                start_date: start,
                end_date: end,
                day_part: sameDay ? part : "full",
                reason,
              },
              { onSuccess: onClose },
            );
          }}
          className="space-y-4"
        >
          <label className="block text-sm">
            <span className="mb-1 block font-medium">Loại nghỉ</span>
            <select
              value={type}
              onChange={(e) => setType(e.target.value as LeaveType)}
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            >
              {Object.entries(LEAVE_TYPE_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
            {type !== "annual" && (
              <span className="mt-1 block text-xs text-neutral-500">
                Loại này không trừ vào quỹ phép năm.
              </span>
            )}
          </label>

          <div className="grid grid-cols-2 gap-3">
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Từ ngày</span>
              <input
                type="date"
                value={start}
                onChange={(e) => {
                  setStart(e.target.value);
                  if (!end) setEnd(e.target.value);
                }}
                required
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
              />
            </label>
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Đến ngày</span>
              <input
                type="date"
                value={end}
                min={start || undefined}
                onChange={(e) => setEnd(e.target.value)}
                required
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
              />
            </label>
          </div>

          {sameDay && (
            <label className="block text-sm">
              <span className="mb-1 block font-medium">Thời lượng</span>
              <select
                value={part}
                onChange={(e) => setPart(e.target.value as DayPart)}
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
              >
                {Object.entries(DAY_PART_LABEL).map(([v, label]) => (
                  <option key={v} value={v}>
                    {label}
                  </option>
                ))}
              </select>
            </label>
          )}

          <label className="block text-sm">
            <span className="mb-1 block font-medium">Lý do</span>
            <textarea
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              rows={3}
              className="w-full rounded border border-neutral-300 px-3 py-2 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>

          <p className="text-xs text-neutral-500">
            Cuối tuần và ngày lễ không bị tính vào số ngày phép.
          </p>

          <FormError message={errMsg(create.error, "Không tạo được đơn")} />

          <div className="flex justify-end gap-2 pt-2">
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
            >
              Huỷ
            </button>
            <button
              type="submit"
              disabled={create.isPending}
              className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
            >
              {create.isPending ? "Đang gửi..." : "Gửi đơn"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
