import { defineConfig, devices } from "@playwright/test";

/**
 * Cấu hình Playwright cho 5 luồng quan trọng nhất.
 *
 * KHÁC GÌ SO VỚI 6 BỘ SMOKE
 *
 * Các bộ smoke gọi thẳng API. Chúng chứng minh máy chủ đúng, nhưng không
 * chứng minh được người dùng làm xong việc: một nút không gắn handler, một
 * form gửi sai tên trường, một trang trắng vì lỗi hydrate — API vẫn xanh
 * trong khi không ai dùng được hệ thống.
 *
 * Bộ này chạy trình duyệt thật và chỉ khẳng định những gì NGƯỜI DÙNG thấy.
 *
 * CẦN STACK ĐANG CHẠY
 *
 * Không dùng `webServer` của Playwright để tự dựng Next.js: frontend cần api,
 * database, Redis và RabbitMQ đứng sau. Dựng nửa hệ thống rồi kiểm thử nó chỉ
 * cho ra những lỗi không có thật.
 *
 *   docker compose up -d
 *   cd frontend && pnpm e2e
 */
export default defineConfig({
  testDir: "./e2e",

  // Một worker: các luồng dùng chung một tài khoản quản trị và cùng chạm vào
  // dữ liệu của nhau. Chạy song song thì hỏng theo kiểu khó lần nhất — lúc
  // được lúc không.
  workers: 1,
  fullyParallel: false,

  // Không thử lại ở máy: một phép thử chập chờn cần được nhìn thấy, không
  // phải được giấu đi. Trên CI thì thử lại một lần, vì runner chia sẻ CPU và
  // đôi khi chậm thật.
  retries: process.env.CI ? 1 : 0,

  timeout: 60_000,
  expect: { timeout: 15_000 },

  reporter: process.env.CI ? [["github"], ["list"]] : [["list"]],

  use: {
    // Đi qua nginx chứ không gọi thẳng cổng 3000 của Next.js.
    //
    // Đây là đường người dùng thật đi, và nó là đường duy nhất kiểm được
    // phần định tuyến của nginx: /api, /ws và phần còn lại sang frontend.
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8088",

    // Chỉ giữ dấu vết khi hỏng. Giữ mọi lần chạy sẽ ngốn hàng trăm MB và
    // rồi không ai mở chúng nữa.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "off",

    locale: "vi-VN",
    timezoneId: "Asia/Ho_Chi_Minh",
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});
