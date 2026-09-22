# Runbook — xử lý sự cố

Tài liệu này dành cho người đang trực, lúc có gì đó hỏng. Mỗi mục bắt đầu
bằng **lệnh cần chạy**, không phải lý thuyết.

Mọi cảnh báo trong `docker/monitoring/prometheus/alerts.yml` đều trỏ tới một
mục ở đây. Nếu một cảnh báo không có mục tương ứng thì cảnh báo đó chưa dùng
được — người nhận lúc 3 giờ sáng sẽ không biết làm gì.

## Trước tiên

```bash
cd /đường/dẫn/tới/manage

# Cái gì đang chạy, cái gì không
docker compose ps

# Hệ thống có tự nhận là sẵn sàng không (kiểm tra cả 3 dependency)
curl -s http://localhost/ready | jq

# 100 dòng log cuối của mọi service
docker compose logs --tail 100
```

`/ready` phân biệt hai thứ rất khác nhau: container **còn sống** (`/health`)
và container **sẵn sàng phục vụ** (`/ready`). Database sập thì `/health` vẫn
xanh — đó là đúng, vì khởi động lại container không sửa được database.

---

## Container khởi động lại liên tục

**Cảnh báo:** `ContainerRestartLoop`

```bash
# Container nào, restart bao nhiêu lần
docker compose ps
docker inspect --format '{{.Name}} {{.RestartCount}} {{.State.Health.Status}}' \
  $(docker compose ps -q)

# Lý do chết: xem log TRƯỚC lần khởi động cuối
docker compose logs --tail 200 <service>

# Bị OOM-kill hay tự thoát?
docker inspect --format '{{.State.OOMKilled}} {{.State.ExitCode}}' \
  $(docker compose ps -q <service>)
```

`OOMKilled=true` → chạm hạn mức RAM. Nâng `deploy.resources.limits.memory`
trong `docker-compose.prod.yml`, hoặc tìm chỗ rò bộ nhớ trên dashboard
Grafana (bảng "RAM theo container").

`ExitCode=1` kèm log `đang khởi động` rồi dừng ngay → lỗi cấu hình. Hay gặp
nhất: thiếu biến trong `.env`, hoặc `JWT_SECRET` ngắn hơn 32 ký tự (ứng dụng
cố ý từ chối khởi động).

Không tìm ra thì dừng vòng lặp lại để đọc log bình tĩnh:

```bash
docker compose stop <service>
docker compose run --rm <service>   # chạy ở tiền cảnh, thấy hết lỗi
```

---

## Ổ đĩa gần đầy

**Cảnh báo:** `DiskSpaceLow` (80%), `DiskSpaceCritical` (92%)

PostgreSQL **ngừng ghi** khi hết đĩa. Đây là sự cố làm hệ thống dừng hẳn, không
chỉ chậm.

```bash
# Ai đang chiếm đĩa
df -h
docker system df -v | head -40
```

Theo thứ tự an toàn giảm dần:

```bash
# 1. Image và build cache không dùng — luôn an toàn
docker image prune -af
docker builder prune -af

# 2. Volume KHÔNG được gắn vào container nào.
#    ĐỌC danh sách trước khi xoá: nếu postgres_data lọt vào đây thì
#    nghĩa là container postgres đang không chạy, và xoá là mất database.
docker volume ls -f dangling=true
docker volume rm <tên-volume-cụ-thể>

# 3. Log container. Đáng lẽ đã bị giới hạn bởi max-size trong
#    docker-compose.prod.yml — nếu chúng to thì kiểm tra lại cấu hình đó.
du -sh /var/lib/docker/containers/*/*-json.log | sort -rh | head
```

**Không bao giờ** chạy `docker system prune -a --volumes` trên máy production:
nó xoá cả volume đang gắn nếu container đang dừng.

Kiểm tra cả bảng `messages` và `notifications` — hai bảng phình nhanh nhất:

```bash
docker compose exec postgres psql -U manage -d manage -c "
SELECT relname, pg_size_pretty(pg_total_relation_size(relid)) AS size
FROM pg_catalog.pg_statio_user_tables
ORDER BY pg_total_relation_size(relid) DESC LIMIT 10;"
```

---

## Hàng đợi ứ đọng

**Cảnh báo:** `JobQueueBacklog`

```bash
# Số message đang chờ
docker compose exec rabbitmq rabbitmqctl list_queues name messages consumers

# Worker còn sống và còn nhận job không
docker compose ps worker
docker compose logs --tail 100 worker
```

Đọc cột `consumers`:

- **`consumers = 0`** → worker không nối vào hàng đợi. Khởi động lại nó.
- **`consumers > 0` mà `messages` vẫn tăng** → xử lý không kịp tốc độ nạp
  vào. Hai cách xử lý, chọn theo nguyên nhân:

```bash
# Nhiều job nhẹ: tăng số worker
docker compose up -d --scale worker=4

# Job nặng (tính lương cả công ty): tăng số goroutine mỗi worker
# WORKER_CONCURRENCY trong .env, rồi khởi động lại
```

Một đợt dồn sau khi bấm "tính lương" là **bình thường** — vài trăm message một
lúc. Ngưỡng cảnh báo là 500 chính vì thế. Kiểm tra trên Grafana xem nó đang
giảm dần hay tăng đều.

---

## Job vào dead-letter

**Cảnh báo:** `DeadLetterQueueNotEmpty`

Ngưỡng là 0: có bất kỳ message nào ở đây nghĩa là **đã có job thất bại và bị
bỏ**. Luôn cần người xem.

```bash
docker compose exec rabbitmq rabbitmqctl list_queues name messages \
  | grep dead

# Nội dung message hỏng: xem log worker quanh thời điểm đó
docker compose logs worker | grep -i "dead-letter\|xử lý job thất bại"
```

Ba nguyên nhân thường gặp:

| Log | Nghĩa | Xử lý |
|---|---|---|
| `message hỏng, đẩy sang dead-letter` | JSON không phân tích được | Bên gửi sai định dạng; tìm trong log api |
| `không có handler cho job này` | Tên job không khớp | Thường do triển khai lệch phiên bản giữa api và worker |
| `xử lý job thất bại` | Handler trả lỗi | Đọc lỗi cụ thể; đây là lỗi nghiệp vụ thật |

Job gửi mail và tính lương đều **chịu được chạy lại** (`ReplaceForPeriod` xoá
sạch phiếu cũ trước khi ghi bộ mới), nên phát lại được. Nhưng phát lại job gửi
mail nghĩa là khách nhận mail hai lần — cân nhắc trước.

---

## Tỷ lệ lỗi 5xx cao

**Cảnh báo:** `HighErrorRate`

```bash
# Route nào lỗi
docker compose logs --tail 500 api | grep '"status":5'

# Dependency nào sập
curl -s http://localhost/ready | jq
```

`/ready` chỉ đúng chỗ ngay: `postgres`, `redis` hoặc `rabbitmq` báo lỗi thay vì
`ok`. Lưu ý `r2` **không** ảnh hưởng tới `ready` — mất R2 thì không tải tệp
lên được, còn lại vẫn phục vụ bình thường.

Mọi response đều có header `X-Request-Id`. Người dùng báo lỗi kèm mã đó thì
tìm được đúng request:

```bash
docker compose logs api | grep '<request-id>'

# Hoặc trên Grafana → panel "Log lỗi của api và worker", truy vấn:
#   {service="api"} |= "<request-id>"
```

`trace_id` đi xuyên cả sang worker và cả bản tin WebSocket, nên một request
sinh ra job nền vẫn lần được cả chuỗi.

---

## Kết nối WebSocket tụt đột ngột

**Cảnh báo:** `WebSocketConnectionsDropped`

Trước tiên: **có phải vừa triển khai không?** Một lần triển khai làm số kết nối
tụt về 0 rồi dựng lại — đó là bình thường, và cảnh báo đã cố loại trường hợp
này nhưng không loại được hết.

```bash
# Số kết nối theo từng instance
curl -s http://localhost/metrics | grep manage_ws_connections

# api có instance nào vừa khởi động lại không
docker compose ps api
```

Nếu không phải do triển khai:

```bash
# nginx còn chuyển tiếp nâng cấp giao thức không
docker compose logs --tail 100 nginx | grep -i "101\|upgrade"

# Kết nối bị đóng vì hàng đợi gửi đầy?
curl -s http://localhost/metrics | grep manage_ws_dropped_total
```

`manage_ws_dropped_total` tăng nghĩa là đang phát nhiều hơn client nhận kịp.
Nguyên nhân hay gặp: một nhóm chat rất lớn, hoặc một vòng lặp phát bản tin.

**Hệ quả quan trọng:** mất kết nối WebSocket là mất dữ liệu chấm công của
khoảng thời gian đó. Xem mục dưới.

---

## Job quét presence ngừng

**Cảnh báo:** `AttendanceCollectorStale` — mức nghiêm trọng

Đây là cảnh báo cấp bách nhất trong hệ thống. Job `attendance.collect_presence`
chạy mỗi phút và là **nguồn dữ liệu chấm công duy nhất**. Mười phút không chạy
là mười phút làm việc biến mất khỏi bảng công của mọi người, và **không dựng
lại được** — bản ghi presence trong Redis chỉ sống 90 giây.

```bash
# Job định kỳ của worker còn chạy không
docker compose logs --tail 200 worker | grep collect_presence

# Redis còn dữ liệu presence không
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning \
  SCARD presence:index

# Mốc thành công gần nhất
curl -s http://localhost:8080/metrics | grep collect_presence
```

Xử lý theo thứ tự:

1. `SCARD presence:index` bằng 0 nhưng có người đang dùng hệ thống → api không
   ghi được presence. Xem log api tìm `không ghi được presence`.
2. Redis sống, presence có dữ liệu, mà job không chạy → khởi động lại worker.
3. Job chạy mà báo lỗi → đọc lỗi cụ thể trong log worker.

Sau khi khôi phục, tổng hợp lại ngày hôm nay:

```bash
# Job tổng hợp chạy lại vô hại (Upsert), nên gọi lại được an toàn
docker compose restart worker   # RunAtStart sẽ quét ngay
```

Phần đã mất thì phải bù bằng **yêu cầu điều chỉnh công** — đó chính là lý do
chức năng đó tồn tại.

---

## Job định kỳ không chạy

**Cảnh báo:** `ScheduledJobStale`

```bash
# Bao lâu rồi chưa THÀNH CÔNG (không phải "chưa chạy")
curl -s http://localhost:8080/metrics \
  | grep manage_scheduled_job_last_success

docker compose logs --tail 300 worker | grep "job định kỳ"
```

Lưu ý phân biệt: chỉ số đo lần **thành công** gần nhất. Một job chạy đúng giờ
nhưng lỗi mỗi lần thì mốc "đã chạy" vẫn mới tinh — cảnh báo này bắt được cả
trường hợp đó.

Danh sách job định kỳ và chu kỳ:

| Job | Chu kỳ | Hậu quả nếu ngừng |
|---|---|---|
| `attendance.collect_presence` | 1 phút | **Mất dữ liệu chấm công, không dựng lại được** |
| `metrics.queue_depth` | 30 giây | Mất chỉ số hàng đợi, không mất dữ liệu |
| `chat.sync_auto_groups` | 15 phút | Nhóm phòng ban/dự án lệch thành viên; tự sửa ở lần chạy sau |
| `notification.email_reminders` | 5 phút | Không gửi email nhắc; thông báo trong ứng dụng vẫn còn |
| `attendance.rollup_yesterday` | 00:30 hằng ngày | Bảng công ngày hôm trước trống; chạy lại được |
| `attendance.rollup_today_noon` | 12:05 hằng ngày | Bảng công hôm nay không cập nhật giữa ngày |

Trừ `collect_presence`, mọi job còn lại đều **chạy lại vô hại**.

---

## Không đăng nhập được

Không có cảnh báo cho việc này — người dùng sẽ báo trước.

```bash
# Mail có ra khỏi hàng đợi không (OTP đi qua worker)
docker compose logs --tail 100 worker | grep -i "mail\|otp"

# Ở môi trường dev: xem hộp thư MailHog
open http://localhost:8025
```

Đăng nhập cần OTP gửi qua email, nên **hàng đợi tắc là không ai đăng nhập
được**. Đó là cái giá của việc bắt buộc OTP, và cũng là lý do có công tắc:

```bash
# Tình huống khẩn cấp: tắt OTP để vào được hệ thống
# CẢNH BÁO: tắt nghĩa là ai có mật khẩu là vào được.
# Bật lại ngay sau khi xử lý xong.
# .env → AUTH_OTP_ENABLED=false → docker compose up -d api
```

Bị chặn vì dò mật khẩu quá nhiều lần:

```bash
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning \
  --scan --pattern 'login_fail:*'

# Xoá bộ đếm của MỘT người
docker compose exec redis redis-cli -a "$REDIS_PASSWORD" --no-auth-warning \
  DEL 'login_fail:<email>'
```

---

## Triển khai và quay lui

```bash
# Triển khai phiên bản mới
IMAGE_TAG=<git-sha> bash scripts/deploy.sh

# Quay lui về phiên bản trước
IMAGE_TAG=<git-sha-cũ> bash scripts/rollback.sh
```

Quay lui **không** hoàn tác migration. Migration của dự án này chỉ thêm, không
xoá cột, nên phiên bản cũ vẫn chạy được trên schema mới — đó là điều kiện để
quay lui an toàn, và nó phải được giữ khi viết migration mới.

Nếu buộc phải hoàn tác migration:

```bash
docker compose run --rm migrate -path /migrations \
  -database "$POSTGRES_DSN" down 1
```

---

## Khôi phục từ backup

```bash
# Liệt kê bản backup có sẵn
bash scripts/restore.sh --list

# Khôi phục. Script hỏi xác nhận vì nó GHI ĐÈ database hiện tại.
bash scripts/restore.sh <tên-file-backup>
```

Chi tiết và quy trình diễn tập: xem `doc/OPERATIONS.md`.
