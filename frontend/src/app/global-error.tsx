"use client";

/**
 * Trang lỗi toàn cục.
 *
 * Next.js dựng sẵn một trang `_global-error` mặc định, nhưng nó render NGOÀI
 * cây provider của root layout. Khi layout có context provider (ở đây là
 * AuthProvider), bản mặc định sẽ lỗi lúc prerender:
 *
 *     TypeError: Cannot read properties of null (reading 'useContext')
 *
 * Khai báo trang này để thay thế bản mặc định. Nó tự render cả thẻ <html>
 * và <body> vì khi lỗi toàn cục xảy ra, root layout đã không còn dùng được.
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  return (
    <html lang="vi">
      <body className="bg-white text-neutral-900">
        <main className="flex min-h-screen items-center justify-center px-4">
          <div className="max-w-md text-center">
            <h1 className="text-xl font-semibold">Đã có lỗi xảy ra</h1>
            <p className="mt-2 text-sm text-neutral-500">
              Hệ thống gặp sự cố ngoài dự kiến. Vui lòng thử lại.
            </p>

            {/* digest là mã lỗi phía máy chủ — người dùng báo lại mã này thì
                bộ phận kỹ thuật tra được đúng log. Không hiện chi tiết lỗi. */}
            {error.digest && (
              <p className="mt-2 font-mono text-xs text-neutral-400">
                Mã lỗi: {error.digest}
              </p>
            )}

            <button
              onClick={reset}
              className="mt-6 rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white"
            >
              Thử lại
            </button>
          </div>
        </main>
      </body>
    </html>
  );
}
