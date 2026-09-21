/**
 * Khoảng thời gian tương đối, dạng "5 phút trước".
 *
 * `now` là THAM SỐ chứ không gọi Date.now() bên trong. Hàm này chạy trong
 * lúc render, và đọc đồng hồ ở đó khiến cùng một dữ liệu cho ra hai kết quả
 * khác nhau giữa hai lần render — React coi đó là hàm không thuần khiết và
 * quy tắc react-hooks/purity chặn lại. Nơi gọi truyền vào một mốc ổn định
 * (thường là dataUpdatedAt của truy vấn).
 */
export function relativeTime(iso: string, now: number): string {
  const diff = Math.floor((now - new Date(iso).getTime()) / 1000);

  if (diff < 60) return "vừa xong";
  if (diff < 3600) return `${Math.floor(diff / 60)} phút trước`;
  if (diff < 86400) return `${Math.floor(diff / 3600)} giờ trước`;
  if (diff < 604800) return `${Math.floor(diff / 86400)} ngày trước`;

  return new Date(iso).toLocaleDateString("vi-VN");
}

/** Giờ:phút, dùng cho bong bóng chat. */
export function clockTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("vi-VN", {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Ngày dạng dài, dùng cho vạch ngăn ngày trong khung chat. */
export function dayLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("vi-VN", {
    weekday: "long",
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
  });
}

/** Khoá ngày (YYYY-MM-DD) để so hai tin nhắn có cùng ngày không. */
export function dayKey(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}
