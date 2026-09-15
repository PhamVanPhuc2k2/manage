"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import Link from "next/link";

import { useAuth } from "@/lib/auth/useAuth";
import { ApiError } from "@/lib/api-client";

const inputClass =
  "w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none focus:border-neutral-900 dark:border-neutral-700 dark:bg-neutral-950 dark:focus:border-neutral-400";

const buttonClass =
  "w-full rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white transition hover:bg-neutral-800 disabled:opacity-50 dark:bg-white dark:text-neutral-900 dark:hover:bg-neutral-200";

/** Thử thách OTP đang chờ xác minh. */
type Challenge = {
  challengeId: string;
  maskedEmail: string;
};

export default function LoginPage() {
  const { login, verifyOtp, resendOtp } = useAuth();
  const router = useRouter();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [code, setCode] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [resendIn, setResendIn] = useState(0);

  // Đếm ngược tới lúc được bấm gửi lại. Nút bị khoá trong lúc đếm vì máy chủ
  // cũng từ chối — khoá ở đây chỉ để người dùng khỏi bấm vào một nút chắc
  // chắn báo lỗi.
  useEffect(() => {
    if (resendIn <= 0) return;
    const t = setTimeout(() => setResendIn((n) => n - 1), 1000);
    return () => clearTimeout(t);
  }, [resendIn]);

  function describe(err: unknown): string {
    return err instanceof ApiError ? err.message : "Không kết nối được máy chủ";
  }

  async function handlePassword(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setNotice(null);
    setSubmitting(true);

    try {
      const res = await login(email, password);

      if (res.otpRequired) {
        setChallenge({
          challengeId: res.challengeId,
          maskedEmail: res.maskedEmail,
        });
        setResendIn(res.resendAfterSeconds);
        // Xoá mật khẩu khỏi state ngay khi không cần nữa.
        setPassword("");
        return;
      }
      router.push(res.mustChangePassword ? "/change-password" : "/employees");
    } catch (err) {
      // Backend cố ý trả cùng một thông báo cho "email không tồn tại" và
      // "sai mật khẩu", nên cứ hiển thị nguyên văn.
      setError(describe(err));
    } finally {
      setSubmitting(false);
    }
  }

  async function handleCode(e: React.FormEvent) {
    e.preventDefault();
    if (!challenge) return;

    setError(null);
    setNotice(null);
    setSubmitting(true);

    try {
      const { mustChangePassword } = await verifyOtp(challenge.challengeId, code);
      router.push(mustChangePassword ? "/change-password" : "/employees");
    } catch (err) {
      setError(describe(err));
      setCode("");
    } finally {
      setSubmitting(false);
    }
  }

  async function handleResend() {
    if (!challenge || resendIn > 0) return;

    setError(null);
    try {
      await resendOtp(challenge.challengeId);
      setNotice("Đã gửi lại mã. Kiểm tra hộp thư của bạn.");
      setResendIn(60);
    } catch (err) {
      setError(describe(err));
    }
  }

  function backToPassword() {
    setChallenge(null);
    setCode("");
    setError(null);
    setNotice(null);
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-neutral-50 px-4 dark:bg-neutral-950">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <h1 className="text-2xl font-semibold">Quản lý công ty</h1>
          <p className="mt-1 text-sm text-neutral-500">
            {challenge ? "Nhập mã xác minh" : "Đăng nhập để tiếp tục"}
          </p>
        </div>

        <form
          onSubmit={challenge ? handleCode : handlePassword}
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
          {notice && (
            <div className="rounded border border-neutral-200 bg-neutral-50 px-3 py-2 text-sm text-neutral-600 dark:border-neutral-800 dark:bg-neutral-950 dark:text-neutral-300">
              {notice}
            </div>
          )}

          {!challenge && (
            <>
              <div>
                <label htmlFor="email" className="mb-1 block text-sm font-medium">
                  Email
                </label>
                <input
                  id="email"
                  type="email"
                  required
                  autoComplete="username"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className={inputClass}
                />
              </div>

              <div>
                <label
                  htmlFor="password"
                  className="mb-1 block text-sm font-medium"
                >
                  Mật khẩu
                </label>
                <input
                  id="password"
                  type="password"
                  required
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className={inputClass}
                />
              </div>

              <button type="submit" disabled={submitting} className={buttonClass}>
                {submitting ? "Đang đăng nhập..." : "Đăng nhập"}
              </button>

              <div className="text-center">
                <Link
                  href="/forgot-password"
                  className="text-sm text-neutral-500 underline-offset-4 hover:underline"
                >
                  Quên mật khẩu?
                </Link>
              </div>
            </>
          )}

          {challenge && (
            <>
              <p className="text-sm text-neutral-600 dark:text-neutral-400">
                Mã gồm 6 chữ số đã được gửi tới{" "}
                <span className="font-medium">{challenge.maskedEmail}</span>. Mã
                có hiệu lực trong 5 phút.
              </p>

              <div>
                <label htmlFor="code" className="mb-1 block text-sm font-medium">
                  Mã xác minh
                </label>
                <input
                  id="code"
                  // inputMode numeric để điện thoại mở bàn phím số, nhưng type
                  // vẫn là text: type="number" cho phép nhập "e", "+", "-" và
                  // cắt mất số 0 đứng đầu.
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  autoFocus
                  required
                  maxLength={6}
                  value={code}
                  onChange={(e) =>
                    setCode(e.target.value.replace(/\D/g, "").slice(0, 6))
                  }
                  className={`${inputClass} text-center text-lg tracking-[0.5em]`}
                />
              </div>

              <button
                type="submit"
                disabled={submitting || code.length < 6}
                className={buttonClass}
              >
                {submitting ? "Đang xác minh..." : "Xác minh"}
              </button>

              <div className="flex items-center justify-between text-sm">
                <button
                  type="button"
                  onClick={backToPassword}
                  className="text-neutral-500 underline-offset-4 hover:underline"
                >
                  Quay lại
                </button>
                <button
                  type="button"
                  onClick={handleResend}
                  disabled={resendIn > 0}
                  className="text-neutral-500 underline-offset-4 hover:underline disabled:no-underline disabled:opacity-60"
                >
                  {resendIn > 0 ? `Gửi lại sau ${resendIn}s` : "Gửi lại mã"}
                </button>
              </div>
            </>
          )}
        </form>
      </div>
    </main>
  );
}
