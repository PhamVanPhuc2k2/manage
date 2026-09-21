"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";

import { Field, FormError, Modal, SubmitButton, inputProps } from "@/components/form";
import { ApiError } from "@/lib/api-client";
import type { ProjectMember } from "../projects/types";
import { useCreateTask, useUpdateTask } from "./queries";
import {
  TASK_PRIORITY_LABEL,
  TASK_STATUS_LABEL,
  type Task,
  type TaskStatus,
} from "./types";

const schema = z.object({
  title: z.string().trim().min(1, "Nhập tiêu đề").max(500, "Tối đa 500 ký tự"),
  description: z.string().optional(),
  status: z.enum(["todo", "in_progress", "review", "done"]),
  priority: z.enum(["low", "medium", "high", "urgent"]),
  assignee_id: z.string().optional(),
  due_date: z.string().optional(),
  estimate_hours: z.string().optional(),
});

type FormValues = z.infer<typeof schema>;

export function TaskForm({
  projectId,
  task,
  parentTask,
  defaultStatus,
  members,
  onClose,
}: {
  projectId: string;
  /** Có giá trị khi đang SỬA một công việc. */
  task?: Task;
  /** Có giá trị khi đang tạo VIỆC CON của công việc này. */
  parentTask?: Task;
  defaultStatus?: TaskStatus;
  members: ProjectMember[];
  onClose: () => void;
}) {
  const isEdit = Boolean(task);

  const create = useCreateTask(projectId);
  const update = useUpdateTask(task?.id ?? "");
  const mutation = isEdit ? update : create;

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      title: task?.title ?? "",
      description: task?.description ?? "",
      status: task?.status ?? defaultStatus ?? "todo",
      priority: task?.priority ?? "medium",
      assignee_id: task?.assignee_id ?? "",
      due_date: task?.due_date?.slice(0, 10) ?? "",
      estimate_hours: task?.estimate_hours ? String(task.estimate_hours) : "",
    },
  });

  const submitError =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "Không lưu được công việc"
        : null;

  const title = isEdit
    ? "Sửa công việc"
    : parentTask
      ? `Việc con của ${parentTask.code}`
      : "Thêm công việc";

  return (
    <Modal title={title} onClose={onClose}>
      <form
        onSubmit={handleSubmit((v) => {
          const body = {
            title: v.title,
            description: v.description ?? "",
            status: v.status,
            priority: v.priority,
            assignee_id: v.assignee_id || null,
            due_date: v.due_date || null,
            estimate_hours: v.estimate_hours ? Number(v.estimate_hours) : null,
            // project_id và parent_task_id chỉ gửi lúc TẠO. API sửa không
            // nhận chúng — chuyển công việc sang dự án khác là thao tác riêng,
            // không phải hệ quả phụ của việc sửa tiêu đề.
            ...(isEdit
              ? {}
              : {
                  project_id: projectId,
                  parent_task_id: parentTask?.id ?? null,
                }),
          };
          mutation.mutate(body, { onSuccess: onClose });
        })}
        className="space-y-4"
      >
        <Field label="Tiêu đề" error={errors.title} required>
          <input {...register("title")} {...inputProps(Boolean(errors.title))} />
        </Field>

        <Field label="Mô tả" error={errors.description}>
          <textarea
            {...register("description")}
            rows={4}
            {...inputProps(Boolean(errors.description))}
          />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Trạng thái" error={errors.status}>
            <select {...register("status")} {...inputProps(Boolean(errors.status))}>
              {Object.entries(TASK_STATUS_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Độ ưu tiên" error={errors.priority}>
            <select {...register("priority")} {...inputProps(Boolean(errors.priority))}>
              {Object.entries(TASK_PRIORITY_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Field
          label="Người thực hiện"
          error={errors.assignee_id}
          hint="Chỉ chọn được thành viên của dự án"
        >
          <select
            {...register("assignee_id")}
            {...inputProps(Boolean(errors.assignee_id))}
          >
            <option value="">— Chưa giao —</option>
            {members.map((m) => (
              <option key={m.employee_id} value={m.employee_id}>
                {m.employee_name}
              </option>
            ))}
          </select>
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Hạn hoàn thành" error={errors.due_date}>
            <input
              type="date"
              {...register("due_date")}
              {...inputProps(Boolean(errors.due_date))}
            />
          </Field>
          <Field label="Ước lượng (giờ)" error={errors.estimate_hours}>
            <input
              type="number"
              step="0.5"
              min="0"
              {...register("estimate_hours")}
              {...inputProps(Boolean(errors.estimate_hours))}
            />
          </Field>
        </div>

        <FormError message={submitError} />

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
          >
            Huỷ
          </button>
          <SubmitButton pending={mutation.isPending}>
            {isEdit ? "Lưu" : "Tạo công việc"}
          </SubmitButton>
        </div>
      </form>
    </Modal>
  );
}
