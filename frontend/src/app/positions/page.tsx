"use client";

import { useState } from "react";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import { PositionForm } from "@/features/positions/PositionForm";
import { useDeletePosition, usePositions } from "@/features/positions/queries";
import type { Position } from "@/features/positions/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

const formatVND = (n?: number) =>
  n === undefined || n === null
    ? "—"
    : new Intl.NumberFormat("vi-VN", {
        style: "currency",
        currency: "VND",
        maximumFractionDigits: 0,
      }).format(n);

export default function PositionsPage() {
  const { can } = usePermission();
  const { data: items = [], isPending, error } = usePositions();
  const remove = useDeletePosition();

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Position | undefined>();

  const deleteError =
    remove.error instanceof ApiError
      ? remove.error.message
      : remove.error
        ? "Không xoá được chức vụ"
        : null;

  return (
    <AppShell>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Chức vụ</h1>
        {can("position:manage") && (
          <button
            onClick={() => {
              setEditing(undefined);
              setFormOpen(true);
            }}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900"
          >
            Thêm chức vụ
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
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Tên chức vụ</th>
              <th className="px-4 py-2 text-right font-medium">Lương tối thiểu</th>
              <th className="px-4 py-2 text-right font-medium">Lương tối đa</th>
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
            {!isPending && items.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có chức vụ nào
                </td>
              </tr>
            )}
            {items.map((p) => (
              <tr
                key={p.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2 font-mono text-xs">{p.code}</td>
                <td className="px-4 py-2">{p.name}</td>
                <td className="px-4 py-2 text-right">{formatVND(p.salary_min)}</td>
                <td className="px-4 py-2 text-right">{formatVND(p.salary_max)}</td>
                <td className="px-4 py-2 text-right whitespace-nowrap">
                  {can("position:manage") && (
                    <>
                      <button
                        onClick={() => {
                          setEditing(p);
                          setFormOpen(true);
                        }}
                        className="text-xs underline-offset-4 hover:underline"
                      >
                        Sửa
                      </button>
                      <button
                        onClick={() => {
                          // Backend từ chối nếu còn nhân viên đang giữ chức
                          // vụ này và nói rõ số lượng.
                          if (confirm(`Xoá chức vụ "${p.name}"?`)) {
                            remove.mutate(p.id);
                          }
                        }}
                        className="ml-3 text-xs text-red-700 underline-offset-4 hover:underline dark:text-red-400"
                      >
                        Xoá
                      </button>
                    </>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {formOpen && (
        <PositionForm editing={editing} onClose={() => setFormOpen(false)} />
      )}
    </AppShell>
  );
}
