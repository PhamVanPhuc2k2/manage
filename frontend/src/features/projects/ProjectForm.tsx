"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";

import { Field, FormError, Modal, SubmitButton, inputProps } from "@/components/form";
import { useDepartments } from "@/features/departments/queries";
import { useEmployees } from "@/features/employees/queries";
import { ApiError } from "@/lib/api-client";
import { useCreateProject, useUpdateProject } from "./queries";
import { PROJECT_STATUS_LABEL, type Project } from "./types";

/**
 * Mã dự án đi vào mã công việc hiển thị ("WEB-42"), nên nó phải ngắn và
 * không chứa khoảng trắng hay dấu gạch ngang — nếu không thì mã task sẽ
 * không đọc được. Cùng luật này được kiểm tra lại ở backend.
 */
const schema = z
  .object({
    code: z
      .string()
      .trim()
      .min(1, "Nhập mã dự án")
      .max(20, "Tối đa 20 ký tự")
      .regex(/^[A-Za-z0-9_]+$/, "Chỉ dùng chữ, số và dấu gạch dưới"),
    name: z.string().trim().min(1, "Nhập tên dự án"),
    description: z.string().optional(),
    status: z.enum(["planning", "active", "on_hold", "completed", "cancelled"]),
    owner_id: z.string().min(1, "Chọn chủ dự án"),
    department_id: z.string().optional(),
    start_date: z.string().optional(),
    due_date: z.string().optional(),
  })
  .refine(
    (v) => !v.start_date || !v.due_date || v.start_date <= v.due_date,
    { message: "Hạn hoàn thành phải sau ngày bắt đầu", path: ["due_date"] },
  );

type FormValues = z.infer<typeof schema>;

export function ProjectForm({
  project,
  onClose,
}: {
  project?: Project;
  onClose: () => void;
}) {
  const isEdit = Boolean(project);

  const create = useCreateProject();
  const update = useUpdateProject(project?.id ?? "");
  const mutation = isEdit ? update : create;

  const { data: departments = [] } = useDepartments();
  // Danh sách chọn chủ dự án. Lấy trang đầu với cỡ trang lớn là đủ cho một
  // công ty vài trăm người; quá ngưỡng đó thì cần ô tìm kiếm riêng.
  const { data: employeeData } = useEmployees({ page: 1, pageSize: 100 });
  const employees = employeeData?.items ?? [];

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(schema),
    defaultValues: {
      code: project?.code ?? "",
      name: project?.name ?? "",
      description: project?.description ?? "",
      status: project?.status ?? "planning",
      owner_id: project?.owner_id ?? "",
      department_id: project?.department_id ?? "",
      start_date: project?.start_date?.slice(0, 10) ?? "",
      due_date: project?.due_date?.slice(0, 10) ?? "",
    },
  });

  const submitError =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "Không lưu được dự án"
        : null;

  return (
    <Modal title={isEdit ? "Sửa dự án" : "Thêm dự án"} onClose={onClose}>
      <form
        onSubmit={handleSubmit((v) =>
          mutation.mutate(
            {
              ...v,
              // Chuỗi rỗng khác null: rỗng là "người dùng để trống", còn API
              // cần null để hiểu là "không có giá trị".
              department_id: v.department_id || null,
              start_date: v.start_date || null,
              due_date: v.due_date || null,
            },
            { onSuccess: onClose },
          ),
        )}
        className="space-y-4"
      >
        <div className="grid grid-cols-3 gap-3">
          <Field label="Mã" error={errors.code} required hint="VD: WEB">
            <input
              {...register("code")}
              disabled={isEdit}
              {...inputProps(Boolean(errors.code))}
            />
          </Field>
          <div className="col-span-2">
            <Field label="Tên dự án" error={errors.name} required>
              <input {...register("name")} {...inputProps(Boolean(errors.name))} />
            </Field>
          </div>
        </div>

        <Field label="Mô tả" error={errors.description}>
          <textarea
            {...register("description")}
            rows={3}
            {...inputProps(Boolean(errors.description))}
          />
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Chủ dự án" error={errors.owner_id} required>
            <select {...register("owner_id")} {...inputProps(Boolean(errors.owner_id))}>
              <option value="">— Chọn —</option>
              {employees.map((e) => (
                <option key={e.id} value={e.id}>
                  {e.full_name}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Trạng thái" error={errors.status}>
            <select {...register("status")} {...inputProps(Boolean(errors.status))}>
              {Object.entries(PROJECT_STATUS_LABEL).map(([v, label]) => (
                <option key={v} value={v}>
                  {label}
                </option>
              ))}
            </select>
          </Field>
        </div>

        <Field label="Phòng ban chủ quản" error={errors.department_id}>
          <select
            {...register("department_id")}
            {...inputProps(Boolean(errors.department_id))}
          >
            <option value="">— Không thuộc phòng nào —</option>
            {departments.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </Field>

        <div className="grid grid-cols-2 gap-3">
          <Field label="Bắt đầu" error={errors.start_date}>
            <input
              type="date"
              {...register("start_date")}
              {...inputProps(Boolean(errors.start_date))}
            />
          </Field>
          <Field label="Hạn hoàn thành" error={errors.due_date}>
            <input
              type="date"
              {...register("due_date")}
              {...inputProps(Boolean(errors.due_date))}
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
            {isEdit ? "Lưu" : "Tạo dự án"}
          </SubmitButton>
        </div>
      </form>
    </Modal>
  );
}
