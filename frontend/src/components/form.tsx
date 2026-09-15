"use client";

import type { ReactNode } from "react";
import type { FieldError } from "react-hook-form";

const inputClass =
  "w-full rounded border border-neutral-300 px-3 py-2 text-sm outline-none " +
  "focus:border-neutral-900 disabled:bg-neutral-100 disabled:text-neutral-500 " +
  "dark:border-neutral-700 dark:bg-neutral-950 dark:focus:border-neutral-400 " +
  "dark:disabled:bg-neutral-900";

const errorClass = "border-red-400 dark:border-red-700";

type FieldProps = {
  label: string;
  error?: FieldError;
  required?: boolean;
  hint?: string;
  children: ReactNode;
};

/**
 * Bọc một ô nhập: nhãn, thông báo lỗi, ghi chú.
 *
 * Gom vào một chỗ để mọi form trong hệ thống hiển thị lỗi giống nhau — người
 * dùng học một lần là hiểu ở mọi màn hình.
 */
export function Field({ label, error, required, hint, children }: FieldProps) {
  return (
    <div>
      <label className="mb-1 block text-sm font-medium">
        {label}
        {required && <span className="ml-0.5 text-red-600">*</span>}
      </label>
      {children}
      {hint && !error && (
        <p className="mt-1 text-xs text-neutral-500">{hint}</p>
      )}
      {error && (
        <p className="mt-1 text-xs text-red-600 dark:text-red-400">
          {error.message}
        </p>
      )}
    </div>
  );
}

export function inputProps(hasError?: boolean) {
  return { className: hasError ? `${inputClass} ${errorClass}` : inputClass };
}

export function FormError({ message }: { message?: string | null }) {
  if (!message) return null;
  return (
    <div
      role="alert"
      className="rounded border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900 dark:bg-red-950 dark:text-red-300"
    >
      {message}
    </div>
  );
}

export function SubmitButton({
  pending,
  children,
  label = "Đang lưu...",
}: {
  pending: boolean;
  children: ReactNode;
  label?: string;
}) {
  return (
    <button
      type="submit"
      disabled={pending}
      className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white transition hover:bg-neutral-800 disabled:opacity-50 dark:bg-white dark:text-neutral-900 dark:hover:bg-neutral-200"
    >
      {pending ? label : children}
    </button>
  );
}

/** Hộp thoại đơn giản, dùng cho form thêm/sửa phòng ban và chức vụ. */
export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      // Bấm ra ngoài thì đóng. Bấm vào trong thì không — stopPropagation ở
      // thẻ con bên dưới lo việc đó.
      onClick={onClose}
    >
      <div
        className="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-6 shadow-lg dark:border-neutral-800 dark:bg-neutral-900"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{title}</h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Đóng"
            className="text-neutral-400 hover:text-neutral-900 dark:hover:text-neutral-100"
          >
            ✕
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}
