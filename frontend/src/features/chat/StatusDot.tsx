import type { PresenceStatus } from "./types";

const LABEL: Record<PresenceStatus, string> = {
  online: "Đang hoạt động",
  idle: "Đang mở nhưng không hoạt động",
  offline: "Ngoại tuyến",
};

const COLOR: Record<PresenceStatus, string> = {
  online: "bg-emerald-500",
  idle: "bg-amber-400",
  offline: "bg-neutral-300 dark:bg-neutral-600",
};

/**
 * Chấm trạng thái, dùng lại đúng presence của Phase 3.
 *
 * Ba trạng thái chứ không phải hai: "đang mở tab" khác "đang làm việc", và
 * gộp chúng lại sẽ hiện màu xanh cho người đã bỏ máy đó cả buổi.
 */
export function StatusDot({ status }: { status?: PresenceStatus }) {
  const s = status ?? "offline";

  return (
    <span
      className={`inline-block h-2 w-2 shrink-0 rounded-full ${COLOR[s]}`}
      title={LABEL[s]}
      aria-label={LABEL[s]}
    />
  );
}
