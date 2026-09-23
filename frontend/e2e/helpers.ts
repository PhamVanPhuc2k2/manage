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

  const emailField = page.getByLabel("Email");
  const codeField = page.getByLabel("Mã xác minh");
  // Thông báo khi nginx chặn vì gọi quá dày — xem gatewayMessage().
  const tooFast = page.getByText("thao tác quá nhanh");

  // ĐUA bốn kết cục có thể xảy ra thay vì chờ một mốc cố định rồi đoán.
  //
  // Bản trước chờ ô nhập mã tối đa 8 giây, không thấy thì coi như máy
  // chủ tắt OTP — nên mọi trục trặc khác đều hiện ra dưới dạng "vẫn đang
  // ở /login", một câu không nói gì về nguyên nhân.
  // KHÔNG đua với "bản báo lỗi hiện ra".
  //
  // Bản báo không tự biến mất, nên ở lần thử LẠI nó đã sẵn trên màn
  // hình và phép đua thắng ngay lập tức — bộ thử kết luận "vẫn bị chặn"
  // dù lần thử đó đã qua. Hỏi theo thứ tự: đi tiếp được chưa, rồi mới
  // hỏi vì sao chưa.
  const submit = async (): Promise<"otp" | "vao-thang" | "bi-chan"> => {
    await clearMailbox();
    await page.getByRole("button", { name: "Đăng nhập" }).click();

    const moved = await Promise.race([
      codeField
        .waitFor({ state: "visible", timeout: 20_000 })
        .then(() => "otp" as const),
      page
        .waitForURL((u) => !u.pathname.startsWith("/login"), { timeout: 20_000 })
        .then(() => "vao-thang" as const),
    ]).catch(() => null);

    if (moved) return moved;
    return (await tooFast.isVisible().catch(() => false))
      ? "bi-chan"
      : "vao-thang"; // để phép thử phía sau báo đúng chỗ hỏng
  };

  await page.goto("/login");
  await emailField.fill(email);
  await page.getByLabel("Mật khẩu").fill(password);

  let outcome = await submit();

  // nginx giới hạn 30 lượt/phút cho nhóm endpoint đăng nhập, và mỗi lần
  // đăng nhập ở đây tốn HAI lượt (login + verify-otp). Cả bộ E2E chạy
  // liền hai lần là chạm trần.
  //
  // Chờ rồi thử lại, KHÔNG nới giới hạn cho dễ test: đó là giới hạn đúng
  // của sản phẩm, và nới ra là bỏ đi lớp chặn dò mật khẩu quy mô lớn.
  for (let i = 0; outcome === "bi-chan" && i < 3; i++) {
    await page.waitForTimeout(62_000);
    outcome = await submit();
  }
  if (outcome === "bi-chan") {
    throw new Error(
      "nginx vẫn chặn sau ba lần chờ — có tiến trình khác đang đăng nhập liên tục?",
    );
  }

  if (outcome === "otp") {
    const otp = await readOtp();
    await codeField.fill(otp);

    // Bước xác minh cũng nằm trong cùng nhóm giới hạn của nginx, nên nó
    // bị chặn riêng được dù bước đăng nhập vừa trôi qua. Mã còn hiệu
    // lực 5 phút nên chỉ cần bấm lại, không phải xin mã mới.
    const verify = async (): Promise<"vao-thang" | "bi-chan"> => {
      await page.getByRole("button", { name: /Xác minh|Đăng nhập/ }).click();

      const ok = await page
        .waitForURL((u) => !u.pathname.startsWith("/login"), { timeout: 20_000 })
        .then(() => true)
        .catch(() => false);

      if (ok) return "vao-thang";
      return (await tooFast.isVisible().catch(() => false))
        ? "bi-chan"
        : "vao-thang";
    };

    let step = await verify();
    for (let i = 0; step === "bi-chan" && i < 3; i++) {
      await page.waitForTimeout(62_000);
      step = await verify();
    }
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

/* ------------------------------------------------------------------ *
 * Dựng dữ liệu qua API
 *
 * Một vài luồng cần HAI người dùng thật — gọi điện là rõ nhất: không có
 * người thứ hai thì không có gì để kiểm. Dựng người đó qua giao diện sẽ
 * tốn cả phút và kiểm lại những màn hình đã có phép thử riêng, nên phần
 * chuẩn bị đi đường API còn phần được kiểm thì đi trình duyệt.
 * ------------------------------------------------------------------ */

const API = process.env.E2E_API_URL ?? "http://localhost:8088/api/v1";

type Envelope<T> = { data?: T; error?: { message?: string } };

async function call<T>(
  path: string,
  init: RequestInit & { token?: string } = {},
): Promise<T> {
  const { token, ...rest } = init;
  const res = await fetch(`${API}${path}`, {
    ...rest,
    headers: {
      "Content-Type": "application/json",
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(rest.headers ?? {}),
    },
  });

  const body = (await res.json().catch(() => ({}))) as Envelope<T>;
  if (!res.ok) {
    throw new Error(
      `${init.method ?? "GET"} ${path} → ${res.status}: ${
        body.error?.message ?? "không rõ lỗi"
      }`,
    );
  }
  return body.data as T;
}

/** Đăng nhập qua API, đi trọn cả bước OTP. Trả về access token. */
export async function apiLogin(
  email: string,
  password: string,
): Promise<string> {
  await clearMailbox();

  const first = await call<{ access_token?: string; challenge_id?: string }>(
    "/auth/login",
    { method: "POST", body: JSON.stringify({ email, password }) },
  );
  if (first.access_token) return first.access_token;

  const otp = await readOtp();
  const second = await call<{ access_token: string }>("/auth/verify-otp", {
    method: "POST",
    body: JSON.stringify({ challenge_id: first.challenge_id, code: otp }),
  });
  return second.access_token;
}

export type Colleague = {
  employeeId: string;
  email: string;
  password: string;
  fullName: string;
};

/**
 * Dựng một đồng nghiệp CÓ TÀI KHOẢN dùng được ngay.
 *
 * Ba bước, thiếu bước nào cũng hỏng theo kiểu khó đoán:
 *   1. tạo nhân viên,
 *   2. cấp tài khoản — trả về mật khẩu tạm,
 *   3. đổi mật khẩu tạm, vì tài khoản mới bị buộc đổi trước khi dùng
 *      được bất kỳ API nào khác.
 *
 * Và cấp vai trò `employee`: không có nó thì tài khoản đăng nhập được
 * nhưng không có quyền nào, và phép thử hỏng ở một màn hình trắng.
 */
export async function createColleague(
  adminToken: string,
  code: string,
  fullName: string,
): Promise<Colleague> {
  const email = `${code.toLowerCase()}@test.local`;
  const password = `E2ePeer#${code}`;

  const [dept] = await call<{ id: string }[]>("/departments", {
    token: adminToken,
  });
  const [pos] = await call<{ id: string }[]>("/positions", {
    token: adminToken,
  });

  const emp = await call<{ id: string }>("/employees", {
    method: "POST",
    token: adminToken,
    body: JSON.stringify({
      employee_code: code,
      full_name: fullName,
      email,
      department_id: dept?.id,
      position_id: pos?.id,
      work_mode: "onsite",
      status: "official",
      joined_at: "2026-01-05",
    }),
  });

  const account = await call<{ temp_password: string }>(
    `/employees/${emp.id}/account`,
    { method: "POST", token: adminToken },
  );

  await call(`/employees/${emp.id}/roles`, {
    method: "PUT",
    token: adminToken,
    body: JSON.stringify({ roles: ["employee"] }),
  });

  const tempToken = await apiLogin(email, account.temp_password);
  await call("/auth/change-password", {
    method: "POST",
    token: tempToken,
    body: JSON.stringify({
      old_password: account.temp_password,
      new_password: password,
    }),
  });

  return { employeeId: emp.id, email, password, fullName };
}

/** Mở (hoặc lấy lại) hội thoại 1-1 với một người. */
export async function openDirectConversation(
  token: string,
  peerId: string,
): Promise<string> {
  const conv = await call<{ id: string }>("/chat/conversations", {
    method: "POST",
    token,
    body: JSON.stringify({ kind: "direct", peer_id: peerId }),
  });
  return conv.id;
}

export type CallDetail = {
  id: string;
  status: string;
  participants?: {
    employee_id: string;
    had_audio: boolean;
    had_video: boolean;
    had_screen: boolean;
  }[];
};

/** Cuộc gọi đang diễn ra của một hội thoại, hoặc null. */
export async function liveCall(
  token: string,
  conversationId: string,
): Promise<CallDetail | null> {
  return (await call<CallDetail | null>(`/calls/live/${conversationId}`, { token })) ?? null;
}

export async function callDetail(
  token: string,
  callId: string,
): Promise<CallDetail> {
  return call<CallDetail>(`/calls/${callId}`, { token });
}
