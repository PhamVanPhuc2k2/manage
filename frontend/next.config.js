/** @type {import('next').NextConfig} */
const nextConfig = {
  // Bắt buộc cho Dockerfile production: Next sinh ra .next/standalone
  // chứa sẵn node_modules tối thiểu, image nhỏ hơn hẳn.
  output: "standalone",
  reactStrictMode: true,
};

module.exports = nextConfig;
