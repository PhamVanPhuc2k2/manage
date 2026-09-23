/**
 * Chuông gọi đến, dựng bằng WebAudio thay vì một tệp mp3.
 *
 * Vì sao không dùng tệp âm thanh: một tệp chuông tử tế nặng vài trăm KB và
 * phải tải về trước khi dùng được. Cuộc gọi tới là đúng lúc KHÔNG được chờ
 * tải gì cả. Hai dao động hình sin xen kẽ nghe đủ giống chuông điện thoại,
 * nặng 0 byte và kêu ngay lập tức.
 *
 * Trình duyệt CHẶN phát âm thanh khi trang chưa từng được người dùng chạm
 * vào. Đó là lý do chuông có thể im lặng không kêu ở tab vừa mở mà chưa
 * click — nên màn hình đổ chuông không bao giờ chỉ dựa vào tiếng: nó luôn
 * hiện ra và nhấp nháy.
 */

type Ringing = { stop: () => void };

const BEEP_MS = 400;
const GAP_MS = 200;
const CYCLE_MS = 2400;

export function startRingtone(): Ringing {
  let ctx: AudioContext | null = null;
  let timer: ReturnType<typeof setInterval> | null = null;
  let stopped = false;

  try {
    ctx = new AudioContext();
  } catch {
    // Không có WebAudio thì bỏ qua tiếng, màn hình vẫn đổ chuông.
    return { stop: () => {} };
  }

  const audio = ctx;

  // Trình duyệt tạo AudioContext ở trạng thái "suspended" cho tới khi trang
  // có tương tác. resume() hỏng thì nuốt lỗi: mất tiếng chứ không được làm
  // hỏng cả luồng nhận cuộc gọi.
  void audio.resume().catch(() => {});

  const beep = (at: number, freq: number) => {
    const osc = audio.createOscillator();
    const gain = audio.createGain();

    osc.type = "sine";
    osc.frequency.value = freq;

    // Vào và ra bằng đường dốc chứ không bật/tắt đột ngột: cắt sóng giữa
    // chừng tạo ra tiếng "cụp" nghe rất chói.
    gain.gain.setValueAtTime(0, at);
    gain.gain.linearRampToValueAtTime(0.18, at + 0.02);
    gain.gain.setValueAtTime(0.18, at + BEEP_MS / 1000 - 0.05);
    gain.gain.linearRampToValueAtTime(0, at + BEEP_MS / 1000);

    osc.connect(gain).connect(audio.destination);
    osc.start(at);
    osc.stop(at + BEEP_MS / 1000);
  };

  const cycle = () => {
    if (stopped) return;
    const now = audio.currentTime;
    beep(now, 440);
    beep(now + (BEEP_MS + GAP_MS) / 1000, 554);
  };

  cycle();
  timer = setInterval(cycle, CYCLE_MS);

  return {
    stop: () => {
      stopped = true;
      if (timer) clearInterval(timer);
      void audio.close().catch(() => {});
    },
  };
}
