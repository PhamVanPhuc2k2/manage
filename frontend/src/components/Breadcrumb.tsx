"use client";

import { useEffect } from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { create } from "zustand";

/**
 * Đường dẫn "Nhân viên › Nguyễn Văn An › Sửa" ở thanh trên.
 *
 * Đoạn tĩnh lấy tên từ bảng dưới. Đoạn là một id (hồ sơ, dự án, kỳ lương)
 * thì trang đó tự báo tên thật qua useCrumbLabel — chỉ trang mới có dữ
 * liệu, và hiện một chuỗi UUID trên breadcrumb thì còn tệ hơn không hiện.
 */

const LABELS: Record<string, string> = {
  "/attendance": "Chấm công",
  "/attendance/exports": "Xuất Excel",
  "/attendance/team": "Công phòng ban",
  "/call-check": "Kiểm tra thiết bị",
  "/change-password": "Đổi mật khẩu",
  "/chat": "Tin nhắn",
  "/departments": "Phòng ban",
  "/employees": "Nhân viên",
  "/employees/imports": "Nhập từ tệp",
  "/employees/new": "Thêm mới",
  "/leaves": "Nghỉ phép",
  "/notifications": "Thông báo",
  "/org-chart": "Sơ đồ tổ chức",
  "/payroll": "Kỳ lương",
  "/payroll/my": "Phiếu lương",
  "/payroll/settings": "Cấu hình lương",
  "/positions": "Chức vụ",
  "/profile": "Hồ sơ của tôi",
  "/projects": "Dự án",
  "/tasks": "Công việc",
};

/** Đoạn cuối dùng chung cho nhiều loại trang. */
const LEAF_LABELS: Record<string, string> = {
  edit: "Sửa",
  board: "Bảng công việc",
};

const useCrumbStore = create<{
  labels: Record<string, string>;
  set: (path: string, label: string) => void;
}>((set) => ({
  labels: {},
  set: (path, label) =>
    set((s) => ({ labels: { ...s.labels, [path]: label } })),
}));

/**
 * Trang có đoạn id gọi hook này khi đã tải xong dữ liệu:
 * useCrumbLabel(`/employees/${id}`, employee?.full_name).
 */
export function useCrumbLabel(path: string, label: string | undefined) {
  const set = useCrumbStore((s) => s.set);
  useEffect(() => {
    if (label) set(path, label);
  }, [path, label, set]);
}

/**
 * Mục menu khớp DÀI NHẤT với đường dẫn hiện tại.
 *
 * Dùng chung cho breadcrumb và cho việc tô sáng menu. So khớp theo tiền tố
 * đơn thuần thì ở /attendance/team cả "Chấm công" (/attendance) lẫn "Công
 * phòng ban" cùng sáng.
 */
export function matchRoot(
  pathname: string,
  roots: string[],
): string | undefined {
  return roots
    .filter((r) => pathname === r || pathname.startsWith(r + "/"))
    .sort((a, b) => b.length - a.length)[0];
}

export function Breadcrumb({ roots }: { roots: string[] }) {
  const pathname = usePathname();
  const dynamic = useCrumbStore((s) => s.labels);

  const segments = pathname.split("/").filter(Boolean);

  // Bắt đầu từ mục menu chứa trang này. /payroll/my là mục "Phiếu lương"
  // riêng — dựng từ gốc thì ra "Kỳ lương › Phiếu lương", kèm một đường dẫn
  // tới trang mà nhân viên thường không có quyền mở.
  const root = matchRoot(pathname, roots);
  const skip = root ? root.split("/").filter(Boolean).length - 1 : 0;

  const crumbs = segments.slice(skip).map((seg, j) => {
    const i = j + skip;
    const href = "/" + segments.slice(0, i + 1).join("/");
    const label = LABELS[href] ?? dynamic[href] ?? LEAF_LABELS[seg] ?? "…";
    return { href, label };
  });

  if (crumbs.length === 0) return null;

  return (
    <nav aria-label="Đường dẫn" className="min-w-0 text-sm">
      <ol className="flex min-w-0 items-center gap-1.5 text-neutral-500">
        {crumbs.map((c, i) => {
          const last = i === crumbs.length - 1;
          return (
            <li key={c.href} className="flex min-w-0 items-center gap-1.5">
              {i > 0 && <span aria-hidden="true">›</span>}
              {last ? (
                <span
                  aria-current="page"
                  className="truncate font-medium text-neutral-900 dark:text-neutral-100"
                >
                  {c.label}
                </span>
              ) : (
                <Link href={c.href} className="truncate hover:underline">
                  {c.label}
                </Link>
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
