# Vận hành

Tài liệu tham chiếu: danh sách service, biến môi trường, lệnh thường dùng, TLS
và quy trình sao lưu — khôi phục.

Đang có sự cố thì xem [RUNBOOK.md](RUNBOOK.md) — tài liệu này là tham chiếu,
runbook là hướng dẫn xử lý.

## Danh sách service

### Stack chính — `docker-compose.yml`

| Service | Image | Cổng | Vai trò |
|---|---|---|---|
| `nginx` | nginx:1.27-alpine | 80, 443 | Cửa ra vào duy nhất của **lưu lượng HTTP**. TLS, giới hạn tốc độ, nâng cấp WebSocket. Media KHÔNG đi qua đây |
| `api` | tự build | 8080 (nội bộ) | REST + WebSocket. Scale được nhiều replica |
| `worker` | tự build | 8080 (nội bộ) | Job nền qua RabbitMQ + job định kỳ. Cổng 8080 chỉ phục vụ `/metrics` và `/health` |
| `frontend` | tự build | 3000 (nội bộ) | Next.js |
| `postgres` | postgres:16-alpine | 5432 (nội bộ) | Dữ liệu |
| `redis` | redis:7-alpine | 6379 (nội bộ) | Phiên đăng nhập, presence, giới hạn tốc độ |
| `rabbitmq` | rabbitmq:3.13-management-alpine | 5672, 15672, 15692 (nội bộ) | Hàng đợi job + fan-out realtime |
| `migrate` | migrate/migrate:v4.18.1 | — | Chạy một lần rồi thoát |
| `livekit` | livekit/livekit-server:v1.13.7 | 7880, 7881, 7882/udp, 3478/udp | Máy chủ media cho gọi thoại/video. **Mở cổng ra ngoài** — xem mục riêng bên dưới |
| `mailhog` | mailhog/mailhog | 8025 | **Chỉ dev.** Hộp thư giả |
| `adminer` | adminer | 8081 | **Chỉ dev.** Giao diện database |

### Giám sát — `docker-compose.monitoring.yml`

Bật thêm khi cần. Sáu container này chiếm khoảng 1GB RAM nên không nhập vào
stack chính.

| Service | Cổng | Vai trò |
|---|---|---|
| `grafana` | 3001 | Container giám sát **duy nhất** mở cổng ra host |
| `prometheus` | 9090 (nội bộ) | Thu chỉ số, tính cảnh báo |
| `loki` | 3100 (nội bộ) | Lưu log |
| `promtail` | — | Gom log container đẩy sang Loki |
| `cadvisor` | 8080 (nội bộ) | Chỉ số CPU/RAM của từng container |
| `node-exporter` | 9100 (nội bộ) | Chỉ số của máy: ổ đĩa, RAM, CPU |

### Production — `docker-compose.prod.yml`

Thêm hai service và siết cấu hình mọi service còn lại.

| Service | Vai trò |
|---|---|
| `certbot` | Gia hạn chứng chỉ Let's Encrypt, vòng lặp 12 giờ |
| `backup` | `pg_dump` hằng ngày, tự kiểm tra bản dump đọc được |

Khác biệt so với dev: image kéo từ GHCR thay vì build tại chỗ, không service
nào ngoài nginx mở cổng, `read_only: true` + `cap_drop: ALL` +
`no-new-privileges` cho api và worker, hạn mức CPU/RAM cho mọi service, và
log có `max-size`/`max-file` để không làm đầy ổ đĩa.

## Mạng

```
                    internet
                       │
                    :80/:443
                       ▼
   ┌─────────── proxy ─────────────┐
   │        nginx  ·  frontend      │
   │              api               │
   └───────────────┬────────────────┘
                   │
   ┌─────────── backend ───────────────────────────┐
   │  api · worker · postgres · redis · rabbitmq   │
   │              prometheus                        │
   └───────────────┬────────────────────────────────┘
                   │
   ┌────────── monitoring ─────────────────────────┐
   │ prometheus · grafana · loki · promtail        │
   │       cadvisor · node-exporter                │
   └───────────────────────────────────────────────┘
```

`postgres`, `redis` và `rabbitmq` **chỉ** ở mạng `backend` — không container
nào ở mạng `proxy` gọi tới chúng được.

`prometheus` ở cả `backend` và `monitoring`: nó cần scrape api/worker
(backend) và cần Grafana gọi tới (monitoring). Thiếu `backend` thì mọi target
báo "connection refused" trong khi cấu hình trông vẫn đúng.

## Biến môi trường

Toàn bộ nằm trong `.env`, mẫu ở `.env.example`. **Không commit `.env`.**

### Bắt buộc đổi trước khi lên production

| Biến | Ghi chú |
|---|---|
| `JWT_SECRET` | Tối thiểu 32 ký tự. Ứng dụng **từ chối khởi động** ở chế độ production nếu còn giá trị mặc định |
| `POSTGRES_PASSWORD` | |
| `REDIS_PASSWORD` | |
| `RABBITMQ_PASSWORD` | |
| `GRAFANA_PASSWORD` | Cửa duy nhất vào dữ liệu vận hành, gồm cả log |
| `PUBLIC_BASE_URL` | Phải khớp domain thật, nếu không liên kết trong email sẽ sai |

### Hay phải điều chỉnh

| Biến | Mặc định | Ghi chú |
|---|---|---|
| `NGINX_PORT` | 80 | Đổi nếu cổng bị chiếm; phải sửa `PUBLIC_BASE_URL` cho khớp |
| `WORKER_CONCURRENCY` | 5 | Số goroutine xử lý job mỗi worker |
| `AUTH_OTP_ENABLED` | true | Tắt nghĩa là ai có mật khẩu là vào được |
| `POSTGRES_MAX_CONN` | 20 | Mỗi replica api dùng tối đa số này; nhân với số replica phải nhỏ hơn `max_connections` của PostgreSQL |
| `BACKUP_KEEP_DAYS` | 30 | Số ngày giữ bản backup |
| `R2_*` | trống | Để trống thì mọi chức năng tệp báo lỗi rõ ràng, phần còn lại chạy bình thường |

## Lệnh thường dùng

```bash
# --- Dev ---
make init                 # dựng lần đầu
make up / make down
make logs
make smoke                # nghiệm thu hạ tầng
docker compose ps

# Windows PowerShell, không cần cài make
.\dev.ps1 init
.\dev.ps1 up

# --- Bật thêm giám sát ---
#
# PHẢI liệt kê cả docker-compose.override.yml. Khi có cờ -f tường minh,
# Docker KHÔNG tự nạp trật tự override nữa — thiếu nó thì api mất cấu hình
# dev (air, hot reload) và chết với "open .air.api.toml: no such file".
docker compose -f docker-compose.yml -f docker-compose.override.yml   -f docker-compose.monitoring.yml up -d
# Grafana: http://localhost:3001

# Trên máy chủ thật thì không có override, nên bỏ nó đi:
docker compose -f docker-compose.yml -f docker-compose.prod.yml   -f docker-compose.monitoring.yml up -d

# --- Production ---
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
IMAGE_TAG=<git-sha> bash scripts/deploy.sh
IMAGE_TAG=<git-sha-cũ> bash scripts/rollback.sh

# --- Scale ---
#
# Trên máy dev phải thêm docker-compose.scale.yml: override công bố cổng cố
# định 8080:8080, mà một cổng chỉ gắn được cho một container — không có tệp
# đó thì bản thứ hai chết với "Bind for 0.0.0.0:8080 failed: port is already
# allocated". Bỏ cổng đi thì gọi api qua nginx ở http://localhost:8088.
docker compose -f docker-compose.yml -f docker-compose.override.yml \
  -f docker-compose.scale.yml up -d --scale api=3 --scale worker=2

# Trên máy chủ thật không có override nên không cần tệp scale:
docker compose -f docker-compose.yml -f docker-compose.prod.yml \
  up -d --scale api=3 --scale worker=4

# BẮT BUỘC sau MỌI lần scale: nạp lại nginx.
#
# nginx phân giải tên `api` đúng một lần lúc nạp cấu hình và giữ danh sách IP
# đó mãi. Không nạp lại thì các bản vừa thêm không nhận được request nào, mà
# cũng chẳng có lỗi nào báo ra — chỉ là tiền mua máy không đến đâu.
docker compose exec nginx nginx -s reload

# Kiểm chứng lưu lượng chia đều thật (request_id mang tên máy của container):
for i in $(seq 1 30); do
  curl -s http://localhost:8088/api/v1/ping \
    | grep -o '"request_id":"[^"]*"' | cut -d'"' -f4 | cut -d/ -f1
done | sort | uniq -c

# --- Migration ---
docker compose run --rm migrate                                    # tiến
docker compose run --rm migrate -path /migrations \
  -database "$POSTGRES_DSN" down 1                                 # lùi 1 bước

# --- Sao lưu ---
bash scripts/backup.sh
bash scripts/restore.sh --list
bash scripts/restore.sh <tên-file>

# --- Kiểm chứng nghiệp vụ (287 mục) ---
for s in auth hr project attendance payroll chat; do
  ADMIN_PASS='...' bash scripts/smoke-$s.sh
done
```

## Máy chủ media (LiveKit)

### Vì sao media không đi qua nginx

nginx là proxy HTTP. Đẩy âm thanh và hình ảnh thời gian thực qua nó là thêm
một chặng buffer vào đường mà mọi mili giây đều nghe thấy. Trình duyệt nối
THẲNG tới LiveKit bằng địa chỉ `media_url` mà API trả về trong lời gọi mở
cuộc gọi.

Hệ quả vận hành: **bốn cổng dưới đây phải mở trên firewall**, không chỉ
443 như phần còn lại của hệ thống.

| Cổng | Giao thức | Dùng khi nào |
|---|---|---|
| 7880 | TCP | Signaling của LiveKit (WebSocket). Bắt buộc |
| 7882 | **UDP** | Toàn bộ media, gồm chung một cổng (UDP mux). Đường chính |
| 7881 | TCP | Media khi mạng chặn UDP. Chậm hơn hẳn nhưng vẫn gọi được |
| 3478 | UDP | TURN tích hợp |

> **Nhiều nhà cung cấp chặn UDP mặc định.** Không mở 7882/udp thì cuộc gọi
> vẫn "kết nối được" — nó rơi về TCP 7881 — nhưng tiếng sẽ giật và không ai
> hiểu vì sao. Kiểm cổng UDP trước khi kết luận là lỗi ứng dụng.

### MỘT cổng UDP, không phải một dải

Hướng dẫn WebRTC thông thường bảo map dải `50000-60000/udp`. **Đừng làm thế
trong Docker.** Docker sinh một tiến trình `docker-proxy` cho MỖI cổng được
publish: mười nghìn tiến trình, máy dev treo và máy chủ khởi động hàng phút.

LiveKit gom toàn bộ media về một cổng UDP duy nhất và tách luồng bằng thông
tin trong gói tin. Cấu hình ở `docker/livekit/livekit.yaml`, khoá `rtc.udp_port`.

Cũng **không dùng `network_mode: host`**: mất cách ly mạng, và không chạy
được trên Docker Desktop Windows — tức là hỏng luôn môi trường dev của cả
đội.

### Khoá API

Hai biến trong `.env`:

```
LIVEKIT_API_KEY=APIdev...
LIVEKIT_API_SECRET=...
```

Chúng đi vào container bằng biến môi trường `LIVEKIT_KEYS`, **không** nằm
trong `docker/livekit/livekit.yaml` — tệp đó nằm trong kho mã.

**Không dùng cặp mẫu `devkey/secret`** mà tài liệu LiveKit hay dẫn: ai cũng
biết nó, và biết nó là tự ký được token vào MỌI phòng họp. Sinh bộ mới:

```bash
openssl rand -hex 32
```

Đổi khoá thì phải khởi động lại **cả livekit lẫn api**: api ký token bằng
khoá cũ sẽ bị LiveKit từ chối.

```bash
docker compose up -d --force-recreate livekit api worker
```

### Chạy sau NAT (VPS, cloud)

Mặc định `rtc.use_external_ip: false` — đúng cho dev, nơi mọi thứ là localhost.

Trên VPS phải **bật lên**, nếu không LiveKit quảng bá IP nội bộ của container
(`172.x.x.x`) cho trình duyệt, và không ai kết nối được.

### HTTPS là BẮT BUỘC

`getUserMedia` và `getDisplayMedia` chỉ chạy trong **secure context**. `localhost`
được miễn, nên dev trên máy cá nhân chạy được. Nhưng:

- thử qua IP LAN (`http://192.168.1.10`) là **hỏng ngay**, không xin được mic;
- khi đó `LIVEKIT_PUBLIC_URL` cũng phải là `wss://`, không phải `ws://` —
  trang https không mở được WebSocket không mã hoá.

Triệu chứng khi thiếu HTTPS: giao diện báo "Không gọi được trên trình duyệt
này" — câu đó có chủ đích, xem `frontend/src/features/call/support.ts`.

### Kiểm tra nhanh

```bash
# LiveKit đã sẵn sàng chưa
docker compose exec livekit wget -q -O- http://127.0.0.1:7880/

# Chỉ số vận hành (cổng riêng, không publish ra host)
docker compose exec livekit wget -q -O- http://127.0.0.1:6789/metrics | grep ^livekit_
```

Ba chỉ số đáng theo dõi nhất:

| Chỉ số | Ý nghĩa |
|---|---|
| `livekit_room_total` | Số phòng đang mở. Tăng dần mà không giảm = phòng rác không được dọn |
| `livekit_participant_total` | Số người đang trong cuộc gọi. Quyết định chi phí băng thông |
| `livekit_node_packet_total{type="dropped"}` | Gói tin bị bỏ. Tăng nhanh = máy chủ quá tải hoặc đường truyền nghẽen |

Prometheus đã có sẵn job `livekit` (xem `docker/monitoring/prometheus/prometheus.yml`).

### Giới hạn đã đặt

| Giới hạn | Giá trị | Ở đâu |
|---|---|---|
| Số người mỗi phòng | 16 | `docker/livekit/livekit.yaml`, `room.max_participants` |
| Tự đóng phòng rỗng | 60 giây | `room.empty_timeout` |
| CPU / RAM của SFU | 2 CPU, 1 GB | `docker-compose.yml`, `deploy.resources.limits` |
| Bitrate luồng màn hình | 1.5 Mbps, 15 khung/giây | `frontend/src/features/call/CallScreen.tsx` |
| Thời gian đổ chuông | 45 giây | `backend/internal/domain/call/entity.go` |
| Hạn token vào phòng | 10 phút | cùng tệp |

**Số người mỗi phòng là giới hạn quan trọng nhất.** Băng thông gửi đi của
máy chủ tăng theo bình phương số người. Xem bảng ước lượng trong
`doc/TASKS.md` trước khi nâng.

### Webhook từ LiveKit

LiveKit đẩy sự kiện về `http://api:8080/webhooks/livekit` — **tên service trong
mạng Docker, không qua nginx**. Đường này không mở ra Internet.

Xác thực bằng chính cặp khoá API, hai bước:

1. Chữ ký JWT trong header `Authorization` phải ký được bằng `LIVEKIT_API_SECRET`.
2. sha256 của **thân bản tin** phải khớp claim trong chính token đó.

Thiếu bước hai thì chặn giữa đường một bản tin hợp lệ rồi đổi nội dung là ghi
được bất cứ gì vào dấu vết audit của cuộc họp.

> **`webhook.api_key` phải nằm cùng tệp cấu hình với `keys`.** LiveKit kiểm
> điều đó lúc khởi động và **không nhìn tới khoá đến từ biến môi trường**;
> thiếu thì nó từ chối chạy với đúng một dòng "api_key is required to use
> webhooks". Vì vậy container ghép bí mật vào một bản sao trong `/tmp` lúc
> khởi động — tệp trong kho mã vẫn sạch bí mật. Xem `docker-compose.yml`.

### Dấu vết luồng media

Ba cờ `had_audio` / `had_video` / `had_screen` trong `call_participants` trả lời
câu hỏi audit: *"trong cuộc họp đó người này có chiếu màn hình không"*.

Chúng được ghi từ thứ **SFU thấy**, không phải từ lời khai của trình duyệt:
backend hỏi `ListParticipants` đúng một lần, ngay trước khi đóng phòng.

Không dùng webhook `track_published`: LiveKit v1.13 không phát sự kiện đó nữa
— đã kiểm chứng bằng cách bật webhook và đếm sự kiện thật.

**Nếu ba cờ này đều false sau một cuộc gọi bình thường**, đó không phải vấn đề
của audit mà là dấu hiệu trình duyệt **không publish được luồng nào** — cuộc
gọi "nối được" nhưng không có tiếng lẫn hình. Kiểm nhanh:

```bash
docker compose exec postgres psql -U manage -d manage -c \
  "SELECT had_audio, had_video, had_screen FROM call_participants
   ORDER BY created_at DESC LIMIT 4;"
```

### Chưa làm

- **Tỉ lệ relay qua TURN** (`relay_ratio`). LiveKit không báo kiểu kết nối của
  từng người, qua webhook lẫn qua API danh sách người tham gia.
- **TURN trên TCP cổng 443.** Đường dự phòng hiện tại là RTC qua TCP 7881.
  Cổng 443 cần chứng chỉ thật nên đi cùng khối HTTPS.
- **Nhiều instance LiveKit.** Cấu hình hiện tại không dùng Redis (lý do ghi
  trong chính tệp cấu hình), nên **chỉ chạy ĐÚNG MỘT instance**.

## Scale

`api` và `worker` đều scale ngang được. Ba điều làm được việc đó:

1. **api không giữ trạng thái nào trong bộ nhớ** mà request sau cần. Phiên
   đăng nhập ở Redis, presence ở Redis.
2. **Bản tin realtime đi qua exchange fanout của RabbitMQ.** Mỗi instance có
   một queue riêng, tự biến mất khi instance ngắt. Nhờ vậy người nhận nối vào
   instance nào cũng nhận được tin phát từ instance khác.
3. **Job định kỳ chạy ở worker, không ở api.** api nhiều replica sẽ khiến mỗi
   replica chạy job một lần và tạo dữ liệu trùng.

```bash
docker compose up -d --scale api=3
docker compose ps api

# Kiểm chứng fan-out: số kết nối phải phân bố trên cả 3 instance
curl -s http://localhost/metrics | grep manage_ws_connections
```

Hai lưu ý khi scale worker:

- Job định kỳ sẽ chạy trên **mọi** replica worker. Mọi job hiện có đều viết
  sao cho chạy lại vô hại (`Upsert`, `ReplaceForPeriod`), nên điều đó chỉ tốn
  công chứ không sai dữ liệu. Thêm job mới thì phải giữ tính chất đó.
- `POSTGRES_MAX_CONN` nhân với tổng số replica phải nhỏ hơn `max_connections`
  của PostgreSQL (mặc định 100).

## TLS

### Lần cấp chứng chỉ đầu tiên

Phải chạy tay một lần. Nó cần domain đã trỏ DNS về máy chủ — việc không tự
động hoá được.

```bash
# 1. DNS đã trỏ đúng chưa
dig +short $DOMAIN

# 2. nginx phải đang chạy và phục vụ được /.well-known/acme-challenge/
mkdir -p docker/nginx/acme docker/nginx/certs
docker compose up -d nginx

# 3. Xin chứng chỉ
docker compose -f docker-compose.yml -f docker-compose.prod.yml run --rm certbot \
  certonly --webroot -w /var/www/certbot \
    -d "$DOMAIN" --email "$ADMIN_EMAIL" --agree-tos --no-eff-email

# 4. Bỏ ghi chú khối server 443 trong docker/nginx/conf.d/app.conf, rồi
docker compose exec nginx nginx -t && docker compose exec nginx nginx -s reload
```

### Gia hạn

Tự động, không phải làm gì. Container `certbot` gọi `certbot renew` mỗi 12
giờ; Let's Encrypt chỉ gia hạn khi chứng chỉ còn dưới 30 ngày.

nginx **tự nạp lại** cấu hình mỗi 6 giờ để dùng chứng chỉ mới. Việc nạp lại
do chính nginx làm, không phải certbot gọi sang: image certbot không có Docker
CLI, nên cách "certbot gọi `docker kill -s HUP`" hay thấy trên mạng sẽ thất
bại trong im lặng và chứng chỉ mới nằm trên đĩa trong khi nginx phục vụ bản cũ
cho tới lúc nó hết hạn.

```bash
# Kiểm tra ngày hết hạn
docker compose exec nginx openssl x509 -enddate -noout \
  -in /etc/nginx/certs/live/$DOMAIN/fullchain.pem

# Thử gia hạn mà không thay đổi gì
docker compose run --rm certbot renew --dry-run
```

## Sao lưu

Container `backup` chạy `pg_dump` mỗi 24 giờ vào `./backups`, và
**tự kiểm tra bản dump đọc được** bằng `pg_restore --list` ngay sau khi tạo.

Bước kiểm tra đó không phải cho đủ thủ tục: `pg_dump` có thể thoát với mã 0
mà vẫn cho file hỏng nếu kết nối đứt giữa chừng. Một bản backup hỏng còn tệ
hơn không có backup — nó tạo cảm giác an toàn sai, và ta chỉ biết vào đúng lúc
cần khôi phục.

`scripts/backup.sh` làm thêm việc đẩy lên Cloudflare R2. Đó là phần quan
trọng: backup nằm **cùng máy** với database không bảo vệ được trước hỏng ổ đĩa
hay mất máy chủ — đúng hai tình huống người ta cần backup nhất.

Định dạng là `pg_dump -Fc` (custom) chứ không phải SQL thuần, vì:

- nén sẵn, nhỏ hơn khoảng 5 lần
- `pg_restore` phục hồi **song song** được (`-j 4`)
- chọn được bảng cụ thể lúc khôi phục

## Diễn tập khôi phục

Một bản backup chưa bao giờ được khôi phục thử thì chưa phải bản backup. Nên
diễn tập **mỗi quý** và trên **máy sạch**, không phải trên máy production.

```bash
# 1. Máy sạch, chỉ cài Docker
git clone <repo> && cd manage
cp .env.example .env    # điền mật khẩu

# 2. Dựng hạ tầng, CHƯA chạy api/worker
docker compose up -d postgres redis rabbitmq
docker compose run --rm migrate

# 3. Lấy bản backup mới nhất từ R2
bash scripts/restore.sh --list
bash scripts/restore.sh <tên-file>

# 4. Bấm đồng hồ từ bước 1 tới lúc /ready xanh
docker compose up -d
time curl -fsS http://localhost/ready

# 5. Kiểm chứng dữ liệu thật sự về, không chỉ là schema rỗng
docker compose exec postgres psql -U manage -d manage -c "
SELECT
  (SELECT COUNT(*) FROM employees WHERE deleted_at IS NULL) AS nhan_vien,
  (SELECT COUNT(*) FROM projects  WHERE deleted_at IS NULL) AS du_an,
  (SELECT COUNT(*) FROM messages)                           AS tin_nhan,
  (SELECT COUNT(*) FROM payslips)                           AS phieu_luong;"
```

**Ghi lại thời gian thực tế** của mỗi lần diễn tập vào bảng dưới. Con số đó
là RTO thật của hệ thống — thứ duy nhất trả lời được câu "mất máy chủ thì bao
lâu chạy lại được", và nó chỉ có giá trị nếu đo bằng đồng hồ thật.

| Ngày diễn tập | Người thực hiện | Dung lượng backup | Thời gian khôi phục | Ghi chú |
|---|---|---|---|---|
| 2026-09-22 | máy dev | 152 KB (39 bảng) | **37 giây** | Lần đầu. Tìm ra 3 lỗi chặn đường — xem bên dưới |

> **Lần diễn tập đầu tiên phát hiện đường sao lưu — khôi phục chưa bao giờ
> chạy được.** Cả ba lỗi đều nằm trong kịch bản chứ không trong dữ liệu, và
> không lỗi nào lộ ra nếu chỉ đọc mã:
>
> 1. `pg_restore --list /dev/stdin` — đưa MỘT TÊN TỆP vào thì pg_restore cần
>    nhảy đểc để đọc mục lục, mà `/dev/stdin` là một ống. Nó báo "did not
>    find magic string in file header", nghe y hệt tệp hỏng. `backup.sh` dừng
>    ngay tại bước tự kiểm tra này, nên **chưa từng tạo ra một bản backup
>    nào**; `restore.sh` dừng tại bước kiểm tra tương ứng.
> 2. `pg_restore -j 4` từ chuỗi chuẩn vào — "parallel restore from standard
>    input is not supported". Phục hồi song song cần nhảy đểc trong tệp.
> 3. `/ready` không được nginx định tuyến nên rơi sang frontend và trả 404.
>    Cả `deploy.sh` lẫn `restore.sh` đều kiểm đường này ở bước cuối, nên cả
>    hai báo thất bại trong khi hệ thống hoàn toàn bình thường.
>
> Đây chính là lý do mục này mở đầu bằng câu "một bản backup chưa bao giờ
> được khôi phục thử thì chưa phải bản backup".
>
> **Phân rã 37 giây** (152 KB, 39 bảng, trên máy dev có sẵn image):
>
> | Bước | Thời gian |
> |---|---|
> | Kiểm tra bản dump đọc được | ~1 giây |
> | Dừng api và worker | ~3 giây |
> | `pg_restore -j 4` | ~14 giây |
> | Chạy migration | ~5 giây |
> | Khởi động lại api, worker, nạp lại nginx | ~14 giây |
>
> Con số này **không phải RTO thật**. Nó đo trên máy đã có sẵn image và dữ
> liệu nhỏ. RTO thật còn phải cộng thời gian dựng máy mới, kéo image, tải
> backup từ R2, và phục hồi một database lớn hơn nhiều lần. Lần diễn tập
> trên **máy sạch** theo đúng quy trình ở trên mới cho con số dùng được.
>
> Kiểm chứng dữ liệu: khôi phục vào một database riêng rồi đối chiếu số
> dòng với bản gốc — **39/39 bảng khớp**, giống nhau ở employees, projects,
> tasks, messages, conversations, payslips, attendance_days, leave_requests.

## Kiểm tra tải WebSocket

```bash
# Lấy access token: đăng nhập rồi copy access_token từ phản hồi
cd backend
go run ./cmd/wsload -url ws://localhost:8080/ws -token "$TOKEN" -n 500
```

Công cụ mở N kết nối **dần** trong 10 giây chứ không cùng một lúc: mở 500 kết
nối trong một mili giây không giống bất kỳ tải thật nào, và nó đo khả năng
chịu đột biến của accept queue chứ không phải khả năng phục vụ.

Nó gửi nhịp tim như client thật và **đọc** bản tin trả về — không đọc thì hàng
đợi gửi phía server đầy lên và server đóng kết nối, ta sẽ đo nhầm thành "server
không chịu nổi tải".

Mã thoát khác 0 khi trên 1% kết nối hỏng hoặc bị đóng giữa chừng, nên chạy
được trong script nghiệm thu.

> **Trước khi kết luận máy chủ không chịu nổi tải, kiểm tra `ulimit -n`.**
> Mỗi kết nối là một file descriptor, và giới hạn mặc định trên nhiều bản Linux
> là 1024. Rất dễ đo nhầm giới hạn của chính máy chạy công cụ đo.
>
> Chạy từ MỘT MÁY KHÁC nếu muốn con số có ý nghĩa: 500 kết nối từ chính máy
> chủ sẽ đo luôn cả tải của công cụ đo.

Đo trong lúc chạy để xem hệ thống phản ứng thế nào:

```bash
watch -n2 'curl -s http://localhost:8080/metrics | grep -E "manage_ws_connections|manage_ws_dropped"'
```

## Quét bảo mật image

```bash
# Quét image đã build
trivy image --severity HIGH,CRITICAL ghcr.io/phamvanphuc2k2/manage-api:latest

# Quét cả thư viện Go
trivy fs --scanners vuln,secret ./backend

# Quét cấu hình Docker và compose
trivy config .
```

Đã nối vào CI ở hai chỗ:

| Workflow | Khi nào | Làm gì |
|---|---|---|
| `.github/workflows/ci.yml` | mỗi lần build | Quét image vừa build, **chặn** khi có HIGH/CRITICAL |
| `.github/workflows/security.yml` | 08:00 thứ hai hằng tuần | Quét image đang chạy, `govulncheck`, Trivy fs + config, `pnpm audit`. Mở issue thay vì chặn |

Cần cả hai: image **không** thay đổi sau khi đẩy lên, còn CVE thì liên tục
được công bố. Một image sạch hôm nay có thể có lỗ hổng nghiêm trọng vào tuần
sau mà không ai chạm vào code — chỉ quét lúc build là bỏ sót toàn bộ nhóm đó.

Lần quét theo lịch **không chặn** mà mở issue: chặn một workflow theo lịch
chẳng ngăn được gì, code đã lên production từ lâu rồi. Nó cũng tìm issue cũ
trước khi mở mới, để không tích thành một chồng issue trùng nhau mà rồi không
ai đọc.

Base image đang dùng và lịch cập nhật:

| Image | Phiên bản | Cập nhật khi |
|---|---|---|
| `golang` | 1.27-alpine | Go phát hành bản vá bảo mật |
| `alpine` | 3.21 | Có CVE mức HIGH trở lên |
| `node` | 22-alpine | Node phát hành bản vá bảo mật |
| `postgres` | 16-alpine | Bản vá của nhánh 16 |
| `redis` | 7-alpine | Bản vá của nhánh 7 |
| `rabbitmq` | 3.13-management-alpine | Bản vá của nhánh 3.13 |
