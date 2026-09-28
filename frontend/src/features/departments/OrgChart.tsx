"use client";

import { useState } from "react";

import styles from "./OrgChart.module.css";
import type { Department } from "./types";

/**
 * Sơ đồ tổ chức theo phòng ban, vẽ từ trên xuống.
 *
 * Mỗi ô là một phòng: tên, trưởng phòng và số người. Số người hiện hai con
 * số khi phòng có phòng con — "5 người · 23 cả khối" — vì câu hỏi thật của
 * người xem thường là "khối này bao nhiêu người", không phải "riêng phòng
 * này".
 */
export function OrgChart({
  roots,
  selectedId,
  onSelect,
}: {
  roots: Department[];
  selectedId?: string;
  onSelect: (d: Department) => void;
}) {
  // Mặc định mở hai cấp đầu. Mở hết thì công ty vài chục phòng thành một
  // dải ngang dài hàng mét; đóng hết thì người xem phải bấm mới thấy gì.
  const [collapsed, setCollapsed] = useState<Set<string>>(() => {
    const s = new Set<string>();
    const walk = (nodes: Department[], depth: number) => {
      for (const n of nodes) {
        if (depth >= 1 && n.children?.length) s.add(n.id);
        walk(n.children ?? [], depth + 1);
      }
    };
    walk(roots, 0);
    return s;
  });

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const setAll = (open: boolean) => {
    const s = new Set<string>();
    if (!open) {
      const walk = (nodes: Department[]) => {
        for (const n of nodes) {
          if (n.children?.length) s.add(n.id);
          walk(n.children ?? []);
        }
      };
      walk(roots);
    }
    setCollapsed(s);
  };

  return (
    <div>
      <div className="mb-3 flex gap-2 text-sm">
        <button
          onClick={() => setAll(true)}
          className="rounded border border-neutral-300 px-3 py-1 dark:border-neutral-700"
        >
          Mở hết
        </button>
        <button
          onClick={() => setAll(false)}
          className="rounded border border-neutral-300 px-3 py-1 dark:border-neutral-700"
        >
          Thu gọn hết
        </button>
      </div>

      {/* Cuộn ngang trong khung này chứ không đẩy cả trang rộng ra. */}
      <div className="overflow-x-auto pb-4">
        <div className={`${styles.tree} mx-auto w-max`}>
          <ul>
            {roots.map((n) => (
              <Node
                key={n.id}
                node={n}
                collapsed={collapsed}
                onToggle={toggle}
                selectedId={selectedId}
                onSelect={onSelect}
              />
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}

function Node({
  node,
  collapsed,
  onToggle,
  selectedId,
  onSelect,
}: {
  node: Department;
  collapsed: Set<string>;
  onToggle: (id: string) => void;
  selectedId?: string;
  onSelect: (d: Department) => void;
}) {
  const children = node.children ?? [];
  const isClosed = collapsed.has(node.id);
  const total = subtreeCount(node);
  const selected = node.id === selectedId;

  return (
    <li>
      <button
        onClick={() => onSelect(node)}
        aria-pressed={selected}
        className={`w-44 rounded-lg border bg-white px-3 py-2 text-left text-sm shadow-sm transition hover:border-neutral-500 dark:bg-neutral-900 ${
          selected
            ? "border-neutral-900 ring-1 ring-neutral-900 dark:border-white dark:ring-white"
            : "border-neutral-200 dark:border-neutral-700"
        }`}
      >
        <div className="truncate font-medium" title={node.name}>
          {node.name}
        </div>
        <div className="font-mono text-[11px] text-neutral-500">
          {node.code}
        </div>
        <div
          className="mt-1 truncate text-xs text-neutral-600 dark:text-neutral-400"
          title={node.manager_name}
        >
          {node.manager_name || "Chưa có trưởng phòng"}
        </div>
        <div className="mt-1 text-xs text-neutral-500">
          {node.employee_count} người
          {children.length > 0 &&
            total !== node.employee_count &&
            ` · ${total} cả khối`}
        </div>
      </button>

      {children.length > 0 && (
        <button
          onClick={() => onToggle(node.id)}
          aria-expanded={!isClosed}
          aria-label={
            isClosed
              ? `Mở các phòng con của ${node.name}`
              : `Thu gọn ${node.name}`
          }
          className="relative z-10 -mt-2 rounded-full border border-neutral-300 bg-white px-2 text-xs leading-5 text-neutral-600 hover:bg-neutral-100 dark:border-neutral-700 dark:bg-neutral-900 dark:text-neutral-300 dark:hover:bg-neutral-800"
        >
          {isClosed ? `+${children.length}` : "−"}
        </button>
      )}

      {children.length > 0 && !isClosed && (
        <ul>
          {children.map((c) => (
            <Node
              key={c.id}
              node={c}
              collapsed={collapsed}
              onToggle={onToggle}
              selectedId={selectedId}
              onSelect={onSelect}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

function subtreeCount(n: Department): number {
  return (
    n.employee_count +
    (n.children ?? []).reduce((s, c) => s + subtreeCount(c), 0)
  );
}
