// ESLint 9 dùng flat config. eslint-config-next 16 đã export flat config gốc
// qua các subpath, nên KHÔNG cần lớp tương thích FlatCompat nữa — dùng nó sẽ
// gây lỗi "Converting circular structure to JSON".
import nextCoreWebVitals from "eslint-config-next/core-web-vitals";
import nextTypeScript from "eslint-config-next/typescript";

const config = [
  ...nextCoreWebVitals,
  ...nextTypeScript,
  {
    ignores: [".next/**", "node_modules/**", "next-env.d.ts"],
  },
];

export default config;
