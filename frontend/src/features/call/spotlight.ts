import type { Tile } from "./useLiveKitRoom";

/**
 * Bố cục "người đang nói": một ô lớn, còn lại là dải ô nhỏ.
 *
 * ĐÂY CHÍNH LÀ CÁCH hạ độ nét những người không nói. Phòng bật
 * adaptiveStream: SDK chọn lớp simulcast theo KÍCH THƯỚC ô trên màn hình,
 * và bỏ qua mọi lệnh đặt độ nét bằng tay (setVideoQuality). Ô trong dải
 * nhỏ cỡ 180px nên SFU tự gửi lớp 180p; ô lớn nhận lớp cao nhất. Không cần
 * — và không được — can thiệp thêm.
 *
 * Trả về null khi nên dùng lưới đều: gọi 1-1 không có màn hình chiếu thì
 * hai ô ngang nhau là đúng, không ai là "người phụ".
 */
export function spotlight(
  tiles: Tile[],
  lastSpeaker: string | undefined,
): { main: Tile; strip: Tile[] } | null {
  // Màn hình chiếu luôn lên ô lớn — đó là thứ cả phòng đang cùng xem.
  // Ưu tiên màn hình của NGƯỜI KHÁC: người đang chiếu không cần xem to
  // chính màn hình của mình.
  const screens = tiles.filter((t) => t.isScreen);
  const screen = screens.find((t) => !t.isLocal) ?? screens[0];
  if (screen) {
    return { main: screen, strip: tiles.filter((t) => t !== screen) };
  }

  const cams = tiles.filter((t) => !t.isScreen);
  if (cams.length < 3) return null;

  // Người đang nói, không tính chính mình. Không ai nói thì giữ người nói
  // gần nhất — ô lớn nhảy qua lại mỗi lần ai đó ho là thứ gây chóng mặt
  // nhất của bố cục này.
  const remote = cams.filter((t) => !t.isLocal);
  const main =
    remote.find((t) => t.speaking) ??
    remote.find((t) => t.identity === lastSpeaker) ??
    remote[0];
  if (!main) return null;

  return { main, strip: tiles.filter((t) => t !== main) };
}
