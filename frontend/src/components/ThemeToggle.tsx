"use client";

import { useTheme, type Theme } from "@/lib/theme";

const OPTIONS: { value: Theme; label: string }[] = [
  { value: "system", label: "Theo máy" },
  { value: "light", label: "Sáng" },
  { value: "dark", label: "Tối" },
];

/**
 * Chọn giao diện. Một ô chọn thay vì nút bấm xoay vòng: ba trạng thái mà
 * chỉ có một nút thì người dùng phải bấm thử để biết lần bấm tới ra gì.
 */
export function ThemeToggle() {
  const { theme, setTheme } = useTheme();

  return (
    <select
      aria-label="Giao diện"
      value={theme}
      onChange={(e) => setTheme(e.target.value as Theme)}
      className="rounded border border-neutral-300 bg-transparent px-2 py-1 text-sm dark:border-neutral-700 dark:bg-neutral-950"
    >
      {OPTIONS.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}
