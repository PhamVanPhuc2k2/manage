"use client";

import { useCallback, useEffect, useState } from "react";

/**
 * Giao diện sáng / tối / theo máy.
 *
 * Tailwind chạy ở chế độ `darkMode: "selector"`: giao diện tối bật khi thẻ
 * <html> có class `dark`. Class đó do đoạn THEME_SCRIPT đặt trong <head>,
 * chạy TRƯỚC khi trang được vẽ — đặt trong một effect của React thì người
 * chọn giao diện tối sẽ thấy trang nháy trắng mỗi lần tải lại.
 */

export type Theme = "light" | "dark" | "system";

const KEY = "theme";

/**
 * Chạy nội tuyến trong <head>, trước mọi thứ khác. Giữ thật ngắn, không phụ
 * thuộc gì, và nuốt mọi lỗi: localStorage có thể bị chặn (chế độ ẩn danh,
 * trình duyệt chặn dữ liệu trang) — khi đó rơi về theo máy.
 */
export const THEME_SCRIPT = `(function(){try{var t=localStorage.getItem("${KEY}");var d=t==="dark"||(t!=="light"&&matchMedia("(prefers-color-scheme: dark)").matches);var e=document.documentElement;e.classList.toggle("dark",d);e.style.colorScheme=d?"dark":"light"}catch(_){}})()`;

function readTheme(): Theme {
  try {
    const t = localStorage.getItem(KEY);
    return t === "light" || t === "dark" ? t : "system";
  } catch {
    return "system";
  }
}

function apply(theme: Theme) {
  const dark =
    theme === "dark" ||
    (theme === "system" &&
      window.matchMedia("(prefers-color-scheme: dark)").matches);
  const root = document.documentElement;
  root.classList.toggle("dark", dark);
  root.style.colorScheme = dark ? "dark" : "light";
}

export function useTheme() {
  // Khởi tạo "system" rồi đọc lại sau khi gắn vào trang: máy chủ không biết
  // lựa chọn của người dùng, đọc localStorage ngay lúc render sẽ lệch với
  // HTML máy chủ gửi về.
  const [theme, setThemeState] = useState<Theme>("system");

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- đồng bộ từ localStorage, chỉ có sau khi gắn vào trang
    setThemeState(readTheme());
  }, []);

  // Theo máy thì phải theo cả khi người dùng đổi giao diện hệ điều hành
  // lúc trang đang mở (ví dụ macOS tự chuyển tối lúc hoàng hôn).
  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => apply("system");
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [theme]);

  const setTheme = useCallback((t: Theme) => {
    try {
      if (t === "system") localStorage.removeItem(KEY);
      else localStorage.setItem(KEY, t);
    } catch {
      // không lưu được thì vẫn đổi cho phiên này
    }
    apply(t);
    setThemeState(t);
  }, []);

  return { theme, setTheme };
}
