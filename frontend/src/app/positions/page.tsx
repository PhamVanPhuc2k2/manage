"use client";

import { useEffect, useState } from "react";

import { AppShell } from "@/components/AppShell";
import { api, ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/AuthProvider";

type Position = {
  id: string;
  code: string;
  name: string;
  salary_min?: number;
  salary_max?: number;
};

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

  const [items, setItems] = useState<Position[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Xem ghi chú ở trang phòng ban: không setState đồng bộ trong thân effect,
  // và có cờ cancelled để tránh setState sau khi component đã unmount.
  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const { data } = await api.get<Position[]>("/positions");
        if (!cancelled) setItems(data ?? []);
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
        <h1 className="text-xl font-semibold">Chức vụ</h1>
        {can("position:manage") && (
          <button className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white dark:bg-white dark:text-neutral-900">
            Thêm chức vụ
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
              <th className="px-4 py-2 font-medium">Mã</th>
              <th className="px-4 py-2 font-medium">Tên chức vụ</th>
              <th className="px-4 py-2 text-right font-medium">Lương tối thiểu</th>
              <th className="px-4 py-2 text-right font-medium">Lương tối đa</th>
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
            {!loading && items.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-neutral-500">
                  Chưa có chức vụ nào
                </td>
              </tr>
            )}
            {!loading &&
              items.map((p) => (
                <tr
                  key={p.id}
                  className="border-t border-neutral-200 dark:border-neutral-800"
                >
                  <td className="px-4 py-2 font-mono text-xs">{p.code}</td>
                  <td className="px-4 py-2">{p.name}</td>
                  <td className="px-4 py-2 text-right">{formatVND(p.salary_min)}</td>
                  <td className="px-4 py-2 text-right">{formatVND(p.salary_max)}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}
