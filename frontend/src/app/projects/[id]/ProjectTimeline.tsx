"use client";

import { useMemo } from "react";

import type { Project } from "@/features/projects/types";
import { useTasks } from "@/features/tasks/queries";
import { TASK_STATUS_LABEL, type Task } from "@/features/tasks/types";

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * Timeline dạng Gantt đơn giản.
 *
 * Tự vẽ bằng div thay vì thêm một thư viện Gantt: thứ cần ở đây là "việc nào
 * kéo dài từ đâu tới đâu" — khoảng 60 dòng CSS. Các thư viện Gantt đầy đủ
 * mang theo kéo-thả, phụ thuộc giữa các việc, thu phóng nhiều mức, và vài
 * trăm KB cho những tính năng Phase 2 không dùng tới.
 *
 * Mỗi công việc là một thanh chạy từ started_at (hoặc created_at) tới
 * due_date (hoặc completed_at). Việc không có mốc nào thì không vẽ được —
 * chúng được đếm riêng và báo bên dưới, thay vì lặng lẽ biến mất.
 */
export function ProjectTimeline({
  projectId,
  project,
}: {
  projectId: string;
  project: Project;
}) {
  const { data, isPending, dataUpdatedAt } = useTasks(
    { project_id: projectId, page: 1 },
    Boolean(projectId),
  );

  const tasks = useMemo(() => data?.items ?? [], [data]);

  const { rows, start, span, skipped } = useMemo(() => {
    const bars: { task: Task; from: number; to: number }[] = [];
    let skippedCount = 0;

    for (const t of tasks) {
      const from = t.started_at
        ? new Date(t.started_at).getTime()
        : new Date(t.created_at).getTime();
      const to = t.due_date
        ? new Date(t.due_date).getTime()
        : t.completed_at
          ? new Date(t.completed_at).getTime()
          : 0;

      if (!to || to < from) {
        skippedCount++;
        continue;
      }
      bars.push({ task: t, from, to });
    }

    if (bars.length === 0) {
      return { rows: [], start: 0, span: 1, skipped: skippedCount };
    }

    // Khung thời gian bám theo dữ liệu, có tính cả mốc của chính dự án —
    // nếu không, một dự án có hạn xa sẽ bị cắt mất phần đuôi.
    const candidates = [
      ...bars.map((b) => b.from),
      ...bars.map((b) => b.to),
      ...(project.start_date ? [new Date(project.start_date).getTime()] : []),
      ...(project.due_date ? [new Date(project.due_date).getTime()] : []),
    ];

    const min = Math.min(...candidates);
    const max = Math.max(...candidates);
    // Tối thiểu một ngày để không chia cho 0 khi mọi việc rơi vào cùng một ngày.
    const total = Math.max(max - min, DAY_MS);

    return {
      rows: bars.sort((a, b) => a.from - b.from),
      start: min,
      span: total,
      skipped: skippedCount,
    };
  }, [tasks, project.start_date, project.due_date]);

  if (isPending) {
    return <p className="text-sm text-neutral-500">Đang tải timeline...</p>;
  }

  // Mốc "hôm nay" lấy từ dataUpdatedAt của TanStack Query — thời điểm dữ
  // liệu này được tải về.
  //
  // Không gọi Date.now() khi render: đó là hàm không thuần khiết, mỗi lần
  // React vẽ lại vì lý do bất kỳ là vạch lại nhích đi một chút. dataUpdatedAt
  // là một con số ổn định, chỉ đổi khi dữ liệu thật sự được tải lại — và khi
  // đó vạch nhích theo là đúng. Nó bằng 0 ở lần render đầu, lúc ấy chưa có
  // gì để vẽ.
  const nowPct = dataUpdatedAt
    ? ((dataUpdatedAt - start) / span) * 100
    : null;

  return (
    <div>
      <h2 className="mb-3 text-sm font-medium">Timeline</h2>

      {rows.length === 0 ? (
        <p className="rounded border border-neutral-200 px-4 py-6 text-center text-sm text-neutral-400 dark:border-neutral-800">
          Chưa có công việc nào đủ mốc thời gian để vẽ timeline. Đặt hạn hoàn
          thành cho công việc để chúng xuất hiện ở đây.
        </p>
      ) : (
        <div className="relative rounded border border-neutral-200 p-4 dark:border-neutral-800">
          {/* Vạch "hôm nay". Chỉ vẽ khi nó nằm trong khung, tránh một vạch
              dính cứng ở mép trái khi xem dự án đã kết thúc từ lâu. */}
          {nowPct !== null && nowPct >= 0 && nowPct <= 100 && (
            <div
              className="pointer-events-none absolute bottom-4 top-4 w-px bg-red-400"
              style={{ left: `calc(1rem + ${nowPct}% * 0.75)` }}
              title="Hôm nay"
            />
          )}

          <div className="space-y-1.5">
            {rows.map(({ task, from, to }) => {
              const left = ((from - start) / span) * 100;
              // Tối thiểu 2% để việc trong ngày vẫn thấy được một vệt.
              const width = Math.max(((to - from) / span) * 100, 2);

              return (
                <div key={task.id} className="flex items-center gap-2 text-xs">
                  <div className="w-1/4 min-w-0 truncate" title={task.title}>
                    <span className="font-mono text-neutral-500">{task.code}</span>{" "}
                    {task.title}
                  </div>

                  <div className="relative h-5 flex-1 rounded bg-neutral-100 dark:bg-neutral-900">
                    <div
                      className={`absolute top-0.5 h-4 rounded ${
                        task.status === "done"
                          ? "bg-green-500"
                          : task.overdue
                            ? "bg-red-500"
                            : task.status === "review"
                              ? "bg-amber-500"
                              : task.status === "in_progress"
                                ? "bg-blue-500"
                                : "bg-neutral-400"
                      }`}
                      style={{ left: `${left}%`, width: `${width}%` }}
                      title={`${TASK_STATUS_LABEL[task.status]} · ${new Date(
                        from,
                      ).toLocaleDateString("vi-VN")} → ${new Date(
                        to,
                      ).toLocaleDateString("vi-VN")}`}
                    />
                  </div>
                </div>
              );
            })}
          </div>

          <div className="mt-3 flex justify-between border-t border-neutral-200 pt-2 text-xs text-neutral-500 dark:border-neutral-800">
            <span>{new Date(start).toLocaleDateString("vi-VN")}</span>
            <span>{new Date(start + span).toLocaleDateString("vi-VN")}</span>
          </div>
        </div>
      )}

      {skipped > 0 && (
        <p className="mt-2 text-xs text-neutral-500">
          {skipped} công việc chưa có hạn hoàn thành nên không hiện trên timeline.
        </p>
      )}
    </div>
  );
}
