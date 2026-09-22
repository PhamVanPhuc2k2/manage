import { NextResponse, type NextRequest } from "next/server";

/**
 * Đặt Content-Security-Policy kèm nonce cho từng request.
 *
 * VÌ SAO KHÔNG ĐỂ NGINX ĐẶT
 *
 * nginx từng đặt `script-src 'self'` với lập luận rằng Next.js ở chế độ
 * production không cần 'unsafe-inline'. Lập luận đó SAI: Next.js luôn nhúng
 * vài đoạn script nội tuyến để khởi động và để truyền dữ liệu streaming
 * (self.__next_f). Trình duyệt chặn chúng, React không hydrate, và mọi trang
 * biến thành HTML tĩnh — bấm nút không có gì xảy ra.
 *
 * Triệu chứng đặc biệt khó lần: trang vẫn hiện đầy đủ và đúng bố cục, máy
 * chủ vẫn trả 200, không có lỗi nào trong log. Chỉ console của trình duyệt
 * mới nói ra sự thật. Bộ Playwright phát hiện ra vì nó là thứ đầu tiên thật
 * sự BẤM vào nút qua nginx.
 *
 * Hai cách sửa, và vì sao chọn cách này:
 *
 *   - Thêm 'unsafe-inline' vào script-src: một dòng, và vứt bỏ đúng lớp
 *     phòng thủ chính chống XSS. Với hệ thống chứa hồ sơ nhân sự và bảng
 *     lương thì đó là đánh đổi tồi.
 *
 *   - Nonce: mỗi request một giá trị ngẫu nhiên, chỉ script mang đúng nonce
 *     được chạy. Script do kẻ tấn công chèn vào không đoán được nonce nên
 *     vẫn bị chặn. Nonce phải sinh ở nơi biết từng request — tức là ở đây,
 *     không phải trong một tệp cấu hình tĩnh của nginx.
 *
 * Next.js tự gắn nonce vào script của nó khi thấy nonce trong header CSP
 * của REQUEST, nên phải đặt header đó lên cả request lẫn response.
 */
export function middleware(request: NextRequest) {
  const nonce = crypto.randomUUID().replaceAll("-", "");

  // 'strict-dynamic': script đã được tin (nhờ nonce) được phép nạp tiếp các
  // script khác. Next.js chia mã thành nhiều chunk nạp động, nên không có nó
  // thì phải liệt kê từng tệp — một danh sách đổi theo mỗi lần build.
  const scriptSrc = [
    "'self'",
    `'nonce-${nonce}'`,
    "'strict-dynamic'",
    // Chế độ phát triển của Next.js dùng eval để nạp lại mã nóng. Chỉ bật ở
    // dev: bật ở production là mở lại đúng cánh cửa vừa đóng.
    process.env.NODE_ENV === "development" ? "'unsafe-eval'" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const csp = [
    "default-src 'self'",
    `script-src ${scriptSrc}`,
    // 'unsafe-inline' cho style là nhượng bộ với Tailwind và Next.js: cả hai
    // chèn style nội tuyến lúc chạy. Style nội tuyến không chạy được mã, nên
    // rủi ro ở đây thấp hơn hẳn so với script.
    "style-src 'self' 'unsafe-inline' https://fonts.googleapis.com",
    "font-src 'self' https://fonts.gstatic.com data:",
    "img-src 'self' data: blob: https:",
    // ws: và wss: là bắt buộc — thiếu chúng thì trình duyệt chặn WebSocket
    // và toàn bộ chat cùng chấm công ngừng hoạt động, còn console chỉ báo
    // một dòng lỗi CSP khó truy.
    "connect-src 'self' ws: wss: https:",
    "frame-ancestors 'self'",
    "base-uri 'self'",
    "form-action 'self'",
    "object-src 'none'",
  ].join("; ");

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  return response;
}

export const config = {
  matcher: [
    /*
     * Bỏ qua các đường tĩnh: chúng không phải tài liệu HTML nên CSP không có
     * tác dụng, và chạy middleware cho từng tệp ảnh là phí.
     *
     * Giữ lại mọi đường còn lại, gồm cả trang lỗi — một trang lỗi cũng là
     * nơi có thể dội lại nội dung do người dùng nhập.
     */
    {
      source: "/((?!_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
