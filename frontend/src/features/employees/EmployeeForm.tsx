"use client";

import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";

import { Field, FormError, SubmitButton, inputProps } from "@/components/form";
import { useDepartments } from "@/features/departments/queries";
import { usePositions } from "@/features/positions/queries";
import { ApiError } from "@/lib/api-client";

import { useEmployees } from "./queries";
import {
  emptyEmployeeForm,
  employeeSchema,
  type EmployeeFormValues,
} from "./schema";
import type { Employee } from "./types";

/** Đổi entity từ API thành giá trị form (ngày ISO → YYYY-MM-DD). */
export function toFormValues(e: Employee): EmployeeFormValues {
  const date = (s?: string) => (s ? s.slice(0, 10) : "");

  return {
    employee_code: e.employee_code,
    full_name: e.full_name,
    email: e.email,
    phone: e.phone ?? "",
    date_of_birth: date(e.date_of_birth),
    gender: e.gender ?? "",
    address: e.address ?? "",
    department_id: e.department_id ?? "",
    position_id: e.position_id ?? "",
    manager_id: e.manager_id ?? "",
    work_mode: e.work_mode,
    status: e.status,
    joined_at: date(e.joined_at),
    resigned_at: date(e.resigned_at),
  };
}

type Props = {
  defaultValues?: EmployeeFormValues;
  /** Id của nhân viên đang sửa — dùng để loại chính họ khỏi danh sách cấp trên. */
  editingId?: string;
  submitLabel: string;
  pending: boolean;
  error?: unknown;
  onSubmit: (values: EmployeeFormValues) => void;
  cancelHref: string;
};

export function EmployeeForm({
  defaultValues = emptyEmployeeForm,
  editingId,
  submitLabel,
  pending,
  error,
  onSubmit,
  cancelHref,
}: Props) {
  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<EmployeeFormValues>({
    resolver: zodResolver(employeeSchema),
    defaultValues,
  });

  const { data: departments = [] } = useDepartments();
  const { data: positions = [] } = usePositions();

  // Danh sách chọn cấp trên. Lấy trang đầu 100 người là đủ cho công ty cỡ
  // này; khi nào vượt thì đổi sang ô tìm kiếm có gợi ý.
  const { data: employeeList } = useEmployees({ pageSize: 100 });
  const managers = (employeeList?.items ?? []).filter(
    // Không cho chọn chính mình làm cấp trên. Backend cũng chặn, nhưng chặn
    // ở đây thì người dùng không phải bấm rồi mới thấy lỗi.
    (e) => e.id !== editingId && e.status !== "resigned",
  );

  const message =
    error instanceof ApiError
      ? error.message
      : error
        ? "Không lưu được. Vui lòng thử lại."
        : null;

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="max-w-3xl space-y-6">
      <FormError message={message} />

      <section className="space-y-4 rounded border border-neutral-200 p-4 dark:border-neutral-800">
        <h2 className="font-medium">Thông tin cơ bản</h2>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Mã nhân viên" required error={errors.employee_code}>
            <input
              {...register("employee_code")}
              {...inputProps(!!errors.employee_code)}
              placeholder="NV0001"
            />
          </Field>

          <Field label="Họ và tên" required error={errors.full_name}>
            <input
              {...register("full_name")}
              {...inputProps(!!errors.full_name)}
              placeholder="Nguyễn Văn A"
            />
          </Field>

          <Field label="Email" required error={errors.email}>
            <input
              {...register("email")}
              {...inputProps(!!errors.email)}
              type="email"
              placeholder="nva@congty.vn"
            />
          </Field>

          <Field
            label="Số điện thoại"
            error={errors.phone}
            hint="Để trống nếu chưa có"
          >
            <input
              {...register("phone")}
              {...inputProps(!!errors.phone)}
              placeholder="0901234567"
            />
          </Field>

          <Field label="Ngày sinh" error={errors.date_of_birth}>
            <input
              {...register("date_of_birth")}
              {...inputProps(!!errors.date_of_birth)}
              type="date"
            />
          </Field>

          <Field label="Giới tính" error={errors.gender}>
            <select {...register("gender")} {...inputProps(!!errors.gender)}>
              <option value="">— Chưa xác định —</option>
              <option value="nam">Nam</option>
              <option value="nu">Nữ</option>
              <option value="khac">Khác</option>
            </select>
          </Field>
        </div>

        <Field label="Địa chỉ" error={errors.address}>
          <textarea
            {...register("address")}
            {...inputProps(!!errors.address)}
            rows={2}
          />
        </Field>
      </section>

      <section className="space-y-4 rounded border border-neutral-200 p-4 dark:border-neutral-800">
        <h2 className="font-medium">Công việc</h2>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Phòng ban" error={errors.department_id}>
            <select
              {...register("department_id")}
              {...inputProps(!!errors.department_id)}
            >
              <option value="">— Chưa phân phòng —</option>
              {departments.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.name}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Chức vụ" error={errors.position_id}>
            <select
              {...register("position_id")}
              {...inputProps(!!errors.position_id)}
            >
              <option value="">— Chưa có chức vụ —</option>
              {positions.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Cấp trên trực tiếp" error={errors.manager_id}>
            <select
              {...register("manager_id")}
              {...inputProps(!!errors.manager_id)}
            >
              <option value="">— Không có —</option>
              {managers.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.full_name} ({m.employee_code})
                </option>
              ))}
            </select>
          </Field>

          <Field label="Hình thức làm việc" required error={errors.work_mode}>
            <select
              {...register("work_mode")}
              {...inputProps(!!errors.work_mode)}
            >
              <option value="onsite">Tại văn phòng</option>
              <option value="remote">Từ xa</option>
              <option value="hybrid">Kết hợp</option>
            </select>
          </Field>

          <Field label="Trạng thái" required error={errors.status}>
            <select {...register("status")} {...inputProps(!!errors.status)}>
              <option value="probation">Thử việc</option>
              <option value="official">Chính thức</option>
              <option value="resigned">Đã nghỉ việc</option>
            </select>
          </Field>

          <Field label="Ngày vào làm" required error={errors.joined_at}>
            <input
              {...register("joined_at")}
              {...inputProps(!!errors.joined_at)}
              type="date"
            />
          </Field>

          <Field
            label="Ngày nghỉ việc"
            error={errors.resigned_at}
            hint="Chỉ điền khi trạng thái là Đã nghỉ việc"
          >
            <input
              {...register("resigned_at")}
              {...inputProps(!!errors.resigned_at)}
              type="date"
            />
          </Field>
        </div>
      </section>

      <div className="flex items-center gap-3">
        <SubmitButton pending={pending}>{submitLabel}</SubmitButton>
        <Link
          href={cancelHref}
          className="rounded border border-neutral-300 px-4 py-2 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
        >
          Huỷ
        </Link>
      </div>
    </form>
  );
}
