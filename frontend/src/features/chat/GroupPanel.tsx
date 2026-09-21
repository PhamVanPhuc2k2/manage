"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { useEmployees } from "@/features/employees/queries";
import { useAuth } from "@/lib/auth/useAuth";

import * as api from "./api";
import { chatKeys, useConversation } from "./queries";
import { StatusDot } from "./StatusDot";

/**
 * Bảng chi tiết bên phải khung chat: thành viên, ghim, tắt thông báo, rời nhóm.
 */
export function GroupPanel({
  conversationId,
  onClose,
}: {
  conversationId: string;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const { user } = useAuth();
  const { data } = useConversation(conversationId);
  const [adding, setAdding] = useState(false);
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);

  const c = data?.conversation;
  const members = data?.members ?? [];
  if (!c) return null;

  const canManage = c.is_admin && !c.managed && c.kind !== "direct";

  function refresh() {
    void qc.invalidateQueries({ queryKey: chatKeys.all });
  }

  async function run(fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      refresh();
    } finally {
      setBusy(false);
    }
  }

  return (
    <aside className="flex w-72 shrink-0 flex-col border-l border-neutral-200 dark:border-neutral-800">
      <div className="flex items-center justify-between border-b border-neutral-200 px-3 py-2.5 dark:border-neutral-800">
        <span className="text-sm font-medium">Chi tiết</span>
        <button onClick={onClose} className="text-neutral-500">
          ✕
        </button>
      </div>

      <div className="flex-1 overflow-y-auto p-3">
        {/* Tuỳ chọn riêng của người xem: ai cũng dùng được, kể cả với nhóm
            do hệ thống quản lý. */}
        <div className="mb-4 space-y-2 text-sm">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={c.is_pinned}
              disabled={busy}
              onChange={(e) =>
                void run(() => api.setFlags(conversationId, { pinned: e.target.checked }))
              }
            />
            Ghim lên đầu danh sách
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              checked={c.is_muted}
              disabled={busy}
              onChange={(e) =>
                void run(() => api.setFlags(conversationId, { muted: e.target.checked }))
              }
            />
            Tắt thông báo
          </label>
        </div>

        {c.managed && (
          <div className="mb-3 rounded bg-neutral-100 px-2 py-1.5 text-xs text-neutral-600 dark:bg-neutral-800 dark:text-neutral-400">
            Nhóm này do hệ thống dựng theo{" "}
            {c.kind === "department" ? "phòng ban" : "dự án"} và tự đồng bộ thành
            viên.
          </div>
        )}

        {canManage && (
          <div className="mb-3">
            {renaming ? (
              <div className="flex gap-1">
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={c.name}
                  className="min-w-0 flex-1 rounded border border-neutral-300 px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-900"
                />
                <button
                  disabled={busy || !name.trim()}
                  onClick={() =>
                    void run(async () => {
                      await api.renameConversation(conversationId, name.trim());
                      setRenaming(false);
                      setName("");
                    })
                  }
                  className="rounded bg-blue-600 px-2 py-1 text-xs text-white disabled:opacity-50"
                >
                  Lưu
                </button>
              </div>
            ) : (
              <button
                onClick={() => setRenaming(true)}
                className="text-xs text-blue-600 hover:underline dark:text-blue-400"
              >
                Đổi tên nhóm
              </button>
            )}
          </div>
        )}

        <div className="mb-2 flex items-center justify-between">
          <span className="text-xs font-medium uppercase text-neutral-500">
            Thành viên ({members.length})
          </span>
          {canManage && (
            <button
              onClick={() => setAdding((v) => !v)}
              className="text-xs text-blue-600 hover:underline dark:text-blue-400"
            >
              {adding ? "Đóng" : "Thêm"}
            </button>
          )}
        </div>

        {adding && (
          <AddMembers
            conversationId={conversationId}
            existing={members.map((m) => m.employee_id)}
            onDone={() => {
              setAdding(false);
              refresh();
            }}
          />
        )}

        <div className="space-y-1">
          {members.map((m) => (
            <div key={m.employee_id} className="flex items-center gap-2 py-1">
              <StatusDot status={m.status} />
              <span className="min-w-0 flex-1 truncate text-sm">
                {m.employee_name}
                {m.employee_id === user?.employee_id && " (bạn)"}
              </span>
              {m.is_admin && (
                <span className="text-[10px] uppercase text-neutral-400">QTV</span>
              )}
              {canManage && m.employee_id !== user?.employee_id && (
                <button
                  disabled={busy}
                  onClick={() =>
                    void run(() => api.removeMember(conversationId, m.employee_id))
                  }
                  title="Gỡ khỏi nhóm"
                  className="text-xs text-neutral-400 hover:text-red-600"
                >
                  ✕
                </button>
              )}
            </div>
          ))}
        </div>
      </div>

      {c.kind === "group" && (
        <div className="border-t border-neutral-200 p-3 dark:border-neutral-800">
          <button
            disabled={busy}
            onClick={() => {
              if (!confirm("Rời khỏi nhóm này?")) return;
              void run(async () => {
                await api.leaveConversation(conversationId);
                onClose();
              });
            }}
            className="w-full rounded border border-red-300 px-3 py-1.5 text-sm text-red-600 transition hover:bg-red-50 disabled:opacity-50 dark:border-red-900 dark:hover:bg-red-950/30"
          >
            Rời nhóm
          </button>
        </div>
      )}
    </aside>
  );
}

function AddMembers({
  conversationId,
  existing,
  onDone,
}: {
  conversationId: string;
  existing: string[];
  onDone: () => void;
}) {
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const { data } = useEmployees({ page: 1, pageSize: 100 });

  const candidates = (data?.items ?? []).filter(
    (e) => !existing.includes(e.id),
  );

  return (
    <div className="mb-3 rounded border border-neutral-200 p-2 dark:border-neutral-700">
      <div className="max-h-40 overflow-y-auto">
        {candidates.length === 0 && (
          <div className="py-2 text-center text-xs text-neutral-500">
            Không còn ai để thêm
          </div>
        )}
        {candidates.map((e) => (
          <label key={e.id} className="flex items-center gap-2 py-0.5 text-sm">
            <input
              type="checkbox"
              checked={picked.includes(e.id)}
              onChange={(ev) =>
                setPicked((prev) =>
                  ev.target.checked ? [...prev, e.id] : prev.filter((x) => x !== e.id),
                )
              }
            />
            <span className="truncate">{e.full_name}</span>
          </label>
        ))}
      </div>

      <button
        disabled={busy || picked.length === 0}
        onClick={async () => {
          setBusy(true);
          try {
            await api.addMembers(conversationId, picked);
            setPicked([]);
            onDone();
          } finally {
            setBusy(false);
          }
        }}
        className="mt-2 w-full rounded bg-blue-600 px-2 py-1 text-xs text-white disabled:opacity-50"
      >
        Thêm {picked.length > 0 && `(${picked.length})`}
      </button>
    </div>
  );
}
