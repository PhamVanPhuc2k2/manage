// Trang kiểm chứng Phase 0.
// Render phía server, gọi thẳng api trong mạng Docker.

async function fetchPing() {
  // Gọi từ phía server của Next.js nên dùng tên service trong mạng Docker,
  // không dùng localhost — localhost ở đây là chính container frontend.
  const res = await fetch("http://api:8080/api/v1/ping", { cache: "no-store" });
  if (!res.ok) throw new Error(`API trả về ${res.status}`);
  return res.json();
}

export default async function Home() {
  let result: unknown = null;
  let error: string | null = null;

  try {
    result = await fetchPing();
  } catch (e) {
    error = e instanceof Error ? e.message : "Lỗi không xác định";
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="text-2xl font-semibold">Manage — Phase 0</h1>
      <p className="mt-2 text-sm text-neutral-500">
        Trang này gọi api, api đọc PostgreSQL và đẩy một job sang worker. Thấy dữ
        liệu bên dưới nghĩa là bộ khung đã thông.
      </p>

      {error ? (
        <pre className="mt-6 overflow-x-auto rounded bg-red-50 p-4 text-sm text-red-700">
          {error}
        </pre>
      ) : (
        <pre className="mt-6 overflow-x-auto rounded bg-neutral-900 p-4 text-sm text-neutral-100">
          {JSON.stringify(result, null, 2)}
        </pre>
      )}

      <p className="mt-6 text-xs text-neutral-400">
        Kiểm tra log worker để xác nhận job đã được xử lý:{" "}
        <code className="rounded bg-neutral-100 px-1 py-0.5 dark:bg-neutral-800">
          make logs s=worker
        </code>
      </p>
    </main>
  );
}
