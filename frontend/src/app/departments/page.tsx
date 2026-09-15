"use client";

import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { DepartmentForm } from "@/features/departments/DepartmentForm";
import {
  useDeleteDepartment,
  useDepartmentTree,
} from "@/features/departments/queries";
import type { Department } from "@/features/departments/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

/**
 * Làm phẳng cây thành danh sách kèm độ sâu.
 *
 * Bảng HTML không lồng được hàng, nên vẽ cây bằng cách thụt lề theo depth.
 */
function flatten(nodes: Department[], depth = 0): { node: Department; depth: number }[] {
  return nodes.flatMap((n) => [
    { node: n, depth },
    ...flatten(n.children ?? [], depth + 1),
  ]);
}

export default function DepartmentsPage() {
  const { can } = usePermission();
  const { data: tree = [], isPending, error } = useDepartmentTree();
  const remove = useDeleteDepartment();

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Department | undefined>();

  const rows = flatten(tree);

  function openCreate() {
    setEditing(undefined);
    setFormOpen(true);
  }
  function openEdit(d: Department) {
    setEditing(d);
    setFormOpen(true);
  }

  const deleteError =
    remove.error instanceof ApiError
      ? remove.error.message
      : remove.error
        ? "Không xoá được phòng ban"
        : null;

  return (
    <AppShell>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Phòng ban</h1>
        {can("department:create") && (
          <button
            onClick={openCreate}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Thêm phòng ban
          </button>
        )}
      </div>

      <div className="mb-4 space-y-2">
        <FormError
          message={
            error instanceof ApiError
              ? error.message
              : error
                ? "Không tải được danh sách"
                : null
          }
        />
        <FormError message={deleteError} />
      </div>

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Tên phòng ban</th>
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Trưởng phòng</th>
              <th className="px-4 py-2 text-right font-medium">Nhân viên</th>
              <th className="px-4 py-2" />
            </tr>
          </thead>
          <tbody>
            {isPending && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!isPending && rows.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có phòng ban nào
                </td>
              </tr>
            )}
            {rows.map(({ node, depth }) => (
              <tr
                key={node.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2">
                  <span style={{ paddingLeft: depth * 20 }}>
                    {depth > 0 && <span className="mr-2 text-neutral-400">└</span>}
                    {node.name}
                  </span>
                </td>
                <td className="px-4 py-2 font-mono text-xs">{node.code}</td>
                <td className="px-4 py-2">{node.manager_name || "—"}</td>
                <td className="px-4 py-2 text-right">{node.employee_count}</td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  {can("department:update") && (
                    <button
                      onClick={() => openEdit(node)}
                      className="text-xs underline-offset-4 hover:underline"
                    >
                      Sửa
                    </button>
                  )}
                  {can("department:delete") && (
                    <button
                      onClick={() => {
                        // Backend từ chối nếu phòng còn người hoặc còn phòng
                        // con, và trả về thông báo giải thích rõ — nên ở đây
                        // chỉ cần hỏi xác nhận chung.
                        if (confirm(`Xoá phòng ban "${node.name}"?`)) {
                          remove.mutate(node.id);
                        }
                      }}
                      className="ml-3 text-xs text-red-700 underline-offset-4 hover:underline dark:text-red-400"
                    >
                      Xoá
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {formOpen && (
        <DepartmentForm editing={editing} onClose={() => setFormOpen(false)} />
      )}
    </AppShell>
  );
}
