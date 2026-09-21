"use client";

import { useMemo, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useAttendanceDay,
  useAttendanceDays,
  useCheckIn,
  useCreateAdjustment,
  useToday,
} from "@/features/attendance/queries";
import {
  DAY_STATUS_CLASS,
  DAY_STATUS_LABEL,
  formatClock,
  formatDate,
  formatDuration,
  type AttendanceDay,
} from "@/features/attendance/types";
import { ApiError } from "@/lib/api-client";
import { useWsStatus } from "@/lib/ws/useWebSocket";

function iso(d: Date): string {
  return d.toISOString().slice(0, 10);
}

function errMsg(e: unknown, fallback: string): string | null {
  if (!e) return null;
  return e instanceof ApiError ? e.message : fallback;
}

/** Dòng thời gian trong ngày: 24 giờ, mỗi phiên là một vệt. */
function DayTimeline({ date }: { date: string }) {
  const { data, isPending } = useAttendanceDay(date);

  if (isPending) return <p className="text-sm text-neutral-500">Đang tải...</p>;
  if (!data) return null;

  const sessions = data.sessions;

  return (
    <div>
      <div className="relative h-8 overflow-hidden rounded border border-neutral-200 bg-neutral-50 dark:border-neutral-800 dark:bg-neutral-900">
        {sessions.map((s) => {
          const start = new Date(s.started_at);
          const end = new Date(s.ended_at);
          // Vị trí tính theo phút kể từ nửa đêm GIỜ ĐỊA PHƯƠNG của trình
          // duyệt — cùng múi giờ mà người dùng đang nhìn đồng hồ của họ.
          const from = (start.getHours() * 60 + start.getMinutes()) / 1440;
          const to = (end.getHours() * 60 + end.getMinutes()) / 1440;
          const width = Math.max((to - from) * 100, 0.6);

          return (
            <div
              key={s.id}
              title={`${formatClock(s.started_at)} – ${formatClock(s.ended_at)} · ${
                s.source === "presence"
                  ? "tự động"
                  : s.source === "manual"
                    ? "khai thủ công"
                    : "đã điều chỉnh"
              }`}
              style={{ left: `${from * 100}%`, width: `${width}%` }}
              className={`absolute top-1 h-6 rounded-sm ${
                s.source === "presence"
                  ? "bg-green-500"
                  : s.source === "manual"
                    ? "bg-blue-500"
                    : "bg-amber-500"
              }`}
            />
          );
        })}
      </div>

      <div className="mt-1 flex justify-between text-[11px] text-neutral-400">
        <span>00:00</span>
        <span>06:00</span>
        <span>12:00</span>
        <span>18:00</span>
        <span>24:00</span>
      </div>

      {sessions.length === 0 && (
        <p className="mt-2 text-sm text-neutral-400">
          Chưa ghi nhận thời gian làm việc nào trong ngày này.
        </p>
      )}

      {sessions.length > 0 && (
        <ul className="mt-3 space-y-1 text-sm">
          {sessions.map((s) => (
            <li key={s.id} className="flex items-center gap-2">
              <span className="tabular-nums">
                {formatClock(s.started_at)} – {formatClock(s.ended_at)}
              </span>
              <span className="text-xs text-neutral-500">
                {formatDuration(s.minutes)}
                {s.source !== "presence" &&
                  ` · ${s.source === "manual" ? "khai thủ công" : "đã điều chỉnh"}`}
                {s.note && ` · ${s.note}`}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function DayRow({ d, onPick }: { d: AttendanceDay; onPick: (s: string) => void }) {
  return (
    <tr className="border-t border-neutral-200 dark:border-neutral-800">
      <td className="px-4 py-2">
        <button onClick={() => onPick(d.work_date)} className="hover:underline">
          {formatDate(d.work_date)}
        </button>
      </td>
      <td className="px-4 py-2">
        <span className={`rounded px-2 py-0.5 text-xs ${DAY_STATUS_CLASS[d.status]}`}>
          {DAY_STATUS_LABEL[d.status]}
        </span>
      </td>
      <td className="px-4 py-2 tabular-nums">{formatDuration(d.online_minutes)}</td>
      <td className="px-4 py-2 tabular-nums text-neutral-500">
        {formatDuration(d.active_minutes)}
      </td>
      <td className="px-4 py-2 tabular-nums">{formatClock(d.first_seen_at)}</td>
      <td className="px-4 py-2 tabular-nums">{formatClock(d.last_seen_at)}</td>
      <td className="px-4 py-2 tabular-nums">
        {d.late_minutes > 0 ? (
          <span className="text-red-600">{d.late_minutes} phút</span>
        ) : (
          "—"
        )}
      </td>
      <td className="px-4 py-2 text-xs text-neutral-400">
        {d.is_locked ? "đã chốt" : ""}
      </td>
    </tr>
  );
}

export default function AttendancePage() {
  const today = iso(new Date());
  const [picked, setPicked] = useState(today);
  const [from, setFrom] = useState(() => {
    const d = new Date();
    d.setDate(d.getDate() - 29);
    return iso(d);
  });
  const [to, setTo] = useState(today);

  const { data: todayStatus } = useToday();
  const { data: days = [], isPending, error } = useAttendanceDays(from, to);
  const wsStatus = useWsStatus();

  const totals = useMemo(() => {
    return days.reduce(
      (acc, d) => ({
        online: acc.online + d.online_minutes,
        active: acc.active + d.active_minutes,
        late: acc.late + d.late_minutes,
        present: acc.present + (d.status === "present" ? 1 : 0),
      }),
      { online: 0, active: 0, late: 0, present: 0 },
    );
  }, [days]);

  return (
    <AppShell>
      <h1 className="mb-1 text-xl font-semibold">Chấm công của tôi</h1>

      {/* Minh bạch dữ liệu: nói thẳng hệ thống đang ghi nhận cái gì. Đây là
          điều kiện để cơ chế đo qua presence không bị cảm nhận như theo dõi
          ngầm — xem bảng rủi ro trong doc/TASKS.md. */}
      <p className="mb-6 max-w-2xl text-sm text-neutral-500">
        Hệ thống ghi nhận thời gian bạn mở ứng dụng để tính công. Chỉ đo thời
        gian trực tuyến — không chụp màn hình, không theo dõi ứng dụng khác.
        Thấy số liệu chưa đúng thì gửi yêu cầu điều chỉnh ở bên dưới.
      </p>

      {wsStatus !== "open" && (
        <div className="mb-4 rounded border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300">
          Mất kết nối thời gian thực — thời gian làm việc tạm thời không được
          ghi nhận. Hệ thống đang tự kết nối lại.
        </div>
      )}

      {/* --- Hôm nay --- */}
      <div className="mb-6 rounded border border-neutral-200 p-4 dark:border-neutral-800">
        <div className="mb-3 flex flex-wrap items-baseline justify-between gap-2">
          <h2 className="text-sm font-medium">Hôm nay</h2>
          {todayStatus?.schedule_name && (
            <span className="text-xs text-neutral-500">
              {todayStatus.schedule_name} · cần{" "}
              {formatDuration(todayStatus.expected_minutes)}
            </span>
          )}
        </div>

        <div className="mb-4 grid gap-4 sm:grid-cols-3">
          <Stat
            label="Thời gian trực tuyến"
            value={formatDuration(todayStatus?.online_minutes ?? 0)}
          />
          <Stat
            label="Có thao tác"
            value={formatDuration(todayStatus?.active_minutes ?? 0)}
          />
          <Stat
            label="Bắt đầu lúc"
            value={formatClock(todayStatus?.first_seen_at)}
          />
        </div>

        <DayTimeline date={picked} />
        {picked !== today && (
          <button
            onClick={() => setPicked(today)}
            className="mt-2 text-xs underline hover:no-underline"
          >
            Về hôm nay
          </button>
        )}
      </div>

      <ManualEntry />

      {/* --- Lịch sử --- */}
      <div className="mb-3 mt-8 flex flex-wrap items-center gap-3">
        <h2 className="text-sm font-medium">Lịch sử</h2>
        <input
          type="date"
          value={from}
          onChange={(e) => setFrom(e.target.value)}
          className="rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        />
        <span className="text-sm text-neutral-500">tới</span>
        <input
          type="date"
          value={to}
          onChange={(e) => setTo(e.target.value)}
          className="rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
        />
        <span className="ml-auto text-xs text-neutral-500">
          {totals.present} ngày có mặt · tổng {formatDuration(totals.online)}
          {totals.late > 0 && ` · đi muộn ${totals.late} phút`}
        </span>
      </div>

      <FormError message={errMsg(error, "Không tải được bảng công")} />

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Ngày</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
              <th className="px-4 py-2 font-medium">Trực tuyến</th>
              <th className="px-4 py-2 font-medium">Có thao tác</th>
              <th className="px-4 py-2 font-medium">Vào</th>
              <th className="px-4 py-2 font-medium">Ra</th>
              <th className="px-4 py-2 font-medium">Đi muộn</th>
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
            {!isPending && days.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có dữ liệu chấm công trong khoảng này.
                </td>
              </tr>
            )}
            {days.map((d) => (
              <DayRow key={d.work_date} d={d} onPick={setPicked} />
            ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-xs text-neutral-500">{label}</div>
      <div className="mt-0.5 text-xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

/** Khai công thủ công và gửi yêu cầu điều chỉnh. */
function ManualEntry() {
  const checkIn = useCheckIn();
  const adjust = useCreateAdjustment();

  const [mode, setMode] = useState<"check-in" | "adjust">("check-in");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [reason, setReason] = useState("");

  const pending = checkIn.isPending || adjust.isPending;
  const error =
    errMsg(checkIn.error, "Không ghi được thời gian") ??
    errMsg(adjust.error, "Không gửi được yêu cầu");

  const done = checkIn.isSuccess || adjust.isSuccess;

  function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!start || !end) return;

    // <input type="datetime-local"> cho ra giờ ĐỊA PHƯƠNG không kèm múi giờ.
    // new Date() đọc nó theo múi giờ trình duyệt, toISOString đổi sang UTC —
    // đúng thứ backend cần. Gửi thẳng chuỗi thô sẽ bị hiểu là UTC và lệch
    // đúng bằng chênh lệch múi giờ.
    const startISO = new Date(start).toISOString();
    const endISO = new Date(end).toISOString();

    const reset = () => {
      setStart("");
      setEnd("");
      setReason("");
    };

    if (mode === "check-in") {
      checkIn.mutate({ startedAt: startISO, endedAt: endISO, note: reason },
        { onSuccess: reset });
    } else {
      adjust.mutate({ startedAt: startISO, endedAt: endISO, reason },
        { onSuccess: reset });
    }
  }

  return (
    <div className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <div className="mb-3 flex gap-2 text-sm">
        {(
          [
            ["check-in", "Khai công thủ công"],
            ["adjust", "Yêu cầu điều chỉnh"],
          ] as const
        ).map(([v, label]) => (
          <button
            key={v}
            onClick={() => setMode(v)}
            className={`rounded px-3 py-1 transition ${
              mode === v
                ? "bg-neutral-900 text-white dark:bg-white dark:text-neutral-900"
                : "border border-neutral-300 dark:border-neutral-700"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <p className="mb-3 text-xs text-neutral-500">
        {mode === "check-in"
          ? "Dùng khi mất mạng hoặc quên mở ứng dụng. Ghi vào bảng công ngay."
          : "Dùng khi số liệu đã ghi chưa đúng. Cần quản lý duyệt trước khi có hiệu lực."}
      </p>

      <form onSubmit={submit} className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="text-sm">
            <span className="mb-1 block font-medium">Từ</span>
            <input
              type="datetime-local"
              value={start}
              onChange={(e) => setStart(e.target.value)}
              required
              className="w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>
          <label className="text-sm">
            <span className="mb-1 block font-medium">Đến</span>
            <input
              type="datetime-local"
              value={end}
              onChange={(e) => setEnd(e.target.value)}
              required
              className="w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-950"
            />
          </label>
        </div>

        <label className="block text-sm">
          <span className="mb-1 block font-medium">
            Lý do{mode === "adjust" && <span className="text-red-600"> *</span>}
          </span>
          <input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            required={mode === "adjust"}
            placeholder="VD: họp với khách ở ngoài, mất mạng"
            className="w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-950"
          />
        </label>

        <FormError message={error} />
        {done && !error && (
          <p className="text-sm text-green-700 dark:text-green-400">
            {mode === "check-in" ? "Đã ghi nhận." : "Đã gửi yêu cầu, chờ duyệt."}
          </p>
        )}

        <button
          type="submit"
          disabled={pending}
          className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
        >
          {pending ? "Đang gửi..." : mode === "check-in" ? "Ghi nhận" : "Gửi yêu cầu"}
        </button>
      </form>
    </div>
  );
}
