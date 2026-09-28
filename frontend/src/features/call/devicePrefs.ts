/**
 * Thiết bị đã chọn cho cuộc gọi, nhớ giữa các lần gọi.
 *
 * Chọn ở trang Kiểm tra thiết bị hay ở bảng Thiết bị giữa cuộc gọi đều ghi
 * vào đây, và cuộc gọi sau bật đúng thiết bị đó. Không nhớ thì người dùng
 * tai nghe phải chọn lại tai nghe ở MỌI cuộc gọi — và thường chỉ nhớ ra
 * khi người kia đã nói "không nghe thấy gì".
 *
 * LƯU CẢ TÊN, KHÔNG CHỈ MÃ. Mã thiết bị (deviceId) không ổn định: Chromium
 * đổi mã khi quyền camera/micro không được lưu vĩnh viễn, Safari đổi theo
 * từng phiên. Playwright bắt được đúng chuyện này — cùng một micro mang hai
 * mã khác nhau ở hai lần tải trang. Tìm theo mã trước, không thấy thì tìm
 * theo tên ("Jabra Evolve 65" thì không đổi).
 *
 * Chỉ là sở thích của trình duyệt này, nên để localStorage chứ không lên
 * máy chủ: thiết bị của máy tính công ty vô nghĩa trên điện thoại.
 */

export type DeviceKind = "audioinput" | "videoinput" | "audiooutput";
export type DevicePref = { id: string; label: string };
export type DevicePrefs = Partial<Record<DeviceKind, DevicePref>>;

const KEY = "call-devices";

export function loadDevicePrefs(): DevicePrefs {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as DevicePrefs) : {};
  } catch {
    return {};
  }
}

export function saveDevicePref(
  kind: DeviceKind,
  device: Pick<MediaDeviceInfo, "deviceId" | "label">,
) {
  try {
    const prefs = loadDevicePrefs();
    prefs[kind] = { id: device.deviceId, label: device.label };
    localStorage.setItem(KEY, JSON.stringify(prefs));
  } catch {
    // Không lưu được (chế độ ẩn danh, bị chặn) thì chỉ mất phần "nhớ".
  }
}

/**
 * Mã hiện tại của thiết bị đã lưu, trong danh sách thiết bị ĐANG có.
 * undefined khi thiết bị không còn (rút ra, đổi máy) — dùng mặc định.
 */
export function resolveDevice(
  pref: DevicePref | undefined,
  devices: Pick<MediaDeviceInfo, "deviceId" | "label">[],
): string | undefined {
  if (!pref) return undefined;
  const byId = devices.find((d) => d.deviceId === pref.id);
  if (byId) return byId.deviceId;
  if (!pref.label) return undefined;
  return devices.find((d) => d.label === pref.label)?.deviceId;
}
