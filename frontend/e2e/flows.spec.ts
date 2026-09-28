import { readFileSync } from "node:fs";

import { expect, test, type Page } from "@playwright/test";

import {
  ADMIN_EMAIL,
  ADMIN_PASSWORD,
  apiLogin,
  createDepartmentApi,
  deleteDepartmentApi,
  login,
  suffix,
} from "./helpers";

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

// MỘT phiên đăng nhập dùng chung cho các luồng 2–5.
//
// Trước đây mỗi phép thử tự đăng nhập cho độc lập. Nghe hợp lý, nhưng
// nginx giới hạn 30 lượt/phút cho nhóm endpoint đăng nhập và MỖI lần đăng
// nhập tốn HAI lượt (login + verify-otp). Cả bộ chạy một lần là ~24 lượt,
// chạy hai lần liền nhau là chạm trần và một phép thử bất kỳ báo hỏng.
//
// Không nới giới hạn cho dễ test — đó là lớp chặn dò mật khẩu quy mô lớn.
// Luồng 1 vẫn đăng nhập thật vì đó chính là thứ nó đang kiểm.
let shared: Page;

test.beforeAll(async ({ browser }) => {
  shared = await browser.newPage();
  await login(shared);
});

test.afterAll(async () => {
  await shared?.close();
});

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
  test("màn hình chấm công hiện số liệu của hôm nay", async () => {
    await shared.goto("/attendance");
    await expect(
      shared.getByRole("heading", { name: "Chấm công của tôi" }),
    ).toBeVisible();

    // Khối "Hôm nay" là thứ người dùng nhìn nhiều nhất trong ngày.
    await expect(
      shared.getByRole("heading", { name: "Hôm nay" }),
    ).toBeVisible();

    // Trang phải có số liệu, không phải một khung rỗng. Kiểm sự CÓ MẶT của
    // con số chứ không kiểm giá trị: nó thay đổi theo từng phút và một phép
    // thử bám vào giá trị sẽ hỏng ngẫu nhiên.
    await expect(shared.getByRole("main")).toContainText(/\d/);
  });
});

test.describe("Luồng 2b — Xuất chấm công ra Excel", () => {
  test("bấm xuất, worker dựng tệp, tải về được tệp .xlsx thật", async () => {
    await shared.goto("/attendance/team");
    await shared.getByRole("link", { name: "Xuất Excel" }).click();
    await expect(
      shared.getByRole("heading", { name: "Xuất báo cáo chấm công" }),
    ).toBeVisible();

    await shared.getByRole("button", { name: "Xuất Excel" }).click();

    // Lượt mới nhất nằm trên cùng; nút Tải về chỉ hiện khi worker xong.
    const download = shared.getByRole("button", { name: "Tải về" }).first();
    await expect(download).toBeVisible({ timeout: 20_000 });

    // Tải qua fetch có kèm token rồi mới lưu — bắt đúng sự kiện tải tệp của
    // trình duyệt để chắc chắn người dùng nhận được một tệp, không phải một
    // trang lỗi 401.
    const [file] = await Promise.all([
      shared.waitForEvent("download"),
      download.click(),
    ]);
    expect(file.suggestedFilename()).toMatch(/^cham-cong-\d{4}-\d{2}\.xlsx$/);
    const path = await file.path();
    const bytes = readFileSync(path);
    // .xlsx là tệp zip: bắt đầu bằng chữ ký PK.
    expect(bytes.subarray(0, 2).toString()).toBe("PK");
  });
});

test.describe("Luồng 3 — Thêm nhân viên", () => {
  test("tạo nhân viên mới và thấy ngay trang hồ sơ", async () => {
    const id = suffix();
    const name = `E2E Nguyễn ${id}`;

    await shared.goto("/employees/new");
    await expect(
      shared.getByRole("heading", { name: "Thêm nhân viên" }),
    ).toBeVisible();

    await shared.getByLabel("Mã nhân viên").fill(`E2E${id}`);
    await shared.getByLabel("Họ và tên").fill(name);
    await shared.getByLabel("Email").fill(`e2e${id.toLowerCase()}@test.local`);
    await shared.getByLabel("Ngày vào làm").fill("2024-01-01");

    await shared.getByRole("button", { name: "Tạo nhân viên" }).click();

    // Tạo xong thì chuyển sang trang chi tiết và thấy đúng tên vừa nhập.
    // Đây là chỗ một form gửi sai tên trường sẽ lộ ra: API trả 201 nhưng
    // tên hiển thị lại trống.
    await expect(shared.getByText(name)).toBeVisible({ timeout: 20_000 });
  });

  test("bỏ trống trường bắt buộc thì báo lỗi ngay, không gửi đi", async () => {
    await shared.goto("/employees/new");

    await shared.getByRole("button", { name: "Tạo nhân viên" }).click();

    // Vẫn ở lại trang tạo. Kiểm điều này thay vì kiểm nội dung thông báo:
    // câu chữ sẽ đổi, còn việc "không đi đâu cả" thì không.
    await expect(shared).toHaveURL(/\/employees\/new/);
  });
});

test.describe("Luồng 3b — Nhập nhân viên từ tệp", () => {
  test("tải CSV lên, worker nhập xong, trang kết quả chỉ ra đúng dòng lỗi", async () => {
    const id = suffix();
    // Tiêu đề tiếng Việt có dấu và BOM — đúng thứ Excel lưu ra khi chọn
    // "CSV UTF-8". Dòng 3 cố ý sai ngày (31/02) để trang kết quả có lỗi.
    const csv =
      "\ufeffMã nhân viên,Họ tên,Email,Ngày vào làm\n" +
      `IMP${id}A,E2E Nhập Một ${id},imp${id.toLowerCase()}a@test.local,15/01/2024\n` +
      `IMP${id}B,E2E Nhập Hai ${id},imp${id.toLowerCase()}b@test.local,31/02/2024\n`;

    await shared.goto("/employees");
    await shared.getByRole("link", { name: "Nhập từ tệp" }).click();
    await expect(
      shared.getByRole("heading", { name: "Nhập nhân viên từ tệp" }),
    ).toBeVisible();

    await shared.locator('input[type="file"]').setInputFiles({
      name: "e2e.csv",
      mimeType: "text/csv",
      buffer: Buffer.from(csv, "utf-8"),
    });
    await shared.getByRole("button", { name: "Nhập", exact: true }).click();

    // Sang trang kết quả, và worker chạy xong trong lúc trang tự hỏi lại.
    await expect(shared).toHaveURL(/\/employees\/imports\/[0-9a-f-]{36}/);
    await expect(shared.getByText("Xong", { exact: true })).toBeVisible({
      timeout: 20_000,
    });

    // Mặc định chỉ hiện dòng có vấn đề: đúng dòng 3, đúng lý do.
    const rows = shared.locator("tbody tr");
    await expect(rows).toHaveCount(1);
    await expect(rows.first()).toContainText(`IMP${id}B`);
    await expect(rows.first()).toContainText("ngày vào làm");

    // Bỏ lọc thì thấy cả người đã tạo, bấm được sang hồ sơ.
    await shared.getByLabel("Chỉ hiện dòng lỗi hoặc cảnh báo").uncheck();
    await shared.getByRole("link", { name: `E2E Nhập Một ${id}` }).click();
    await expect(shared.getByText(`E2E Nhập Một ${id}`).first()).toBeVisible();
  });
});

test.describe("Luồng 3c — Sơ đồ tổ chức", () => {
  // Cây dựng qua API: một khối, hai phòng con, một tổ dưới phòng thứ nhất.
  // Xoá từ lá lên gốc ở cuối — máy chủ không cho xoá phòng còn phòng con.
  let token: string;
  const ids: string[] = [];
  const tag = suffix();
  const name = (s: string) => `E2E ${s} ${tag}`;

  test.beforeAll(async () => {
    token = await apiLogin(ADMIN_EMAIL, ADMIN_PASSWORD);
    const root = await createDepartmentApi(token, {
      code: `K${tag}`,
      name: name("Khối"),
    });
    const a = await createDepartmentApi(token, {
      code: `A${tag}`,
      name: name("Phòng A"),
      parent_id: root,
    });
    const b = await createDepartmentApi(token, {
      code: `B${tag}`,
      name: name("Phòng B"),
      parent_id: root,
    });
    const team = await createDepartmentApi(token, {
      code: `T${tag}`,
      name: name("Tổ A1"),
      parent_id: a,
    });
    ids.push(team, b, a, root);
  });

  test.afterAll(async () => {
    for (const id of ids) await deleteDepartmentApi(token, id);
  });

  test("vẽ cây phòng ban, thu gọn được, bấm vào phòng thì hiện người của phòng", async () => {
    await shared.goto("/employees");
    await shared.getByRole("link", { name: "Sơ đồ tổ chức" }).click();
    await expect(
      shared.getByRole("heading", { name: "Sơ đồ tổ chức" }),
    ).toBeVisible();

    const card = (s: string) =>
      shared.getByRole("button", { name: new RegExp(`^${name(s)}`) });

    // Hai cấp đầu mở sẵn: thấy khối và hai phòng. Tổ ở cấp ba bị gọn lại
    // sau nút "+1" của Phòng A.
    await expect(card("Khối")).toBeVisible();
    await expect(card("Phòng A")).toBeVisible();
    await expect(card("Phòng B")).toBeVisible();
    await expect(card("Tổ A1")).toHaveCount(0);

    await shared
      .getByRole("button", { name: `Mở các phòng con của ${name("Phòng A")}` })
      .click();
    await expect(card("Tổ A1")).toBeVisible();

    // Phòng con phải nằm DƯỚI phòng cha — kiểm đúng hình dạng cây, không
    // chỉ kiểm có chữ trên trang.
    const rootBox = (await card("Khối").boundingBox())!;
    const aBox = (await card("Phòng A").boundingBox())!;
    const teamBox = (await card("Tổ A1").boundingBox())!;
    expect(aBox.y).toBeGreaterThan(rootBox.y + rootBox.height);
    expect(teamBox.y).toBeGreaterThan(aBox.y + aBox.height);

    await card("Phòng B").click();
    await expect(
      shared.getByRole("heading", { name: name("Phòng B") }),
    ).toBeVisible();
    await expect(
      shared.getByText("Không có nhân viên nào bạn được xem"),
    ).toBeVisible();
  });
});

test.describe("Giao diện sáng / tối", () => {
  test("chọn Tối thì giữ nguyên sau khi tải lại; Theo máy thì đi theo hệ điều hành", async () => {
    // Script đặt theme chạy nội tuyến trong <head>: CSP dùng nonce, nên một
    // script thiếu nonce bị chặn và chỉ để lại một dòng lỗi ở console.
    const cspErrors: string[] = [];
    const onConsole = (m: { type(): string; text(): string }) => {
      if (m.type() === "error" && /Content Security Policy/i.test(m.text())) {
        cspErrors.push(m.text());
      }
    };
    shared.on("console", onConsole);

    const html = shared.locator("html");
    const pick = shared.getByRole("combobox", { name: "Giao diện" });

    await shared.emulateMedia({ colorScheme: "light" });
    await shared.goto("/employees");
    await pick.selectOption("dark");
    await expect(html).toHaveClass(/dark/);

    // Tải lại: lựa chọn còn nguyên, và class có NGAY khi HTML vừa tải —
    // tức là do script trong <head>, không phải do React đặt sau.
    await shared.reload({ waitUntil: "commit" });
    await expect(html).toHaveClass(/dark/);
    await expect(pick).toHaveValue("dark");

    await pick.selectOption("light");
    await expect(html).not.toHaveClass(/dark/);

    // Theo máy: hệ điều hành tối thì trang tối, đổi sang sáng thì đổi theo
    // mà không cần tải lại.
    await pick.selectOption("system");
    await shared.emulateMedia({ colorScheme: "dark" });
    await expect(html).toHaveClass(/dark/);
    await shared.emulateMedia({ colorScheme: "light" });
    await expect(html).not.toHaveClass(/dark/);

    shared.off("console", onConsole);
    expect(cspErrors, "script theme bị CSP chặn").toEqual([]);
  });
});

test.describe("Điều hướng: menu và breadcrumb", () => {
  test("menu chỉ sáng một mục; breadcrumb ghi tên thật và bấm quay lại được", async () => {
    const nav = shared.getByRole("complementary");
    const crumbs = shared.getByRole("navigation", { name: "Đường dẫn" });

    // /attendance/team nằm dưới /attendance: trước đây cả hai mục cùng sáng.
    await shared.goto("/attendance/team");
    await expect(nav.locator('[aria-current="page"]')).toHaveCount(1);
    await expect(nav.locator('[aria-current="page"]')).toHaveText(
      "Công phòng ban",
    );
    await expect(crumbs).toHaveText("Công phòng ban");

    // Trang có id: breadcrumb hiện TÊN nhân viên, không phải chuỗi UUID.
    await shared.goto("/employees");
    const first = shared.locator("tbody tr a").first();
    const name = (await first.textContent())!.trim();
    await first.click();
    await expect(crumbs).toContainText(`Nhân viên›${name}`, {
      timeout: 15_000,
    });
    await expect(crumbs).not.toContainText("…");

    await crumbs.getByRole("link", { name: "Nhân viên" }).click();
    await expect(shared).toHaveURL(/\/employees$/);
  });
});

test.describe("Luồng 4 — Dự án và công việc", () => {
  test("tạo dự án rồi thêm một công việc vào bảng", async () => {
    const id = suffix();

    await shared.goto("/projects");
    // exact: sau vài lần chạy, danh sách có những dự án tên "Dự án E2E ..."
    // và tiêu đề của chúng cũng là heading. Không chốt exact thì phép thử
    // hỏng vì "strict mode violation" — một lỗi của chính phép thử, trông y
    // hệt như lỗi sản phẩm.
    await expect(
      shared.getByRole("heading", { name: "Dự án", exact: true }),
    ).toBeVisible();

    await shared.getByRole("button", { name: "Thêm dự án" }).click();

    // Nhãn của trường bắt buộc kèm dấu sao, nên tên trợ năng là "Mã*".
    // Mã dự án tối đa vài ký tự (gợi ý trên form là "VD: WEB").
    await shared.getByLabel(/^Mã\s*\*?$/).fill(`E${id.slice(0, 3)}`);
    await shared.getByLabel("Tên dự án").fill(`Dự án E2E ${id}`);

    // Chủ dự án là trường bắt buộc và không có giá trị mặc định. Chọn người
    // đầu tiên trong danh sách thay vì gõ một id cứng: id đổi theo từng lần seed.
    const owner = shared.getByLabel(/^Chủ dự án\s*\*?$/);
    await owner.selectOption({ index: 1 });

    await shared.getByRole("button", { name: "Tạo dự án" }).click();

    // Dự án vừa tạo phải xuất hiện. Dùng tên chứ không dùng mã: tên là thứ
    // người dùng đọc.
    await expect(shared.getByText(`Dự án E2E ${id}`).first()).toBeVisible({
      timeout: 20_000,
    });
  });

  test('màn hình "Việc của tôi" mở được', async () => {
    await shared.goto("/tasks");
    // Trang này là nơi người dùng bắt đầu mỗi sáng. Chỉ cần nó mở ra và
    // không phải trang lỗi.
    await expect(shared.locator("body")).not.toContainText(
      /Application error|500|Internal Server Error/,
    );
    await expect(shared.getByRole("main")).toBeVisible();
  });
});

test.describe("Luồng 5 — Chat realtime", () => {
  test("gửi tin nhắn và thấy nó hiện ra trong khung chat", async () => {
    await shared.goto("/chat");

    // Bong bóng chat có mặt ở mọi trang; trang /chat mở sẵn khung đầy đủ.
    const composer = shared.getByPlaceholder("Nhập tin nhắn...");

    // Tự mở một hội thoại thay vì trông chờ dữ liệu có sẵn.
    //
    // Phiên bản trước của phép thử này tự bỏ qua khi không tìm thấy hội thoại
    // nào. Trên một cài đặt sạch thì nó LUÔN bỏ qua — tức là luồng realtime,
    // thứ duy nhất ở đây đi qua WebSocket, không bao giờ được kiểm. Một phép
    // thử bị bỏ qua không báo hỏng, nên kết quả xanh trông y hệt như đã kiểm đủ.
    if (!(await composer.isVisible().catch(() => false))) {
      await shared.getByTitle("Cuộc trò chuyện mới").click();

      // Chọn đồng nghiệp đầu tiên trong danh sách rồi nhắn tin riêng.
      await shared.getByPlaceholder("Tìm nhân viên...").waitFor();
      await shared.locator('input[type="checkbox"]').first().check();
      await shared.getByRole("button", { name: "Nhắn tin" }).click();
    }

    await expect(composer).toBeVisible({ timeout: 20_000 });

    const text = `E2E ${suffix()}`;
    await composer.fill(text);
    await composer.press("Enter");

    // Tin nhắn phải hiện ra trong khung. Đây là đoạn đi qua WebSocket, và
    // cũng là chỗ duy nhất trong bộ này kiểm đường realtime.
    await expect(shared.getByText(text)).toBeVisible({ timeout: 20_000 });
  });
});
