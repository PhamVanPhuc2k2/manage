import { expect, type Page } from "@playwright/test";

/**
 * Tiện ích dùng chung cho bộ E2E.
 *
 * Phần đáng nói nhất ở đây là lấy mã OTP: hệ thống bắt nhập mã sáu chữ số
 * gửi qua email, nên không có cách nào đăng nhập bằng trình duyệt mà không
 * đọc được hộp thư. Bộ này đọc qua MailHog, đúng đường mà các bộ smoke đi.
 */

const MAILHOG = process.env.E2E_MAILHOG ?? "http://localhost:8025";

export const ADMIN_EMAIL = process.env.E2E_ADMIN_EMAIL ?? "admin@abc.vn";
export const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? "";

/** Xoá sạch hộp thư để không nhặt nhầm mã của lần đăng nhập trước. */
export async function clearMailbox(): Promise<void> {
  await fetch(`${MAILHOG}/api/v1/messages`, { method: "DELETE" });
}

/**
 * Đọc mã OTP mới nhất trong MailHog.
 *
 * Đi vòng qua hộp thư thay vì đọc thẳng Redis là có chủ ý: mã phải đi hết
 * api → RabbitMQ → worker → SMTP mới tới được đây, và đó đúng là đoạn hay
 * hỏng nhất. Đọc Redis thì chỉ kiểm chứng được một phép so chuỗi.
 */
export async function readOtp(timeoutMs = 20_000): Promise<string> {
  const deadline = Date.now() + timeoutMs;

  while (Date.now() < deadline) {
    const res = await fetch(`${MAILHOG}/api/v2/messages?limit=1`);
    if (res.ok) {
      const body = await res.text();
      // Trong thân thư chỉ có đúng một dãy 6 chữ số: chính là mã.
      const match = body.match(/\b\d{6}\b/);
      if (match) return match[0];
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error("không nhận được mã OTP qua MailHog trong thời gian chờ");
}

/**
 * Đăng nhập bằng giao diện, đi trọn cả bước nhập mã nếu máy chủ đòi.
 *
 * Không nhét token thẳng vào localStorage cho nhanh: chính màn hình đăng
 * nhập là thứ mọi người dùng chạm vào đầu tiên mỗi ngày, và bỏ qua nó nghĩa
 * là không bao giờ biết nó hỏng.
 */
export async function login(
  page: Page,
  email = ADMIN_EMAIL,
  password = ADMIN_PASSWORD,
): Promise<void> {
  if (!password) {
    throw new Error(
      "thiếu E2E_ADMIN_PASSWORD — đặt biến môi trường trước khi chạy",
    );
  }

  await clearMailbox();
  await page.goto("/login");

  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Mật khẩu").fill(password);
  await page.getByRole("button", { name: "Đăng nhập" }).click();

  // Máy chủ có thể bật hoặc tắt OTP. Hỏi giao diện thay vì đoán: ô nhập mã
  // chỉ hiện khi bước hai được yêu cầu.
  const codeField = page.getByLabel("Mã xác minh");
  const appeared = await codeField
    .waitFor({ state: "visible", timeout: 8_000 })
    .then(() => true)
    .catch(() => false);

  if (appeared) {
    const otp = await readOtp();
    await codeField.fill(otp);
    await page.getByRole("button", { name: /Xác minh|Đăng nhập/ }).click();
  }

  // Đăng nhập xong thì rời khỏi /login. Chờ điều đó thay vì chờ một phần tử
  // cụ thể trên trang chủ: trang chủ đổi bố cục thì phép thử này vẫn đúng.
  await expect(page).not.toHaveURL(/\/login/, { timeout: 20_000 });
}

/**
 * Chuỗi ngẫu nhiên ngắn để dữ liệu mỗi lần chạy không đụng lần trước.
 *
 * Bộ E2E ghi vào database thật và KHÔNG dọn sạch mọi thứ nó tạo. Trùng mã
 * nhân viên hay mã dự án sẽ cho ra lỗi 409 trông y như lỗi sản phẩm.
 */
export function suffix(): string {
  return Date.now().toString(36).slice(-6).toUpperCase();
}
