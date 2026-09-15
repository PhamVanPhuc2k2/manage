# Manage — Hệ thống quản lý công ty

Hệ thống quản trị nội bộ doanh nghiệp: nhân sự, phòng ban, dự án, giao việc, chấm công, lương, cùng thông báo và chat thời gian thực.

**Trạng thái:** Phase 0 và Phase 1 đã xong và chạy được. Đang chuẩn bị Phase 2.

## Kiến trúc

Monolith theo module, hai binary chạy độc lập:

| Thành phần | Vai trò |
|---|---|
| `api` | HTTP REST (và WebSocket từ Phase 5) |
| `worker` | Job nền qua RabbitMQ: gửi mail, tính lương, xuất Excel |

Hai binary dùng chung toàn bộ `internal/`, chỉ khác tầng `delivery`.

**Stack:** Go 1.27 + go-chi · PostgreSQL 16 · Redis 7 · RabbitMQ 3.13 · Next.js 16 · nginx — toàn bộ chạy trong Docker. Tệp lưu trên Cloudflare R2 (dịch vụ ngoài).

Backend theo Clean Architecture: `delivery` → `usecase` → `domain` ← `repository`.

## Chạy thử

Chỉ cần cài Docker, không cần Go hay Node trên máy.

```bash
git clone https://github.com/PhamVanPhuc2k2/manage.git
cd manage
cp .env.example .env
```

Mở `.env` và **kiểm tra các cổng có bị chiếm không** (IIS, Laravel Herd, XAMPP hay chiếm cổng 80). Nếu có, đổi `NGINX_PORT` và sửa `PUBLIC_BASE_URL` cho khớp.

**Cloudflare R2 là tuỳ chọn khi phát triển.** Để trống các biến `R2_*` thì hệ thống chạy bình thường, chỉ chức năng tải tệp báo lỗi rõ ràng. Muốn dùng thì tạo bucket ở Cloudflare Dashboard → R2, lấy access key ở Manage R2 API Tokens, rồi điền `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_BUCKET`.

```bash
# Linux, macOS, WSL
make init && make smoke

# Windows PowerShell (không cần cài make)
.\dev.ps1 init
.\dev.ps1 smoke
```

Kết quả mong đợi của `smoke`:

```json
{"data":{"database_time":"...","job_queued":true,"redis_ok":true,"request_id":"..."}}
```

kèm một dòng log của `worker` mang **đúng `request_id`** đó — chứng minh cả hai chuỗi đã thông:

```
Trình duyệt ─▶ nginx ─▶ api ─▶ PostgreSQL
                         └──▶ RabbitMQ ─▶ worker
```

## Lệnh hay dùng

| Việc | `make` | `dev.ps1` |
|---|---|---|
| Khởi động | `make up` | `.\dev.ps1 up` |
| Xem log | `make logs s=worker` | `.\dev.ps1 logs worker` |
| Trạng thái | `make ps` | `.\dev.ps1 ps` |
| Kiểm tra nhanh | `make smoke` | `.\dev.ps1 smoke` |
| Mở psql | `make psql` | `.\dev.ps1 psql` |
| Xem hàng đợi | `make queues` | `.\dev.ps1 queues` |
| Lint Go | `make lint` | `.\dev.ps1 lint` |
| Tạo migration | `make migrate-create n=create_users` | `.\dev.ps1 migrate-create create_users` |
| Xem hết lệnh | `make help` | `.\dev.ps1 help` |

Sửa file `.go` là Air tự build lại trong container, không cần restart.

## Tài liệu

| File | Nội dung |
|---|---|
| [doc/TASKS.md](doc/TASKS.md) | Lộ trình đầy đủ 7 phase, mô hình dữ liệu, bảng rủi ro |
| [doc/PHASE-0-SETUP.md](doc/PHASE-0-SETUP.md) | Hướng dẫn chi tiết Phase 0 kèm mã nguồn và các lỗi đã gặp thật |
| [doc/PHASE-1-SETUP.md](doc/PHASE-1-SETUP.md) | Thiết kế xác thực và phân quyền: chiến lược token, chống đánh cắp, phạm vi dữ liệu |

## Lộ trình

| Phase | Nội dung | Trạng thái |
|---|---|---|
| 0 | Nền tảng: Docker, api + worker, hot reload, CI | Xong |
| 1 | Xác thực JWT, phân quyền RBAC, nhân viên, phòng ban, chức vụ | Xong |
| 2 | Dự án, giao việc, bảng Kanban | Kế tiếp |
| 3 | Chấm công theo presence realtime, nghỉ phép | |
| 4 | Lương, phiếu lương | |
| 5 | WebSocket: thông báo và chat | |
| 6 | Hoàn thiện, bảo mật, giám sát, vận hành | |

## Lưu ý

- **Không commit `.env`.** File này chứa mật khẩu và đã nằm trong `.gitignore`. Mật khẩu trong `.env.example` chỉ là giá trị mẫu cho môi trường dev — phải đổi hết trước khi lên production.
- **Đổi `JWT_SECRET`** trước khi deploy. Ứng dụng sẽ từ chối khởi động ở chế độ production nếu còn dùng giá trị mặc định.

## Kiểm chứng

Hai script chạy lại bất cứ lúc nào, tự tạo và tự dọn dữ liệu kiểm thử:

```bash
ADMIN_PASS='...' bash scripts/smoke-auth.sh   # 26 mục bảo mật
ADMIN_PASS='...' bash scripts/smoke-hr.sh     # 31 mục nghiệp vụ nhân sự
```

`smoke-auth.sh` kiểm tra những thứ dễ hỏng âm thầm: giả mạo JWT, xoay vòng
refresh token, phát hiện token bị đánh cắp, đăng xuất có hiệu lực tức thì,
chống dò mật khẩu và chống dò email.

`smoke-hr.sh` kiểm tra nghiệp vụ: chặn vòng lặp trong cây phòng ban, chặn xoá
phòng còn người, tìm kiếm tiếng Việt không dấu, tạo tài khoản, đổi vai trò và
phạm vi dữ liệu đổi theo.
