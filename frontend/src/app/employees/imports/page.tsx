"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  IMPORT_STATUS_LABEL,
  columnTitle,
  downloadTemplate,
  useImports,
  useStartImport,
} from "@/features/employees/imports";
import { ApiError } from "@/lib/api-client";

const formatTime = (s: string) => new Date(s).toLocaleString("vi-VN");

export default function EmployeeImportPage() {
  const router = useRouter();
  const { data, isPending } = useImports();
  const start = useStartImport();

  const [file, setFile] = useState<File | null>(null);
  const [createAccounts, setCreateAccounts] = useState(false);

  const template = data?.template;
  const items = data?.items ?? [];

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    start.mutate(
      { file, createAccounts },
      // Sang ngay trang kết quả: ở đó hỏi lại định kỳ tới khi worker xong.
      { onSuccess: (imp) => router.push(`/employees/imports/${imp.id}`) },
    );
  };

  return (
    <AppShell>
      <div className="mb-6">
        <Link
          href="/employees"
          className="text-sm text-neutral-500 hover:underline"
        >
          ← Nhân viên
        </Link>
        <h1 className="mt-2 text-xl font-semibold">Nhập nhân viên từ tệp</h1>
      </div>

      <div className="grid gap-6 lg:grid-cols-[1fr_20rem]">
        <form
          onSubmit={submit}
          className="space-y-4 rounded border border-neutral-200 p-5 dark:border-neutral-800"
        >
          <FormError
            message={
              start.error
                ? start.error instanceof ApiError
                  ? start.error.message
                  : "Không tải được tệp lên"
                : null
            }
          />

          <label className="block">
            <span className="mb-1 block text-sm font-medium">
              Tệp CSV hoặc Excel (.xlsx)
            </span>
            <input
              type="file"
              accept=".csv,.xlsx"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
              className="block w-full text-sm file:mr-3 file:rounded file:border-0 file:bg-neutral-100 file:px-3 file:py-2 file:text-sm dark:file:bg-neutral-800"
            />
          </label>

          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              checked={createAccounts}
              onChange={(e) => setCreateAccounts(e.target.checked)}
              className="mt-0.5"
            />
            <span>
              Tạo luôn tài khoản đăng nhập
              <span className="block text-xs text-neutral-500">
                Mỗi người nhận một email kèm mật khẩu tạm và phải đổi ở lần đăng
                nhập đầu. Vai trò mặc định là nhân viên.
              </span>
            </span>
          </label>

          <button
            type="submit"
            disabled={!file || start.isPending}
            className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-40 dark:bg-white dark:text-neutral-900"
          >
            {start.isPending ? "Đang tải lên..." : "Nhập"}
          </button>
        </form>

        <aside className="space-y-3 rounded border border-neutral-200 p-5 text-sm dark:border-neutral-800">
          <div className="font-medium">Cách chuẩn bị tệp</div>
          <button
            type="button"
            disabled={!template}
            onClick={() => template && downloadTemplate(template)}
            className="rounded border border-neutral-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-neutral-700"
          >
            Tải tệp mẫu
          </button>
          {template && (
            <ul className="list-disc space-y-1 pl-4 text-neutral-600 dark:text-neutral-400">
              <li>
                Bắt buộc:{" "}
                {template.required_columns.map(columnTitle).join(", ")}.
              </li>
              <li>Tối đa {template.max_rows} dòng mỗi tệp.</li>
              <li>Ngày viết dạng 15/01/2024 (ngày trước, tháng sau).</li>
              <li>
                Phòng ban và chức vụ ghi mã hoặc tên, có dấu hay không đều được.
              </li>
              <li>
                Mã cấp trên phải là người đã có, hoặc nằm ở dòng phía trên trong
                cùng tệp.
              </li>
              <li>
                Lưu từ Excel thì chọn <b>.xlsx</b> hoặc <b>CSV UTF-8</b> — CSV
                thường làm hỏng tiếng Việt.
              </li>
            </ul>
          )}
        </aside>
      </div>

      <h2 className="mb-3 mt-8 font-medium">Các lần nhập gần đây</h2>
      <div className="overflow-x-auto rounded border border-neutral-200 dark:border-neutral-800">
        <table className="w-full text-sm">
          <thead className="bg-neutral-50 text-left dark:bg-neutral-900">
            <tr>
              <th className="px-4 py-2 font-medium">Tệp</th>
              <th className="px-4 py-2 font-medium">Người nhập</th>
              <th className="px-4 py-2 font-medium">Lúc</th>
              <th className="px-4 py-2 font-medium">Kết quả</th>
              <th className="px-4 py-2 font-medium">Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {isPending && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-6 text-center text-neutral-500"
                >
                  Đang tải...
                </td>
              </tr>
            )}
            {!isPending && items.length === 0 && (
              <tr>
                <td
                  colSpan={5}
                  className="px-4 py-6 text-center text-neutral-500"
                >
                  Chưa nhập lần nào
                </td>
              </tr>
            )}
            {items.map((i) => (
              <tr
                key={i.id}
                className="border-t border-neutral-200 dark:border-neutral-800"
              >
                <td className="px-4 py-2">
                  <Link
                    href={`/employees/imports/${i.id}`}
                    className="font-medium underline-offset-4 hover:underline"
                  >
                    {i.file_name}
                  </Link>
                </td>
                <td className="px-4 py-2">{i.creator_name || "—"}</td>
                <td className="px-4 py-2 text-neutral-500">
                  {formatTime(i.created_at)}
                </td>
                <td className="px-4 py-2">
                  {i.succeeded}/{i.total_rows} thành công
                  {i.failed > 0 && (
                    <span className="text-red-600 dark:text-red-400">
                      {" "}
                      · {i.failed} lỗi
                    </span>
                  )}
                </td>
                <td className="px-4 py-2">{IMPORT_STATUS_LABEL[i.status]}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </AppShell>
  );
}
