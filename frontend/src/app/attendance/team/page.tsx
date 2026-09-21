"use client";

import { useMemo, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useAdjustments,
  useDecideAdjustment,
  useMonthSummary,
  useTeamPresence,
} from "@/features/attendance/queries";
import {
  PRESENCE_DOT,
  PRESENCE_LABEL,
  formatDuration,
  type PresenceStatus,
} from "@/features/attendance/types";
import { useEmployees } from "@/features/employees/queries";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

const ORDER: Record<PresenceStatus, number> = { online: 0, idle: 1, offline: 2 };

export default function TeamAttendancePage() {
  const { can } = usePermission();
  const now = new Date();

  const [year, setYear] = useState(now.getFullYear());
  const [month, setMonth] = useState(now.getMonth() + 1);

  const { data: presence = [], error: presenceError } = useTeamPresence(
    can("attendance:read_all"),
  );
  const { data: summary = [], isPending } = useMonthSummary(year, month);
  const { data: employeeData } = useEmployees({ page: 1, pageSize: 100 });
  const { data: adjustments = [] } = useAdjustments("pending");
  const decide = useDecideAdjustment();

  const nameOf = useMemo(() => {
    const m = new Map<string, string>();
    (employeeData?.items ?? []).forEach((e) => m.set(e.id, e.full_name));
    return m;
  }, [employeeData]);

  // Sắp người đang làm việc lên đầu: màn hình này tồn tại để trả lời "giờ
  // hỏi được ai", nên thứ tự bảng chữ cái là thứ tự sai.
  const sortedPresence = useMemo(
    () =>
      [...presence].sort(
        (a, b) =>
          ORDER[a.status] - ORDER[b.status] ||
          (nameOf.get(a.employee_id) ?? "").localeCompare(
            nameOf.get(b.employee_id) ?? "",
          ),
      ),
    [presence, nameOf],
  );

  const onlineCount = presence.filter((p) => p.status !== "offline").length;

  if (!can("attendance:read_all")) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">
          Bạn không có quyền xem chấm công của người khác.
        </p>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <h1 className="mb-6 text-xl font-semibold">Chấm công phòng ban</h1>

      <FormError message={errMsg(presenceError, "Không tải được trạng thái")} />

      {/* --- Ai đang làm việc --- */}
      <div className="mb-8">
        <h2 className="mb-3 text-sm font-medium">
          Đang làm việc{" "}
          <span className="text-neutral-500">
            ({onlineCount}/{presence.length})
          </span>
        </h2>

        <div className="flex flex-wrap gap-2">
          {sortedPresence.map((p) => (
            <span
              key={p.employee_id}
              title={PRESENCE_LABEL[p.status]}
              className={`flex items-center gap-2 rounded-full border px-3 py-1 text-sm ${
                p.status === "offline"
                  ? "border-neutral-200 text-neutral-400 dark:border-neutral-800"
                  : "border-neutral-300 dark:border-neutral-700"
              }`}
            >
              <span className={`h-2 w-2 rounded-full ${PRESENCE_DOT[p.status]}`} />
              {nameOf.get(p.employee_id) ?? p.employee_id.slice(0, 8)}
            </span>
          ))}
          {presence.length === 0 && (
            <p className="text-sm text-neutral-400">Chưa có dữ liệu.</p>
          )}
        </div>
      </div>

      {/* --- Yêu cầu điều chỉnh chờ duyệt --- */}
      {can("attendance:manage") && adjustments.length > 0 && (
        <div className="mb-8">
          <h2 className="mb-3 text-sm font-medium">
            Yêu cầu điều chỉnh chờ duyệt ({adjustments.length})
          </h2>

          <FormError message={errMsg(decide.error, "Không xử lý được yêu cầu")} />

          <div className="divide-y divide-neutral-200 rounded border border-neutral-200 dark:divide-neutral-800 dark:border-neutral-800">
            {adjustments.map((a) => (
              <div
                key={a.id}
                className="flex flex-wrap items-center justify-between gap-2 px-4 py-2 text-sm"
              >
                <div>
                  <span className="font-medium">{a.employee_name}</span>
                  <span className="ml-2 text-neutral-500">
                    {new Date(a.requested_start).toLocaleString("vi-VN")} →{" "}
                    {new Date(a.requested_end).toLocaleTimeString("vi-VN", {
                      hour: "2-digit",
                      minute: "2-digit",
                    })}
                  </span>
                  <div className="text-xs text-neutral-500">{a.reason}</div>
                </div>

                <div className="flex gap-2">
                  <button
                    onClick={() => decide.mutate({ id: a.id, approve: true, note: "" })}
                    className="text-xs text-green-700 underline hover:no-underline dark:text-green-400"
                  >
                    Duyệt
                  </button>
                  <button
                    onClick={() =>
                      decide.mutate({
                        id: a.id,
                        approve: false,
                        note: prompt("Lý do từ chối:") ?? "",
                      })
                    }
                    className="text-xs text-red-600 underline hover:no-underline"
                  >
                    Từ chối
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* --- Tổng hợp tháng --- */}
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="text-sm font-medium">Tổng hợp tháng</h2>
        <select
          value={month}
          onChange={(e) => setMonth(Number(e.target.value))}
          className="rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          {Array.from({ length: 12 }, (_, i) => i + 1).map((m) => (
            <option key={m} value={m}>
              Tháng {m}
            </option>
          ))}
        </select>
        <select
          value={year}
          onChange={(e) => setYear(Number(e.target.value))}
          className="rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        >
          {[now.getFullYear(), now.getFullYear() - 1].map((y) => (
            <option key={y} value={y}>
              {y}
            </option>
          ))}
        </select>
      </div>

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Nhân viên</th>
              <th className="px-4 py-2 font-medium">Ngày công</th>
              <th className="px-4 py-2 font-medium">Có mặt</th>
              <th className="px-4 py-2 font-medium">Vắng</th>
              <th className="px-4 py-2 font-medium">Nghỉ phép</th>
              <th className="px-4 py-2 font-medium">Tổng giờ</th>
              <th className="px-4 py-2 font-medium">Đi muộn</th>
              <th className="px-4 py-2 font-medium">Thiếu giờ</th>
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
            {!isPending && summary.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có dữ liệu cho tháng này.
                </td>
              </tr>
            )}
            {summary.map((r) => (
              <tr
                key={r.employee_id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2">{r.employee_name}</td>
                <td className="px-4 py-2 tabular-nums">{r.workday_count}</td>
                <td className="px-4 py-2 tabular-nums">{r.present_days}</td>
                <td className="px-4 py-2 tabular-nums">
                  {r.absent_days > 0 ? (
                    <span className="text-red-600">{r.absent_days}</span>
                  ) : (
                    0
                  )}
                </td>
                <td className="px-4 py-2 tabular-nums">{r.leave_days}</td>
                <td className="px-4 py-2 tabular-nums">
                  {formatDuration(r.online_minutes)}
                </td>
                <td className="px-4 py-2 tabular-nums">
                  {r.late_days > 0 ? `${r.late_days} lần` : "—"}
                </td>
                <td className="px-4 py-2 tabular-nums text-neutral-500">
                  {r.shortfall_minutes > 0
                    ? formatDuration(r.shortfall_minutes)
                    : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}
