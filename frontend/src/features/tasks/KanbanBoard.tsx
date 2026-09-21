"use client";

import { useState } from "react";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useDroppable,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { useMoveTask } from "./queries";
import {
  BOARD_ORDER,
  TASK_PRIORITY_CLASS,
  TASK_PRIORITY_LABEL,
  TASK_STATUS_LABEL,
  canTransition,
  formatMinutes,
  type Board,
  type Task,
  type TaskStatus,
} from "./types";

/* ------------------------------------------------------------------ *
 * Thẻ công việc
 * ------------------------------------------------------------------ */

function TaskCard({
  task,
  onOpen,
  dragging,
}: {
  task: Task;
  onOpen: (id: string) => void;
  dragging?: boolean;
}) {
  return (
    <div
      className={`rounded border bg-white p-3 text-sm shadow-sm dark:bg-neutral-900 ${
        task.overdue
          ? "border-red-300 dark:border-red-800"
          : "border-neutral-200 dark:border-neutral-800"
      } ${dragging ? "opacity-40" : "hover:border-neutral-400 dark:hover:border-neutral-600"}`}
    >
      <div className="mb-1 flex items-start justify-between gap-2">
        <span className="font-mono text-xs text-neutral-500">{task.code}</span>
        <span
          className={`shrink-0 rounded px-1.5 py-0.5 text-[11px] ${TASK_PRIORITY_CLASS[task.priority]}`}
        >
          {TASK_PRIORITY_LABEL[task.priority]}
        </span>
      </div>

      {/* Nút chứ không phải div có onClick: bàn phím Tab tới được, Enter mở
          được. Thẻ kéo-thả rất dễ trở thành thứ chỉ dùng được bằng chuột. */}
      <button
        type="button"
        onClick={() => onOpen(task.id)}
        className="block w-full text-left font-medium hover:underline"
      >
        {task.title}
      </button>

      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-neutral-500">
        {task.assignee_name && <span>{task.assignee_name}</span>}
        {task.due_date && (
          <span className={task.overdue ? "font-medium text-red-600" : ""}>
            {new Date(task.due_date).toLocaleDateString("vi-VN")}
          </span>
        )}
        {task.subtask_count > 0 && (
          <span>
            {task.done_subtasks}/{task.subtask_count} việc con
          </span>
        )}
        {task.comment_count > 0 && <span>{task.comment_count} bình luận</span>}
        {task.attachment_count > 0 && <span>{task.attachment_count} tệp</span>}
        {task.spent_minutes > 0 && <span>{formatMinutes(task.spent_minutes)}</span>}
      </div>
    </div>
  );
}

function SortableCard({
  task,
  onOpen,
}: {
  task: Task;
  onOpen: (id: string) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: task.id });

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      {...attributes}
      {...listeners}
      className="touch-none"
    >
      <TaskCard task={task} onOpen={onOpen} dragging={isDragging} />
    </div>
  );
}

/* ------------------------------------------------------------------ *
 * Cột
 * ------------------------------------------------------------------ */

function Column({
  status,
  tasks,
  onOpen,
  onAdd,
  disabled,
  canWrite,
}: {
  status: TaskStatus;
  tasks: Task[];
  onOpen: (id: string) => void;
  onAdd: (status: TaskStatus) => void;
  /** Cột không nhận được thẻ đang kéo (bước chuyển không hợp lệ). */
  disabled: boolean;
  canWrite: boolean;
}) {
  // id của vùng thả = tên trạng thái, nên thả vào khoảng trống cuối cột vẫn
  // biết được là cột nào.
  const { setNodeRef, isOver } = useDroppable({ id: status });

  return (
    <div className="flex min-w-[260px] flex-1 flex-col">
      <div className="mb-2 flex items-center justify-between px-1">
        <h3 className="text-sm font-medium">
          {TASK_STATUS_LABEL[status]}
          <span className="ml-2 text-xs text-neutral-500">{tasks.length}</span>
        </h3>
        {canWrite && (
          <button
            type="button"
            onClick={() => onAdd(status)}
            aria-label={`Thêm việc vào ${TASK_STATUS_LABEL[status]}`}
            className="rounded px-1.5 text-lg leading-none text-neutral-400 hover:bg-neutral-200 hover:text-neutral-900 dark:hover:bg-neutral-800 dark:hover:text-neutral-100"
          >
            +
          </button>
        )}
      </div>

      <div
        ref={setNodeRef}
        className={`flex min-h-[120px] flex-1 flex-col gap-2 rounded border p-2 transition ${
          disabled
            ? "border-neutral-200 bg-neutral-100/60 opacity-50 dark:border-neutral-800 dark:bg-neutral-900/60"
            : isOver
              ? "border-neutral-900 bg-neutral-50 dark:border-neutral-400 dark:bg-neutral-900"
              : "border-neutral-200 bg-neutral-50/50 dark:border-neutral-800 dark:bg-neutral-900/40"
        }`}
      >
        <SortableContext
          items={tasks.map((t) => t.id)}
          strategy={verticalListSortingStrategy}
        >
          {tasks.map((t) => (
            <SortableCard key={t.id} task={t} onOpen={onOpen} />
          ))}
        </SortableContext>

        {tasks.length === 0 && (
          <p className="px-1 py-4 text-center text-xs text-neutral-400">
            Chưa có việc nào
          </p>
        )}
      </div>
    </div>
  );
}

/* ------------------------------------------------------------------ *
 * Bảng
 * ------------------------------------------------------------------ */

export function KanbanBoard({
  board,
  canWrite,
  onOpenTask,
  onAddTask,
  onError,
}: {
  board: Board;
  canWrite: boolean;
  onOpenTask: (id: string) => void;
  onAddTask: (status: TaskStatus) => void;
  onError: (message: string | null) => void;
}) {
  const move = useMoveTask(board.project.id);
  const [activeTask, setActiveTask] = useState<Task | null>(null);

  // Yêu cầu kéo 6px mới kích hoạt.
  //
  // Không có ngưỡng này thì mỗi lần bấm vào tiêu đề để mở chi tiết đều bị
  // hiểu là bắt đầu kéo, và thẻ không mở ra được.
  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const columnOf = (id: string): TaskStatus | null => {
    if (BOARD_ORDER.includes(id as TaskStatus)) return id as TaskStatus;
    return board.columns.find((c) => c.tasks.some((t) => t.id === id))?.status ?? null;
  };

  function handleDragStart(e: DragStartEvent) {
    onError(null);
    const id = String(e.active.id);
    const t = board.columns.flatMap((c) => c.tasks).find((x) => x.id === id);
    setActiveTask(t ?? null);
  }

  function handleDragEnd(e: DragEndEvent) {
    const { active, over } = e;
    setActiveTask(null);
    if (!over) return;

    const activeId = String(active.id);
    const overId = String(over.id);

    const from = columnOf(activeId);
    const to = columnOf(overId);
    if (!from || !to) return;
    if (from === to && activeId === overId) return;

    // Chặn sớm ở giao diện cho thông báo dễ hiểu. Backend vẫn kiểm tra lại —
    // đây chỉ là phép lịch sự với người dùng, không phải hàng rào bảo mật.
    if (!canTransition(from, to)) {
      onError(
        `Không chuyển trực tiếp từ "${TASK_STATUS_LABEL[from]}" sang "${TASK_STATUS_LABEL[to]}". Đi lần lượt từng bước.`,
      );
      return;
    }

    const target = board.columns.find((c) => c.status === to);
    if (!target) return;

    // Bỏ chính thẻ đang kéo ra khỏi danh sách trước khi tính vị trí — kéo
    // trong cùng một cột thì nó vừa bị gỡ vừa được chèn lại.
    const rest = target.tasks.filter((t) => t.id !== activeId);

    let insertAt: number;
    if (overId === to) {
      insertAt = rest.length; // thả vào khoảng trống cuối cột
    } else {
      const overIdxInRest = rest.findIndex((t) => t.id === overId);
      const activeIdx = target.tasks.findIndex((t) => t.id === activeId);
      const overIdx = target.tasks.findIndex((t) => t.id === overId);

      // Kéo XUỐNG thì thả SAU thẻ đang hover, kéo LÊN thì thả TRƯỚC nó.
      // Không phân biệt hai chiều, thẻ sẽ luôn rơi lệch một ô so với chỗ
      // người dùng nhìn thấy con trỏ.
      const movingDown = activeIdx >= 0 && activeIdx < overIdx;
      insertAt = movingDown ? overIdxInRest + 1 : overIdxInRest;
      if (overIdxInRest < 0) insertAt = rest.length;
    }

    const afterTaskId = insertAt <= 0 ? null : rest[insertAt - 1].id;

    move.mutate(
      { taskId: activeId, status: to, afterTaskId },
      {
        onError: (err) =>
          onError(err instanceof Error ? err.message : "Không di chuyển được công việc"),
      },
    );
  }

  const activeColumn = activeTask ? activeTask.status : null;

  return (
    <DndContext
      sensors={sensors}
      collisionDetection={closestCorners}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={() => setActiveTask(null)}
    >
      <div className="flex gap-4 overflow-x-auto pb-4">
        {board.columns.map((col) => (
          <Column
            key={col.status}
            status={col.status}
            tasks={col.tasks}
            onOpen={onOpenTask}
            onAdd={onAddTask}
            canWrite={canWrite}
            // Làm mờ cột không nhận được thẻ đang kéo: người dùng thấy ngay
            // chỗ nào thả được, thay vì thả rồi mới nhận thông báo lỗi.
            disabled={
              activeColumn !== null &&
              activeColumn !== col.status &&
              !canTransition(activeColumn, col.status)
            }
          />
        ))}
      </div>

      {/* Thẻ bay theo con trỏ. Không có nó, thẻ đang kéo biến mất khỏi màn
          hình và người dùng mất dấu thứ mình đang cầm. */}
      <DragOverlay>
        {activeTask && (
          <div className="w-[260px] rotate-2">
            <TaskCard task={activeTask} onOpen={() => {}} />
          </div>
        )}
      </DragOverlay>
    </DndContext>
  );
}
