"use client";

import { Suspense, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";

import { api, ApiError } from "@/lib/api-client";

function ResetForm() {
  const params = useSearchParams();
  const router = useRouter();
  const token = params.get("token") ?? "";

  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (password !== confirm) {
      setError("Hai lần nhập mật khẩu không khớp");
      return;
    }

    setSubmitting(true);
    try {
      await api.post(
        "/auth/reset-password",
        { token, new_password: password },
        { skipAuth: true },
      );
      router.push("/login?reset=1");
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : "Không đặt lại được mật khẩu",
      );
    } finally {
      setSubmitting(false);
    }
  }

  if (!token) {
    return (
      <div className="space-y-4 text-sm">
        <p className="text-red-700 dark:text-red-300">
          Liên kết không hợp lệ — thiếu mã xác nhận.
        </p>
        <Link href="/forgot-password" className="underline-offset-4 hover:underline">
          Yêu cầu liên kết mới
        </Link>
      </div>
    );
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      {error && (
        <div
          role="alert"
          className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
        >
          {error}
        </div>
      )}

      <div>
        <label htmlFor="pw" className="mb-1 block text-sm font-medium">
          Mật khẩu mới
        </label>
        <input
          id="pw"
          type="password"
          required
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          className="w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950"
        />
      </div>

      <div>
        <label htmlFor="pw2" className="mb-1 block text-sm font-medium">
          Nhập lại mật khẩu mới
        </label>
        <input
          id="pw2"
          type="password"
          required
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          className="w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950"
        />
      </div>

      <button
        type="submit"
        disabled={submitting}
        className="w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
      >
        {submitting ? "Đang xử lý..." : "Đặt lại mật khẩu"}
      </button>
    </form>
  );
}

export default function ResetPasswordPage() {
  return (
    <main className="flex min-h-screen items-center justify-center bg-neutral-50 px-4 dark:bg-neutral-950">
      <div className="w-full max-w-sm">
        <h1 className="mb-1 text-center text-xl font-semibold">
          Đặt lại mật khẩu
        </h1>
        <p className="mb-6 text-center text-sm text-neutral-500">
          Mật khẩu cần ít nhất 10 ký tự, có cả chữ và số
        </p>

        <div className="rounded-lg border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
          {/* useSearchParams bắt buộc phải nằm trong Suspense ở App Router. */}
          <Suspense fallback={<p className="text-sm text-neutral-500">Đang tải...</p>}>
            <ResetForm />
          </Suspense>
        </div>
      </div>
    </main>
  );
}
