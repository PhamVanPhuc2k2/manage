"use client";

import { useEffect, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { api, ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/AuthProvider";

type Department = {
  id: string;
  parent_id?: string;
  code: string;
  name: string;
  description?: string;
  manager_name?: string;
  employee_count: number;
  children?: Department[];
};

/** Vẽ một nhánh của cây, thụt lề theo độ sâu. */
function TreeNode({ node, depth }: { node: Department; depth: number }) {
  return (
    <>
      <tr className="border-t border-neutral-200 dark:border-neutral-800">
        <td className="px-4 py-2">
          <span style={{ paddingLeft: depth * 20 }}>
            {depth > 0 && <span className="mr-2 text-neutral-400">└</span>}
            {node.name}
          </span>
        </td>
        <td className="px-4 py-2 font-mono text-xs">{node.code}</td>
        <td className="px-4 py-2">{node.manager_name || "—"}</td>
        <td className="px-4 py-2 text-right">{node.employee_count}</td>
      </tr>
      {node.children?.map((c) => (
        <TreeNode key={c.id} node={c} depth={depth + 1} />
      ))}
    </>
  );
}

export default function DepartmentsPage() {
  const { can } = usePermission();

  const [tree, setTree] = useState<Department[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Gọi API bên trong hàm async, không setState đồng bộ ngay trong thân
  // effect — React 19 cảnh báo vì nó gây render dây chuyền.
  //
  // Cờ `cancelled` cũng sửa một lỗi thật: nếu người dùng rời trang trước khi
  // request xong, setState trên component đã unmount sẽ báo lỗi.
  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const { data } = await api.get<Department[]>("/departments/tree");
        if (!cancelled) setTree(data ?? []);
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof ApiError ? e.message : "Không tải được danh sách");
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <AppShell>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Phòng ban</h1>
        {can("department:create") && (
          <button className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900">
            Thêm phòng ban
          </button>
        )}
      </div>

      {error && (
        <div className="mb-4 rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300">
          {error}
        </div>
      )}

      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Tên phòng ban</th>
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Trưởng phòng</th>
              <th className="px-4 py-2 text-right font-medium">Số nhân viên</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-neutral-500">
                  Đang tải...
                </td>
              </tr>
            )}
            {!loading && tree.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có phòng ban nào
                </td>
              </tr>
            )}
            {!loading &&
              tree.map((d) => <TreeNode key={d.id} node={d} depth={0} />)}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}
