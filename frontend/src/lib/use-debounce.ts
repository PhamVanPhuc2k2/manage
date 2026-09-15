"use client";

import { useEffect, useState } from "react";

/**
 * Trả về giá trị đã trễ lại, chỉ cập nhật sau khi ngừng thay đổi `delay` ms.
 *
 * Dùng cho ô tìm kiếm: giá trị này đi vào queryKey của react-query, nên mỗi
 * ký tự gõ thêm KHÔNG tạo một khoá cache mới và một request mới — chỉ khi
 * người dùng ngừng gõ mới có một request duy nhất.
 */
export function useDebounce<T>(value: T, delay = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delay);
    return () => clearTimeout(t);
  }, [value, delay]);

  return debounced;
}
