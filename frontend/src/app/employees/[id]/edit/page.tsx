"use client";

import { useParams, useRouter } from "next/navigation";

import { AppShell } from "@/components/AppShell";
import {
  EmployeeForm,
  toFormValues,
} from "@/features/employees/EmployeeForm";
import { useEmployee, useUpdateEmployee } from "@/features/employees/queries";
import { toEmployeePayload } from "@/features/employees/schema";
import { ApiError } from "@/lib/api-client";

export default function EditEmployeePage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();

  const { data: employee, isPending, error } = useEmployee(id);
  const update = useUpdateEmployee(id);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải...</p>
      </AppShell>
    );
  }

  if (error || !employee) {
    return (
      <AppShell>
        <div className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error instanceof ApiError ? error.message : "Không tìm thấy nhân viên"}
        </div>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <h1 className="mb-1 text-xl font-semibold">Sửa hồ sơ</h1>
      <p className="mb-6 text-sm text-neutral-500">
        {employee.full_name} · {employee.employee_code}
      </p>

      <EmployeeForm
        // key buộc form dựng lại khi dữ liệu từ server về, để defaultValues
        // được áp dụng. react-hook-form chỉ đọc defaultValues lúc khởi tạo.
        key={employee.id}
        defaultValues={toFormValues(employee)}
        editingId={employee.id}
        submitLabel="Lưu thay đổi"
        pending={update.isPending}
        error={update.error}
        cancelHref={`/employees/${id}`}
        onSubmit={(values) => {
          update.mutate(toEmployeePayload(values), {
            onSuccess: () => router.push(`/employees/${id}`),
          });
        }}
      />
    </AppShell>
  );
}
