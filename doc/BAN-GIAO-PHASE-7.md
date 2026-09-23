# Bàn giao Phase 7 — Gọi thoại & gọi video

Ghi lại đúng trạng thái lúc dừng, để lần sau mở máy lên là biết đang ở đâu.

Chi tiết từng hạng mục nằm trong [TASKS.md](TASKS.md); phần vận hành nằm trong
[OPERATIONS.md](OPERATIONS.md); phần cho người dùng cuối nằm trong
[HUONG-DAN-SU-DUNG.md](HUONG-DAN-SU-DUNG.md). Tệp này chỉ trả lời ba câu:
**chạy được đến đâu, còn gì, và cần chú ý gì.**

---

## Chạy được đến đâu

Gọi 1-1 và gọi nhóm **chạy thật, đã nghiệm thu trên stack Docker**: bấm gọi,
đầu kia đổ chuông có tiếng, bắt máy, thấy hình và nghe tiếng nhau, chia sẻ màn
hình, cúp máy, và khung chat có dòng "Cuộc gọi video · 12 phút".

| Đo trên máy thật | Kết quả |
|---|---|
| 7 bộ smoke (thêm `smoke-call.sh` 37 phép) | **323/323** |
| `cmd/callsignal` — đường signaling qua WebSocket | **14/14** |
| `cmd/callsignal` khi `--scale api=3` | **14/14**, hai người ở hai instance khác nhau |
| Playwright, hai trình duyệt thật | **10/10** |
| Unit test `usecase/call` | 81.7% |
| Unit test `repository/media` | 92.7% |
| Integration test trên PostgreSQL thật | 14 phép cho module gọi |

Nhánh `phase-7-goi-video`, 14 commit, đã đẩy lên GitHub. **Chưa merge vào
`main`** — `main` vẫn đang ở Phase 6.

---

## Còn gì

### Cần người thật, không dựng được trên một máy

Năm mục nghiệm thu này vẫn để trống trong TASKS.md, và cố ý để trống:

- gọi giữa hai máy khác mạng (một WiFi, một 4G)
- gọi nhóm 6 người
- chiếu màn hình giữa lúc đang gọi, kiểm chữ có đọc rõ không
- gọi từ **mạng công ty chặn UDP**
- đổi WiFi sang 4G giữa cuộc gọi

> Mục thứ tư là mục rủi ro nhất. Nếu mạng công ty chặn UDP, đường dự phòng
> hiện tại là **RTC qua TCP cổng 7881**. Nó chạy được nhưng chậm hơn hẳn.
> Nên thử sớm, trước khi mở cho cả công ty.

### Chặn bởi khối HTTPS

**`getUserMedia` chỉ chạy trong secure context.** `localhost` được miễn nên
máy cá nhân chạy được, nhưng **mở qua IP LAN là hỏng ngay** — không xin được
micro. Kéo phần TLS của Phase 6 lên làm trước khi cho người khác dùng thử.

Đi kèm: `LIVEKIT_PUBLIC_URL` phải đổi sang `wss://`, và listener TURN trên
TCP cổng 443 cũng cần chứng chỉ thật.

### Cố ý chưa làm, có lý do đã ghi

| Mục | Vì sao |
|---|---|
| Gọi 1-1 đi P2P | Làm P2P là viết thêm một đường media THỨ HAI, tự lo bốn việc mà SFU đã làm sẵn và đã chạy đúng. Làm khi chỉ số băng thông thật sự thành vấn đề |
| coturn riêng | coturn cần một dải cổng UDP relay — đúng thứ không map được trong Docker. Mã sinh credential HMAC vẫn giữ sẵn, điền hai biến là dùng được |
| Ghi hình cuộc gọi | Kéo theo container Egress, khoá R2, chính sách lưu trữ, màn hình xin đồng ý và audit log — một khối riêng |
| Tỉ lệ relay qua TURN | LiveKit v1.13 không báo kiểu kết nối của từng người, qua webhook lẫn qua API |

### Làm dở, nhỏ

- **Bảng chọn thiết bị** (`DevicePanel.tsx`) đã viết xong, qua `tsc` và
  `eslint`, nhưng **chưa mở ra trong trình duyệt thật lần nào**. Đây là thứ
  đầu tiên nên kiểm khi quay lại.
- "Mỗi lúc chỉ một người chiếu màn hình" — chưa làm.
- "Chỉ hiện người đang nói ở độ nét cao" — hiện đang dựa vào `adaptiveStream`
  và `dynacast` của LiveKit, chưa tự điều khiển.

---

## Cần chú ý

### Mật khẩu quản trị hiện tại

`admin@abc.vn` / `E2eSmoke#1790151133`

Database dev đã bị dựng lại từ đầu bằng `scripts/ci-e2e.sh` (lệnh đó có
`docker compose down -v`). Mọi mật khẩu cũ không còn dùng được.

### Khoá LiveKit

`LIVEKIT_API_KEY=manage` là **định danh**, không phải bí mật — nó nằm trong
trường `iss` của mọi access token gửi xuống trình duyệt, và nó được ghi thẳng
vào `docker/livekit/livekit.yaml`. Thứ phải giữ kín là `LIVEKIT_API_SECRET`
trong `.env`.

Đổi khoá thì phải **dựng lại cả `livekit` lẫn `api`**, không chỉ restart:

```bash
docker compose up -d --force-recreate livekit api worker
```

### Bốn cổng phải mở trên firewall

Media **không đi qua nginx**. Ngoài 80/443 còn cần:

| Cổng | Dùng khi nào |
|---|---|
| 7880/tcp | Signaling của LiveKit |
| 7882/**udp** | Toàn bộ media. Đường chính |
| 7881/tcp | Media khi mạng chặn UDP |
| 3478/udp | TURN tích hợp |

Nhiều nhà cung cấp chặn UDP mặc định. Thiếu 7882/udp thì cuộc gọi vẫn "kết
nối được" — nó rơi về TCP — nhưng tiếng sẽ giật và không ai hiểu vì sao.

### Nếu ba cờ `had_audio/had_video/had_screen` đều false

Đó **không** phải vấn đề của dấu vết audit mà là dấu hiệu trình duyệt không
publish được luồng nào — cuộc gọi "nối được" nhưng không có tiếng lẫn hình,
trong khi giao diện trông hoàn toàn bình thường. Đây chính là lỗi nặng nhất
tìm ra trong phase này.

```bash
docker compose exec postgres psql -U manage -d manage -c \
  "SELECT had_audio, had_video, had_screen FROM call_participants
   ORDER BY created_at DESC LIMIT 4;"
```

### HMR của frontend không thấy thay đổi từ host

Sửa mã frontend mà giao diện không đổi thì **khởi động lại container**, đừng
đi tìm lỗi trong mã:

```bash
docker compose restart frontend
```

Đã mất khá nhiều thời gian vì điều này: mã đã sửa đúng nhưng trình duyệt vẫn
chạy bản cũ, và triệu chứng giống hệt một lỗi chưa được sửa.

### Bộ E2E chạm trần giới hạn đăng nhập

nginx cho 30 lượt/phút ở nhóm endpoint đăng nhập, mỗi lần đăng nhập tốn hai
lượt. Chạy cả bộ Playwright hai ba lần liền nhau là chạm trần; helper sẽ chờ
hơn một phút rồi thử lại. **Chờ một phút giữa hai lần chạy là xong** — đừng
nới giới hạn cho dễ test, đó là lớp chặn dò mật khẩu.

---

## Lệnh hay dùng

```bash
# Dừng stack (dữ liệu vẫn còn)
docker compose stop

# Chạy lại
docker compose up -d

# Bộ smoke cho module gọi
ADMIN_PASS='E2eSmoke#1790151133' bash scripts/smoke-call.sh

# Đường signaling qua WebSocket
cd backend && go run ./cmd/callsignal \
  -url ws://localhost/ws -api http://localhost/api/v1 \
  -caller <token_A> -callee <token_B> -conversation <id>

# Bộ trình duyệt
cd frontend && E2E_ADMIN_PASSWORD='E2eSmoke#1790151133' pnpm e2e
```

---

## Việc tiếp theo, theo thứ tự đề xuất

1. **Mở bảng chọn thiết bị trong trình duyệt** và sửa nốt nếu có gì lệch.
2. **Merge nhánh vào `main`** — phần lõi đã nghiệm thu đầy đủ.
3. **Làm khối HTTPS của Phase 6.** Nó chặn cả việc thử trong mạng LAN lẫn
   listener TURN trên 443, tức là chặn luôn năm mục nghiệm thu còn lại.
4. Thử trên **mạng công ty thật** — mục rủi ro nhất của cả phase.
