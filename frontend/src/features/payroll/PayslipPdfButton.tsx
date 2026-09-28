"use client";

import { useState } from "react";

import { ApiError, downloadFile } from "@/lib/api-client";

/**
 * Nút tải phiếu lương PDF (dựng ở máy chủ).
 *
 * Thay cho đường dẫn <a href> trỏ thẳng vào API trước đây: tab mới mở từ
 * đường dẫn đó không mang access token (token nằm trong bộ nhớ, không phải
 * cookie), nên máy chủ luôn trả 401 và người dùng thấy một trang JSON lỗi.
 */
export function PayslipPdfButton({
  payslipId,
  label = "Tải PDF",
  className,
}: {
  payslipId: string;
  label?: string;
  className?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const download = async () => {
    setBusy(true);
    setError(null);
    try {
      await downloadFile(
        `/payroll/payslips/${payslipId}/pdf`,
        "phieu-luong.pdf",
      );
    } catch (err) {
      setError(
        err instanceof ApiError ? err.message : "Không tải được phiếu lương",
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="inline-flex flex-col items-start gap-1">
      <button
        onClick={() => void download()}
        disabled={busy}
        className={className}
      >
        {busy ? "Đang tải..." : label}
      </button>
      {error && (
        <span className="text-xs text-red-600 dark:text-red-400">{error}</span>
      )}
    </span>
  );
}
