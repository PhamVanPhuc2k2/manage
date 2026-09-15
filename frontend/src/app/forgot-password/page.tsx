"use client";

import { useState } from "react";
import Link from "next/link";

import { api } from "@/lib/api-client";

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState("");
  const [sent, setSent] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSubmitting(true);

    try {
      await api.post("/auth/forgot-password", { email }, { skipAuth: true });
    } catch {
      // Bỏ qua lỗi CÓ CHỦ Ý.
      //
      // Backend luôn trả 200 dù email có tồn tại hay không, để endpoint này
      // không trở thành công cụ dò danh sách email nhân viên. Giao diện phải
      // giữ đúng tinh thần đó: hiện cùng một thông báo trong mọi trường hợp.
    } finally {
      setSent(true);
      setSubmitting(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-neutral-50 px-4 dark:bg-neutral-950">
      <div className="w-full max-w-sm">
        <h1 className="mb-6 text-center text-xl font-semibold">Quên mật khẩu</h1>

        <div className="rounded-lg border border-neutral-200 bg-white p-6 shadow-sm dark:border-neutral-800 dark:bg-neutral-900">
          {sent ? (
            <div className="space-y-4 text-sm">
              <p>
                Nếu email này tồn tại trong hệ thống, hướng dẫn đặt lại mật khẩu
                đã được gửi tới hộp thư của bạn.
              </p>
              <p className="text-neutral-500">
                Liên kết có hiệu lực trong 30 phút và chỉ dùng được một lần.
              </p>
              <Link href="/login" className="block text-center underline-offset-4 hover:underline">
                Quay lại đăng nhập
              </Link>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-4">
              <div>
                <label htmlFor="email" className="mb-1 block text-sm font-medium">
                  Email
                </label>
                <input
                  id="email"
                  type="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950 dark:focus:border-neutral-400"
                />
              </div>

              <button
                type="submit"
                disabled={submitting}
                className="w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
              >
                {submitting ? "Đang gửi..." : "Gửi hướng dẫn"}
              </button>

              <Link
                href="/login"
                className="block text-center text-sm text-neutral-500 underline-offset-4 hover:underline"
              >
                Quay lại đăng nhập
              </Link>
            </form>
          )}
        </div>
      </div>
    </main>
  );
}
