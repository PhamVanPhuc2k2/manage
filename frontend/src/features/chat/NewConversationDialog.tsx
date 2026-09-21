"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useEmployees } from "@/features/employees/queries";
import { useAuth } from "@/lib/auth/useAuth";
import { useDebounce } from "@/lib/use-debounce";

import * as api from "./api";
import { chatKeys } from "./queries";

/**
 * Hộp thoại "cuộc trò chuyện mới".
 *
 * Một hộp thoại cho cả 1-1 lẫn nhóm: người dùng chỉ có một ý định ("nói
 * chuyện với ai đó"), và bắt họ chọn loại trước khi chọn người là bắt họ trả
 * lời câu hỏi của hệ thống thay vì câu hỏi của mình.
 */
export function NewConversationDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [search, setSearch] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const qc = useQueryClient();
  const { user } = useAuth();
  const debounced = useDebounce(search, 300);
  const { data } = useEmployees({ search: debounced, page: 1, pageSize: 50 });

  // Chọn đúng một người thì đó là hội thoại 1-1; từ hai người trở lên là nhóm.
  const isGroup = picked.length > 1;
  const candidates = (data?.items ?? []).filter((e) => e.id !== user?.employee_id);

  async function onCreate() {
    setError(null);
    setBusy(true);
    try {
      const c = isGroup
        ? await api.createGroup(name.trim() || "Nhóm mới", picked)
        : await api.openDirect(picked[0]);

      void qc.invalidateQueries({ queryKey: chatKeys.all });
      onCreated(c.id);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Không tạo được cuộc trò chuyện");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-4 shadow-xl dark:border-neutral-700 dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-3 flex items-center justify-between">
          <h2 className="font-medium">Cuộc trò chuyện mới</h2>
          <button onClick={onClose} className="text-neutral-500">
            ✕
          </button>
        </div>

        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Tìm nhân viên..."
          className="mb-2 w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-900"
        />

        <div className="mb-3 max-h-64 overflow-y-auto rounded border border-neutral-200 dark:border-neutral-700">
          {candidates.length === 0 && (
            <div className="py-4 text-center text-sm text-neutral-500">
              Không tìm thấy nhân viên nào
            </div>
          )}
          {candidates.map((e) => (
            <label
              key={e.id}
              className="flex items-center gap-2 border-b border-neutral-100 px-2 py-1.5 text-sm last:border-0 dark:border-neutral-800"
            >
              <input
                type="checkbox"
                checked={picked.includes(e.id)}
                onChange={(ev) =>
                  setPicked((prev) =>
                    ev.target.checked ? [...prev, e.id] : prev.filter((x) => x !== e.id),
                  )
                }
              />
              <span className="min-w-0 flex-1 truncate">{e.full_name}</span>
              <span className="text-xs text-neutral-400">{e.employee_code}</span>
            </label>
          ))}
        </div>

        {isGroup && (
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Tên nhóm"
            className="mb-3 w-full rounded border border-neutral-300 px-2 py-1.5 text-sm dark:border-neutral-700 dark:bg-neutral-900"
          />
        )}

        {error && <div className="mb-2 text-xs text-red-600">{error}</div>}

        <div className="flex justify-end gap-2">
          <button
            onClick={onClose}
            className="rounded border border-neutral-300 px-3 py-1.5 text-sm transition hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
          >
            Huỷ
          </button>
          <button
            onClick={() => void onCreate()}
            disabled={busy || picked.length === 0}
            className="rounded bg-blue-600 px-4 py-1.5 text-sm text-white transition hover:bg-blue-700 disabled:opacity-50"
          >
            {isGroup ? `Tạo nhóm (${picked.length})` : "Nhắn tin"}
          </button>
        </div>
      </div>
    </div>
  );
}
