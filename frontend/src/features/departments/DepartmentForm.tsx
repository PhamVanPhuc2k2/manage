"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";

import {
  Field,
  FormError,
  Modal,
  SubmitButton,
  inputProps,
} from "@/components/form";
import { useEmployees } from "@/features/employees/queries";
import { ApiError } from "@/lib/api-client";

import { useCreateDepartment, useDepartments, useUpdateDepartment } from "./queries";
import type { Department } from "./types";

const schema = z.object({
  code: z.string().trim().min(1, "Mã phòng ban không được để trống").max(50),
  name: z.string().trim().min(1, "Tên phòng ban không được để trống").max(255),
  description: z.string(),
  parent_id: z.string(),
  manager_id: z.string(),
});

type Values = z.infer<typeof schema>;

export function DepartmentForm({
  editing,
  onClose,
}: {
  /** Có giá trị là đang sửa, không có là đang tạo mới. */
  editing?: Department;
  onClose: () => void;
}) {
  const isEdit = Boolean(editing);

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<Values>({
    resolver: zodResolver(schema),
    defaultValues: {
      code: editing?.code ?? "",
      name: editing?.name ?? "",
      description: editing?.description ?? "",
      parent_id: editing?.parent_id ?? "",
      manager_id: editing?.manager_id ?? "",
    },
  });

  const { data: departments = [] } = useDepartments();
  const { data: employeeList } = useEmployees({ pageSize: 100 });

  const create = useCreateDepartment();
  const update = useUpdateDepartment(editing?.id ?? "");
  const active = isEdit ? update : create;

  // Không cho chọn chính nó làm cha. Backend còn chặn cả trường hợp chọn
  // phòng con của mình (tạo vòng lặp), việc đó phải tra cây nên để server lo.
  const parentOptions = departments.filter((d) => d.id !== editing?.id);

  const message =
    active.error instanceof ApiError
      ? active.error.message
      : active.error
        ? "Không lưu được"
        : null;

  function onSubmit(v: Values) {
    const payload = {
      code: v.code.trim(),
      name: v.name.trim(),
      description: v.description,
      parent_id: v.parent_id || null,
      manager_id: v.manager_id || null,
    };
    active.mutate(payload as never, { onSuccess: onClose });
  }

  return (
    <Modal title={isEdit ? "Sửa phòng ban" : "Thêm phòng ban"} onClose={onClose}>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <FormError message={message} />

        <Field label="Mã phòng ban" required error={errors.code}>
          <input
            {...register("code")}
            {...inputProps(!!errors.code)}
            placeholder="KT"
          />
        </Field>

        <Field label="Tên phòng ban" required error={errors.name}>
          <input
            {...register("name")}
            {...inputProps(!!errors.name)}
            placeholder="Phòng Kỹ thuật"
          />
        </Field>

        <Field label="Trực thuộc" error={errors.parent_id}>
          <select {...register("parent_id")} {...inputProps(!!errors.parent_id)}>
            <option value="">— Phòng ban cấp cao nhất —</option>
            {parentOptions.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </select>
        </Field>

        <Field label="Trưởng phòng" error={errors.manager_id}>
          <select {...register("manager_id")} {...inputProps(!!errors.manager_id)}>
            <option value="">— Chưa có —</option>
            {(employeeList?.items ?? [])
              .filter((e) => e.status !== "resigned")
              .map((e) => (
                <option key={e.id} value={e.id}>
                  {e.full_name} ({e.employee_code})
                </option>
              ))}
          </select>
        </Field>

        <Field label="Mô tả" error={errors.description}>
          <textarea
            {...register("description")}
            {...inputProps(!!errors.description)}
            rows={2}
          />
        </Field>

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
          >
            Huỷ
          </button>
          <SubmitButton pending={active.isPending}>
            {isEdit ? "Lưu thay đổi" : "Tạo phòng ban"}
          </SubmitButton>
        </div>
      </form>
    </Modal>
  );
}
