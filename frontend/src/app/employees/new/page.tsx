"use client";

import { useRouter } from "next/navigation";

import { AppShell } from "@/components/AppShell";
import { EmployeeForm } from "@/features/employees/EmployeeForm";
import { useCreateEmployee } from "@/features/employees/queries";
import { toEmployeePayload } from "@/features/employees/schema";
import type { Employee } from "@/features/employees/types";

export default function NewEmployeePage() {
  const router = useRouter();
  const create = useCreateEmployee();

  return (
    <AppShell>
      <h1 className="mb-6 text-xl font-semibold">Thêm nhân viên</h1>

      <EmployeeForm
        submitLabel="Tạo nhân viên"
        pending={create.isPending}
        error={create.error}
        cancelHref="/employees"
        onSubmit={(values) => {
          create.mutate(toEmployeePayload(values), {
            // Chuyển thẳng sang trang chi tiết: người dùng vừa tạo xong
            // thường muốn làm tiếp ngay (tạo tài khoản, gán vai trò).
            onSuccess: (e: Employee) => router.push(`/employees/${e.id}`),
          });
        }}
      />
    </AppShell>
  );
}
