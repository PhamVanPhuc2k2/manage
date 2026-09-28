"use client";

import { useState } from "react";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { OrgChart } from "@/features/departments/OrgChart";
import { useDepartmentTree } from "@/features/departments/queries";
import type { Department } from "@/features/departments/types";
import { useEmployees } from "@/features/employees/queries";
import { STATUS_LABEL } from "@/features/employees/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

export default function OrgChartPage() {
  const { can } = usePermission();
  const { data: tree = [], isPending, error } = useDepartmentTree();
  const [selected, setSelected] = useState<Department | null>(null);

  return (
    <AppShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold">Sơ đồ tổ chức</h1>
        <p className="mt-1 text-sm text-neutral-500">
          Bấm vào một phòng để xem ai đang làm ở đó.
        </p>
      </div>

      {isPending && <p className="text-sm text-neutral-500">Đang tải...</p>}
      {error && (
        <p className="text-sm text-red-600">
          {error instanceof ApiError ? error.message : "Không tải được sơ đồ"}
        </p>
      )}
      {!isPending && !error && tree.length === 0 && (
        <p className="text-sm text-neutral-500">
          Chưa có phòng ban nào.{" "}
          {can("department:create") && (
            <Link href="/departments" className="underline">
              Tạo phòng ban
            </Link>
          )}
        </p>
      )}

      {tree.length > 0 && (
        <OrgChart
          roots={tree}
          selectedId={selected?.id}
          onSelect={(d) => setSelected((cur) => (cur?.id === d.id ? null : d))}
        />
      )}

      {selected && can("employee:read") && (
        <DepartmentMembers
          department={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </AppShell>
  );
}

function DepartmentMembers({
  department,
  onClose,
}: {
  department: Department;
  onClose: () => void;
}) {
  // 100 là trần page_size của API. Phòng đông hơn thế thì bảng này chỉ là
  // xem nhanh, và dòng cuối nói rõ đang hiện bao nhiêu trên tổng số.
  const { data, isPending } = useEmployees({
    departmentId: department.id,
    pageSize: 100,
  });
  const items = data?.items ?? [];
  const total = data?.meta?.total_items ?? 0;

  return (
    <section className="mt-6 rounded border border-neutral-200 dark:border-neutral-800">
      <header className="flex items-center justify-between border-b border-neutral-200 px-4 py-3 dark:border-neutral-800">
        <div>
          <h2 className="font-medium">{department.name}</h2>
          <p className="text-xs text-neutral-500">
            Trưởng phòng: {department.manager_name || "chưa có"}
          </p>
        </div>
        <button
          onClick={onClose}
          className="text-sm text-neutral-500 hover:underline"
        >
          Đóng
        </button>
      </header>

      {isPending ? (
        <p className="px-4 py-6 text-sm text-neutral-500">Đang tải...</p>
      ) : items.length === 0 ? (
        // Phạm vi quyền áp ở máy chủ: nhân viên thường chỉ thấy chính mình,
        // nên "không có ai" có thể là "không có ai bạn được xem".
        <p className="px-4 py-6 text-sm text-neutral-500">
          Không có nhân viên nào bạn được xem trong phòng này.
        </p>
      ) : (
        <ul className="divide-y divide-neutral-200 dark:divide-neutral-800">
          {items.map((e) => (
            <li
              key={e.id}
              className="flex items-center justify-between px-4 py-2 text-sm"
            >
              <Link
                href={`/employees/${e.id}`}
                className="font-medium underline-offset-4 hover:underline"
              >
                {e.full_name}
              </Link>
              <span className="text-neutral-500">
                {e.position_name || "—"} · {STATUS_LABEL[e.status]}
              </span>
            </li>
          ))}
        </ul>
      )}

      {total > items.length && (
        <p className="border-t border-neutral-200 px-4 py-2 text-xs text-neutral-500 dark:border-neutral-800">
          Đang hiện {items.length}/{total} người.
        </p>
      )}
    </section>
  );
}
