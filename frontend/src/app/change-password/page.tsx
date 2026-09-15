"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";

import { useChangePassword } from "@/features/auth/queries";
import { ApiError } from "@/lib/api-client";
import { useAuth } from "@/lib/auth/AuthProvider";

export default function ChangePasswordPage() {
  const { user, loading } = useAuth();
  const router = useRouter();

  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  // useMutation lo giúp trạng thái đang gửi và lỗi từ server, nên component
  // chỉ còn phải giữ lỗi kiểm tra tại chỗ.
  const changePassword = useChangePassword();

  const error =
    localError ??
    (changePassword.error instanceof ApiError
      ? changePassword.error.message
      : changePassword.error
        ? "Không đổi được mật khẩu"
        : null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setLocalError(null);

    // Kiểm tra trùng khớp ở client chỉ để phản hồi nhanh — backend vẫn
    // kiểm tra độ mạnh mật khẩu đầy đủ.
    if (newPassword !== confirm) {
      setLocalError("Hai lần nhập mật khẩu mới không khớp");
      return;
    }

    try {
      await changePassword.mutateAsync({ oldPassword, newPassword });
      setDone(true);
      setTimeout(() => router.push("/employees"), 1200);
    } catch {
      // Lỗi đã nằm trong changePassword.error, không cần xử lý thêm.
    }
  }

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center text-sm text-neutral-500">
        Đang tải...
      </div>
    );
  }
  if (!user) {
    router.replace("/login");
    return null;
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-neutral-50 px-4 dark:bg-neutral-950">
      <div className="w-full max-w-sm">
        <h1 className="mb-1 text-center text-xl font-semibold">Đổi mật khẩu</h1>
        <p className="mb-6 text-center text-sm text-neutral-500">
          Mật khẩu cần ít nhất 10 ký tự, có cả chữ và số
        </p>

        <form
          onSubmit={handleSubmit}
          className="space-y-4 rounded-lg border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900"
        >
          {error && (
            <div
              role="alert"
              className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
            >
              {error}
            </div>
          )}
          {done && (
            <div className="rounded border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-700 dark:border-green-900 dark:bg-green-950 dark:text-green-300">
              Đã đổi mật khẩu. Đang chuyển tiếp...
            </div>
          )}

          {[
            { id: "old", label: "Mật khẩu hiện tại", value: oldPassword, set: setOldPassword, ac: "current-password" },
            { id: "new", label: "Mật khẩu mới", value: newPassword, set: setNewPassword, ac: "new-password" },
            { id: "confirm", label: "Nhập lại mật khẩu mới", value: confirm, set: setConfirm, ac: "new-password" },
          ].map((f) => (
            <div key={f.id}>
              <label htmlFor={f.id} className="mb-1 block text-sm font-medium">
                {f.label}
              </label>
              <input
                id={f.id}
                type="password"
                required
                autoComplete={f.ac}
                value={f.value}
                onChange={(e) => f.set(e.target.value)}
                className="w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950 dark:focus:border-neutral-400"
              />
            </div>
          ))}

          <button
            type="submit"
            disabled={changePassword.isPending || done}
            className="w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white transition hover:bg-neutral-800 disabled:opacity-50 dark:bg-white dark:text-neutral-900"
          >
            {changePassword.isPending ? "Đang xử lý..." : "Đổi mật khẩu"}
          </button>
        </form>
      </div>
    </main>
  );
}
