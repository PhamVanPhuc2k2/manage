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
import { ApiError } from "@/lib/api-client";

import { useCreatePosition, useUpdatePosition } from "./queries";
import type { Position } from "./types";

const schema = z
  .object({
    code: z.string().trim().min(1, "Mã chức vụ không được để trống").max(50),
    name: z.string().trim().min(1, "Tên chức vụ không được để trống").max(255),
    // Input number trả về chuỗi, nên nhận chuỗi rồi tự đổi.
    salary_min: z.string(),
    salary_max: z.string(),
  })
  .refine(
    (v) =>
      !v.salary_min ||
      !v.salary_max ||
      Number(v.salary_min) <= Number(v.salary_max),
    { message: "Lương tối thiểu không được lớn hơn lương tối đa", path: ["salary_max"] },
  )
  .refine((v) => !v.salary_min || Number(v.salary_min) >= 0, {
    message: "Lương không được âm",
    path: ["salary_min"],
  });

type Values = z.infer<typeof schema>;

export function PositionForm({
  editing,
  onClose,
}: {
  editing?: Position;
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
      salary_min: editing?.salary_min != null ? String(editing.salary_min) : "",
      salary_max: editing?.salary_max != null ? String(editing.salary_max) : "",
    },
  });

  const create = useCreatePosition();
  const update = useUpdatePosition(editing?.id ?? "");
  const active = isEdit ? update : create;

  const message =
    active.error instanceof ApiError
      ? active.error.message
      : active.error
        ? "Không lưu được"
        : null;

  function onSubmit(v: Values) {
    // Chuỗi rỗng phải thành null, không phải 0 — "chưa đặt khung lương"
    // khác hẳn "khung lương bằng 0".
    const num = (s: string) => (s.trim() === "" ? null : Number(s));

    active.mutate(
      {
        code: v.code.trim(),
        name: v.name.trim(),
        salary_min: num(v.salary_min),
        salary_max: num(v.salary_max),
      } as never,
      { onSuccess: onClose },
    );
  }

  return (
    <Modal title={isEdit ? "Sửa chức vụ" : "Thêm chức vụ"} onClose={onClose}>
      <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
        <FormError message={message} />

        <Field label="Mã chức vụ" required error={errors.code}>
          <input
            {...register("code")}
            {...inputProps(!!errors.code)}
            placeholder="DEV"
          />
        </Field>

        <Field label="Tên chức vụ" required error={errors.name}>
          <input
            {...register("name")}
            {...inputProps(!!errors.name)}
            placeholder="Lập trình viên"
          />
        </Field>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Lương tối thiểu"
            error={errors.salary_min}
            hint="Để trống nếu chưa có khung"
          >
            <input
              {...register("salary_min")}
              {...inputProps(!!errors.salary_min)}
              type="number"
              min={0}
              step={100000}
              placeholder="15000000"
            />
          </Field>

          <Field label="Lương tối đa" error={errors.salary_max}>
            <input
              {...register("salary_max")}
              {...inputProps(!!errors.salary_max)}
              type="number"
              min={0}
              step={100000}
              placeholder="25000000"
            />
          </Field>
        </div>

        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-neutral-300 px-4 py-2 text-sm dark:border-neutral-700"
          >
            Huỷ
          </button>
          <SubmitButton pending={active.isPending}>
            {isEdit ? "Lưu thay đổi" : "Tạo chức vụ"}
          </SubmitButton>
        </div>
      </form>
    </Modal>
  );
}
