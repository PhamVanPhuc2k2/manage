import type { Config } from "tailwindcss";

export default {
  content: ["./src/**/*.{js,ts,jsx,tsx,mdx}"],
  // Tối theo class `dark` trên <html>, không theo media query: người dùng
  // chọn được Sáng / Tối / Theo máy. "Theo máy" do THEME_SCRIPT tự đặt class
  // theo prefers-color-scheme — xem src/lib/theme.ts.
  darkMode: "selector",
  theme: {
    extend: {},
  },
  plugins: [],
} satisfies Config;
