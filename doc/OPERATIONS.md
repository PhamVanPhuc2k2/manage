# Vận hành

Tài liệu tham chiếu: danh sách service, biến môi trường, lệnh thường dùng, TLS
và quy trình sao lưu — khôi phục.

Đang có sự cố thì xem [RUNBOOK.md](RUNBOOK.md) — tài liệu này là tham chiếu,
runbook là hướng dẫn xử lý.

## Danh sách service

### Stack chính — `docker-compose.yml`

| Service | Image | Cổng | Vai trò |
|---|---|---|---|
| `nginx` | nginx:1.27-alpine | 80, 443 | Container **duy nhất** mở cổng ra ngoài. TLS, giới hạn tốc độ, nâng cấp WebSocket |
| `api` | tự build | 8080 (nội bộ) | REST + WebSocket. Scale được nhiều replica |
| `worker` | tự build | 8080 (nội bộ) | Job nền qua RabbitMQ + job định kỳ. Cổng 8080 chỉ phục vụ `/metrics` và `/health` |
| `frontend` | tự build | 3000 (nội bộ) | Next.js |
| `postgres` | postgres:16-alpine | 5432 (nội bộ) | Dữ liệu |
| `redis` | redis:7-alpine | 6379 (nội bộ) | Phiên đăng nhập, presence, giới hạn tốc độ |
| `rabbitmq` | rabbitmq:3.13-management-alpine | 5672, 15672, 15692 (nội bộ) | Hàng đợi job + fan-out realtime |
| `migrate` | migrate/migrate:v4.18.1 | — | Chạy một lần rồi thoát |
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
docker compose up -d --scale api=3 --scale worker=4

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
| _chưa diễn tập lần nào_ | | | | |

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
