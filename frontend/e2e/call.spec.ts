import { expect, test, type Browser, type Page } from "@playwright/test";

import {
  ADMIN_EMAIL,
  ADMIN_PASSWORD,
  apiLogin,
  callDetail,
  createColleague,
  liveCall,
  login,
  openDirectConversation,
  suffix,
  type Colleague,
} from "./helpers";

/**
 * Gọi thoại và gọi video — Phase 7.
 *
 * TẠI SAO PHẢI LÀ TRÌNH DUYỆT THẬT
 *
 * Bộ smoke-call.sh đã kiểm 34 điểm của phần máy chủ, và cmd/callsignal đã
 * kiểm 14 điểm của đường signaling. Cả hai đều xanh mà người dùng vẫn có
 * thể không gọi được: chuông không hiện ra, nút "Nghe" không gắn handler,
 * getUserMedia bị CSP chặn, hay màn hình gọi trắng vì lỗi hydrate.
 *
 * Bộ này dựng HAI phiên trình duyệt riêng và chỉ khẳng định những gì NGƯỜI
 * DÙNG thấy.
 *
 * MICRO VÀ CAMERA GIẢ
 *
 * Chromium chạy với thiết bị giả (xem playwright.config.ts). Không có
 * chúng thì getUserMedia treo ở hộp thoại xin quyền và mọi phép thử hết
 * giờ chờ — triệu chứng giống hệt lỗi sản phẩm, nên rất tốn thời gian lần.
 */

// Hai phiên dùng chung một hộp thư MailHog, nên đăng nhập phải TUẦN TỰ:
// hai lần đăng nhập chồng nhau sẽ nhặt nhầm mã của nhau.
async function openSession(
  browser: Browser,
  email: string,
  password: string,
): Promise<Page> {
  const context = await browser.newContext({
    permissions: ["microphone", "camera"],
  });
  const page = await context.newPage();
  await login(page, email, password);
  return page;
}

/**
 * Mở đúng hội thoại trong trang chat.
 *
 * Trang chat giữ hội thoại đang mở trong state chứ không đưa lên URL, nên
 * không có đường tắt nào — phải bấm đúng như người dùng bấm. Hội thoại
 * 1-1 hiển thị bằng tên người đối diện.
 */
async function openConversation(page: Page, name: string): Promise<void> {
  await page.goto("/chat");
  await page.getByText(name, { exact: true }).first().click();
  await expect(page.getByPlaceholder("Nhập tin nhắn...")).toBeVisible({
    timeout: 20_000,
  });
}

test.describe("Luồng 6 — Gọi thoại và gọi video", () => {
  let peer: Colleague;
  let conversationId: string;

  // HAI phiên dùng chung cho cả hai phép thử, dựng MỘT lần.
  //
  // Khác với các luồng khác — chúng đăng nhập lại ở mỗi phép thử cho
  // độc lập. Ở đây mỗi phép thử cần HAI người, tức bốn lượt đăng nhập,
  // và nginx chỉ cho 30 lượt mỗi phút cho nhóm endpoint đăng nhập (mỗi
  // lượt tốn hai request: login và verify-otp). Dùng chung phiên đưa bốn
  // lượt về còn hai, đủ để cả bộ không chạm trần.
  let callerPage: Page;
  let calleePage: Page;
  let adminToken: string;

  test.beforeAll(async ({ browser }) => {
    adminToken = await apiLogin(ADMIN_EMAIL, ADMIN_PASSWORD);
    const admin = adminToken;

    // Tên phải DUY NHẤT theo từng lần chạy.
    //
    // Bộ này không dọn nhân viên nó tạo ra, nên sau vài lần chạy sẽ có
    // nhiều người trùng tên và nhiều hội thoại trùng nhãn. Lúc đó
    // .first() bấm vào hội thoại của LẦN TRƯỚC, cuộc gọi đổ chuông cho
    // một tài khoản không ai đang mở, và phép thử báo hỏng giống hệt
    // như lỗi sản phẩm. Đã mất thời gian lần ra một lần.
    const tag = suffix();
    peer = await createColleague(
      admin,
      `CALL${tag}`.slice(0, 20),
      `Đỗ Thị Nghe Máy ${tag}`,
    );
    conversationId = await openDirectConversation(admin, peer.employeeId);
    expect(conversationId).toBeTruthy();

    callerPage = await openSession(browser, ADMIN_EMAIL, ADMIN_PASSWORD);
    calleePage = await openSession(browser, peer.email, peer.password);
  });

  test.afterAll(async () => {
    await callerPage?.context().close();
    await calleePage?.context().close();
  });

  test("gọi video: đầu kia đổ chuông, bắt máy, rồi kết thúc cho tất cả", async () => {
    // Người nhận chỉ cần ĐANG MỞ hệ thống, không cần ở trang chat. Cuộc gọi
    // tới phải reo ở mọi trang — đó là lý do màn hình gọi gắn ở AppShell.
    await calleePage.goto("/attendance");

    await openConversation(callerPage, peer.fullName);
    await callerPage.getByRole("button", { name: "Gọi video" }).click();

    // --- Đổ chuông ---
    const ring = calleePage.getByText("Cuộc gọi video đến");
    await expect(ring).toBeVisible({ timeout: 20_000 });
    await expect(calleePage.getByText(/Tự tắt sau \d+ giây/)).toBeVisible();

    // --- Bắt máy ---
    await calleePage.getByRole("button", { name: "Nghe" }).click();

    // Cả hai phải vào màn hình cuộc gọi. Nút "Rời cuộc gọi" là bằng chứng
    // chắc nhất: nó chỉ tồn tại khi màn hình đó đang mở.
    await expect(
      calleePage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toBeVisible({ timeout: 20_000 });
    await expect(
      callerPage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toBeVisible({ timeout: 20_000 });

    // Màn hình đổ chuông phải biến mất sau khi bắt máy.
    await expect(ring).toHaveCount(0);

    // Thanh điều khiển có đủ ba nút thiết bị.
    for (const name of [/mic/i, /camera/i, /Chia sẻ màn hình/]) {
      await expect(calleePage.getByRole("button", { name })).toBeVisible();
    }

    // --- Dấu vết luồng media ---
    //
    // Ba cờ had_* được ghi từ thứ SFU THẤY, đúng lúc kết thúc cuộc gọi.
    //
    // Phép thử này canh một lỗi đã xảy ra thật: trình duyệt "nối được"
    // vào phòng nhưng không publish luồng nào, nên cuộc gọi không có
    // tiếng lẫn hình. Giao diện trông bình thường hoàn toàn — chỉ SFU
    // biết, và chỉ biết nếu có ai hỏi.
    const live = await liveCall(adminToken, conversationId);
    expect(live, "không tra được cuộc gọi đang diễn ra").not.toBeNull();

    // Đợi trình duyệt publish xong mic và camera.
    await callerPage.waitForTimeout(5_000);

    // --- Kết thúc cho tất cả ---
    //
    // Chỉ người khởi tạo thấy nút này. Người kia chỉ rời được — "tôi xong
    // rồi" và "cuộc họp này xong rồi" là hai việc khác nhau.
    await expect(
      calleePage.getByRole("button", { name: "Kết thúc cho tất cả" }),
    ).toHaveCount(0);

    await callerPage
      .getByRole("button", { name: "Kết thúc cho tất cả" })
      .click();

    // Màn hình gọi phải đóng ở CẢ HAI phía, không chỉ phía bấm nút.
    await expect(
      callerPage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toHaveCount(0, { timeout: 20_000 });
    await expect(
      calleePage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toHaveCount(0, { timeout: 20_000 });

    const done = await callDetail(adminToken, live!.id);
    expect(done.status).toBe("ended");

    // Khẳng định số người TRƯỚC khi duyệt: danh sách rỗng thì vòng lặp
    // bên dưới chạy không lần nào và phép thử xanh mà chẳng kiểm gì.
    expect(done.participants).toHaveLength(2);

    for (const p of done.participants ?? []) {
      expect(p.had_audio, `${p.employee_id} không phát mic`).toBe(true);
      expect(p.had_video, `${p.employee_id} không phát camera`).toBe(true);
    }
  });

  test("bảng thiết bị: đúng thiết bị đang dùng, micro có tiếng, đổi được camera", async () => {
    await calleePage.goto("/attendance");
    await openConversation(callerPage, peer.fullName);
    await callerPage.getByRole("button", { name: "Gọi video" }).click();
    await expect(calleePage.getByText("Cuộc gọi video đến")).toBeVisible({
      timeout: 20_000,
    });
    await calleePage.getByRole("button", { name: "Nghe" }).click();
    await expect(
      callerPage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toBeVisible({ timeout: 20_000 });

    await callerPage.getByRole("button", { name: "Thiết bị" }).click();

    const mic = callerPage.getByRole("combobox", {
      name: "Micro",
      exact: true,
    });
    const cam = callerPage.getByRole("combobox", {
      name: "Camera",
      exact: true,
    });

    // Nhãn thật, không phải "Thiết bị 1": quyền đã cấp thì trình duyệt
    // phải trả tên thiết bị. Nhãn rỗng nghĩa là danh sách được đọc trước
    // khi có quyền.
    await expect(mic.locator("option").first()).not.toHaveText(
      /^Thiết bị \d+$/,
    );
    await expect(cam.locator("option").first()).not.toHaveText(
      "Không tìm thấy thiết bị",
    );

    // Thanh đo phải nhảy. Micro giả của Chromium phát tiếng bíp mỗi giây,
    // SFU báo mức âm thanh theo chu kỳ — chờ vài chu kỳ.
    const meter = callerPage.getByRole("meter", { name: "Mức âm thanh micro" });
    await expect
      .poll(async () => Number(await meter.getAttribute("aria-valuenow")), {
        timeout: 15_000,
      })
      .toBeGreaterThan(0);

    // Đổi camera sang thiết bị khác (nếu máy có hơn một) và select phải
    // giữ đúng lựa chọn, không nhảy về dòng đầu.
    const camIds = await cam
      .locator("option")
      .evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value));
    if (camIds.length > 1) {
      await cam.selectOption(camIds[1]);
      await expect(cam).toHaveValue(camIds[1]);
      await expect(
        callerPage.getByText("Không chuyển được sang thiết bị này"),
      ).toHaveCount(0);
    }

    await callerPage.getByRole("button", { name: "Đóng" }).click();
    await expect(meter).toHaveCount(0);

    await callerPage
      .getByRole("button", { name: "Kết thúc cho tất cả" })
      .click();
    await expect(
      calleePage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toHaveCount(0, { timeout: 20_000 });
  });

  test("mỗi lúc một người chiếu: người sau chiếu thay, người trước tự dừng và được báo", async () => {
    await calleePage.goto("/attendance");
    await openConversation(callerPage, peer.fullName);
    await callerPage.getByRole("button", { name: "Gọi video" }).click();
    await expect(calleePage.getByText("Cuộc gọi video đến")).toBeVisible({
      timeout: 20_000,
    });
    await calleePage.getByRole("button", { name: "Nghe" }).click();
    await expect(
      callerPage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toBeVisible({ timeout: 20_000 });

    // Người gọi chiếu trước.
    await callerPage.getByRole("button", { name: "Chia sẻ màn hình" }).click();
    await expect(
      callerPage.getByRole("button", { name: "Dừng chia sẻ" }),
    ).toBeVisible({ timeout: 15_000 });

    // Người nghe bấm chiếu: phải được hỏi trước, chưa chiếu ngay.
    await calleePage.getByRole("button", { name: "Chia sẻ màn hình" }).click();
    const ask = calleePage.getByRole("alertdialog", { name: "Thay người đang chiếu" });
    await expect(ask).toBeVisible({ timeout: 10_000 });
    await expect(
      callerPage.getByRole("button", { name: "Dừng chia sẻ" }),
    ).toBeVisible();

    await ask.getByRole("button", { name: "Chiếu thay" }).click();

    // Người nghe chiếu; máy người gọi tự dừng và nói rõ ai đã thay.
    await expect(
      calleePage.getByRole("button", { name: "Dừng chia sẻ" }),
    ).toBeVisible({ timeout: 15_000 });
    await expect(
      callerPage.getByRole("button", { name: "Chia sẻ màn hình" }),
    ).toBeVisible({ timeout: 15_000 });
    await expect(callerPage.getByText(/đã bắt đầu chiếu màn hình/)).toBeVisible();

    await callerPage
      .getByRole("button", { name: "Kết thúc cho tất cả" })
      .click();
    await expect(
      calleePage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toHaveCount(0, { timeout: 20_000 });
  });

  test("từ chối cuộc gọi thì màn hình gọi đóng ở cả hai phía", async () => {
    await calleePage.goto("/attendance");
    await openConversation(callerPage, peer.fullName);

    await callerPage.getByRole("button", { name: "Gọi", exact: true }).click();

    await expect(calleePage.getByText("Cuộc gọi đến")).toBeVisible({
      timeout: 20_000,
    });
    await calleePage.getByRole("button", { name: "Từ chối" }).click();

    // Người từ chối không còn thấy chuông.
    await expect(calleePage.getByText("Cuộc gọi đến")).toHaveCount(0, {
      timeout: 20_000,
    });

    // Và người gọi cũng không ngồi lại trong một phòng rỗng: gọi 1-1 mà
    // đầu kia từ chối thì cuộc gọi kết thúc ngay, dù người gọi VẪN đang
    // trong phòng. Đây từng là chỗ hỏng làm cuộc gọi treo ở 'ringing' và
    // chặn vĩnh viễn mọi cuộc gọi sau trong cùng hội thoại.
    await expect(
      callerPage.getByRole("button", { name: "Rời cuộc gọi" }),
    ).toHaveCount(0, { timeout: 25_000 });
  });
});
