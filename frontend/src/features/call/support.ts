/**
 * Kiểm tra trình duyệt có gọi được không.
 *
 * Ba nhóm hay gặp trong thực tế, và cả ba đều hỏng LẶNG LẼ nếu không kiểm:
 *
 *   - Trình duyệt nhúng trong app Facebook, Zalo, Messenger. Nhân viên bấm
 *     link hệ thống từ tin nhắn là rơi thẳng vào đây. Chúng thường không
 *     cấp `getUserMedia`, nên bấm "Gọi" xong không có gì xảy ra.
 *   - Safari cũ (trước 14.1) thiếu nhiều phần của WebRTC hiện đại.
 *   - Trang chạy qua HTTP trên địa chỉ KHÁC localhost. `getUserMedia` chỉ
 *     tồn tại trong secure context, nên thử hệ thống qua IP LAN là hỏng —
 *     và triệu chứng giống hệt "trình duyệt không hỗ trợ".
 *
 * Trả về chuỗi rỗng khi gọi được, hoặc câu giải thích cho người dùng.
 */
export function callSupportProblem(): string {
  if (typeof window === "undefined") return "";

  if (!window.isSecureContext) {
    return (
      "Trình duyệt chỉ cho phép dùng micro và camera trên kết nối bảo mật. " +
      "Hãy mở hệ thống bằng địa chỉ https://"
    );
  }

  if (!navigator.mediaDevices?.getUserMedia) {
    return (
      "Trình duyệt này không cho phép dùng micro và camera. " +
      "Nếu bạn đang mở trong ứng dụng Zalo hay Facebook, hãy chọn " +
      '"Mở bằng trình duyệt" rồi thử lại.'
    );
  }

  if (typeof window.RTCPeerConnection === "undefined") {
    return "Trình duyệt quá cũ để gọi video. Hãy cập nhật hoặc dùng Chrome, Edge, Firefox.";
  }

  return "";
}
