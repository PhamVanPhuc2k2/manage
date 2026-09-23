import { expect, test } from "@playwright/test";

import { login, suffix } from "./helpers";

/**
 * Năm luồng quan trọng nhất, chạy trên trình duyệt thật.
 *
 * TIÊU CHÍ CHỌN
 *
 * Không chọn theo "màn hình nào nhiều mã nhất" mà theo "hỏng cái gì thì cả
 * công ty dừng lại":
 *
 *  1. Đăng nhập — hỏng thì không ai vào được, mọi thứ khác thành vô nghĩa.
 *  2. Chấm công — chạy ngầm cả ngày và quyết định con số đi vào phiếu lương.
 *  3. Nhân sự — cửa duy nhất để một người mới bắt đầu dùng hệ thống.
 *  4. Công việc — thứ người dùng chạm vào nhiều nhất mỗi ngày.
 *  5. Chat — đường realtime, và là thứ duy nhất ở đây đi qua WebSocket.
 *
 * CÁC PHÉP THỬ KHẲNG ĐỊNH ĐIỀU GÌ
 *
 * Chỉ khẳng định những gì NGƯỜI DÙNG thấy. Không kiểm lời gọi mạng, không
 * kiểm hình dạng dữ liệu — hai thứ đó đã có 286 phép kiểm smoke lo, và lặp
 * lại chúng ở đây chỉ làm bộ này chậm và dễ vỡ hơn mà không thêm thông tin.
 */

// Các luồng chạy tuần tự và dùng chung một phiên đăng nhập trong mỗi file
// test, nên đăng nhập một lần ở đầu mỗi phép thử là đủ rẻ và giữ cho từng
// phép thử độc lập.

test.describe("Luồng 1 — Đăng nhập", () => {
  test("đăng nhập bằng mật khẩu và mã xác minh rồi vào được hệ thống", async ({
    page,
  }) => {
    await login(page);

    // Vào được nghĩa là thấy khung ứng dụng, không phải màn hình đăng nhập.
    await expect(page).not.toHaveURL(/\/login/);

    // Ô nhập mật khẩu biến mất là bằng chứng chắc chắn nhất: nó chỉ có trên
    // màn hình đăng nhập. Bám vào một phần tử cụ thể của trang chủ thì phép
    // thử hỏng mỗi lần đổi bố cục, mà đó không phải thứ nó đang kiểm.
    await expect(page.getByLabel("Mật khẩu")).toHaveCount(0);
  });

  test("sai mật khẩu thì báo lỗi và KHÔNG vào được", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Email").fill("admin@abc.vn");
    await page.getByLabel("Mật khẩu").fill("SaiHoanToan#2026");
    await page.getByRole("button", { name: "Đăng nhập" }).click();

    // Thông báo lỗi phải HIỆN RA. Một trang đứng im sau khi bấm là cách tệ
    // nhất để báo lỗi: người dùng bấm lại vài lần rồi nghĩ hệ thống hỏng.
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(page).toHaveURL(/\/login/);
  });
});

test.describe("Luồng 2 — Chấm công", () => {
  test("màn hình chấm công hiện số liệu của hôm nay", async ({ page }) => {
    await login(page);

    await page.goto("/attendance");
    await expect(
      page.getByRole("heading", { name: "Chấm công của tôi" }),
    ).toBeVisible();

    // Khối "Hôm nay" là thứ người dùng nhìn nhiều nhất trong ngày.
    await expect(
      page.getByRole("heading", { name: "Hôm nay" }),
    ).toBeVisible();

    // Trang phải có số liệu, không phải một khung rỗng. Kiểm sự CÓ MẶT của
    // con số chứ không kiểm giá trị: nó thay đổi theo từng phút và một phép
    // thử bám vào giá trị sẽ hỏng ngẫu nhiên.
    await expect(page.getByRole("main")).toContainText(/\d/);
  });
});

test.describe("Luồng 3 — Thêm nhân viên", () => {
  test("tạo nhân viên mới và thấy ngay trang hồ sơ", async ({ page }) => {
    await login(page);

    const id = suffix();
    const name = `E2E Nguyễn ${id}`;

    await page.goto("/employees/new");
    await expect(
      page.getByRole("heading", { name: "Thêm nhân viên" }),
    ).toBeVisible();

    await page.getByLabel("Mã nhân viên").fill(`E2E${id}`);
    await page.getByLabel("Họ và tên").fill(name);
    await page.getByLabel("Email").fill(`e2e${id.toLowerCase()}@test.local`);
    await page.getByLabel("Ngày vào làm").fill("2024-01-01");

    await page.getByRole("button", { name: "Tạo nhân viên" }).click();

    // Tạo xong thì chuyển sang trang chi tiết và thấy đúng tên vừa nhập.
    // Đây là chỗ một form gửi sai tên trường sẽ lộ ra: API trả 201 nhưng
    // tên hiển thị lại trống.
    await expect(page.getByText(name)).toBeVisible({ timeout: 20_000 });
  });

  test("bỏ trống trường bắt buộc thì báo lỗi ngay, không gửi đi", async ({
    page,
  }) => {
    await login(page);
    await page.goto("/employees/new");

    await page.getByRole("button", { name: "Tạo nhân viên" }).click();

    // Vẫn ở lại trang tạo. Kiểm điều này thay vì kiểm nội dung thông báo:
    // câu chữ sẽ đổi, còn việc "không đi đâu cả" thì không.
    await expect(page).toHaveURL(/\/employees\/new/);
  });
});

test.describe("Luồng 4 — Dự án và công việc", () => {
  test("tạo dự án rồi thêm một công việc vào bảng", async ({ page }) => {
    await login(page);

    const id = suffix();

    await page.goto("/projects");
    // exact: sau vài lần chạy, danh sách có những dự án tên "Dự án E2E ..."
    // và tiêu đề của chúng cũng là heading. Không chốt exact thì phép thử
    // hỏng vì "strict mode violation" — một lỗi của chính phép thử, trông y
    // hệt như lỗi sản phẩm.
    await expect(
      page.getByRole("heading", { name: "Dự án", exact: true }),
    ).toBeVisible();

    await page.getByRole("button", { name: "Thêm dự án" }).click();

    // Nhãn của trường bắt buộc kèm dấu sao, nên tên trợ năng là "Mã*".
    // Mã dự án tối đa vài ký tự (gợi ý trên form là "VD: WEB").
    await page.getByLabel(/^Mã\s*\*?$/).fill(`E${id.slice(0, 3)}`);
    await page.getByLabel("Tên dự án").fill(`Dự án E2E ${id}`);

    // Chủ dự án là trường bắt buộc và không có giá trị mặc định. Chọn người
    // đầu tiên trong danh sách thay vì gõ một id cứng: id đổi theo từng lần seed.
    const owner = page.getByLabel(/^Chủ dự án\s*\*?$/);
    await owner.selectOption({ index: 1 });

    await page.getByRole("button", { name: "Tạo dự án" }).click();

    // Dự án vừa tạo phải xuất hiện. Dùng tên chứ không dùng mã: tên là thứ
    // người dùng đọc.
    await expect(page.getByText(`Dự án E2E ${id}`).first()).toBeVisible({
      timeout: 20_000,
    });
  });

  test('màn hình "Việc của tôi" mở được', async ({ page }) => {
    await login(page);

    await page.goto("/tasks");
    // Trang này là nơi người dùng bắt đầu mỗi sáng. Chỉ cần nó mở ra và
    // không phải trang lỗi.
    await expect(page.locator("body")).not.toContainText(
      /Application error|500|Internal Server Error/,
    );
    await expect(page.getByRole("main")).toBeVisible();
  });
});

test.describe("Luồng 5 — Chat realtime", () => {
  test("gửi tin nhắn và thấy nó hiện ra trong khung chat", async ({ page }) => {
    await login(page);

    await page.goto("/chat");

    // Bong bóng chat có mặt ở mọi trang; trang /chat mở sẵn khung đầy đủ.
    const composer = page.getByPlaceholder("Nhập tin nhắn...");

    // Tự mở một hội thoại thay vì trông chờ dữ liệu có sẵn.
    //
    // Phiên bản trước của phép thử này tự bỏ qua khi không tìm thấy hội thoại
    // nào. Trên một cài đặt sạch thì nó LUÔN bỏ qua — tức là luồng realtime,
    // thứ duy nhất ở đây đi qua WebSocket, không bao giờ được kiểm. Một phép
    // thử bị bỏ qua không báo hỏng, nên kết quả xanh trông y hệt như đã kiểm đủ.
    if (!(await composer.isVisible().catch(() => false))) {
      await page.getByTitle("Cuộc trò chuyện mới").click();

      // Chọn đồng nghiệp đầu tiên trong danh sách rồi nhắn tin riêng.
      await page.getByPlaceholder("Tìm nhân viên...").waitFor();
      await page.locator('input[type="checkbox"]').first().check();
      await page.getByRole("button", { name: "Nhắn tin" }).click();
    }

    await expect(composer).toBeVisible({ timeout: 20_000 });

    const text = `E2E ${suffix()}`;
    await composer.fill(text);
    await composer.press("Enter");

    // Tin nhắn phải hiện ra trong khung. Đây là đoạn đi qua WebSocket, và
    // cũng là chỗ duy nhất trong bộ này kiểm đường realtime.
    await expect(page.getByText(text)).toBeVisible({ timeout: 20_000 });
  });
});
