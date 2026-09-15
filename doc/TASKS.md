# Hệ thống Quản lý Công ty — Danh sách nhiệm vụ

> Tài liệu lộ trình triển khai. Cập nhật lần cuối: 2026-09-15
> Trạng thái: `[ ]` chưa làm · `[~]` đang làm · `[x]` xong
>
> Hướng dẫn chi tiết: [PHASE-0-SETUP.md](./PHASE-0-SETUP.md) · [PHASE-1-SETUP.md](./PHASE-1-SETUP.md)

---

## 1. Tổng quan

Hệ thống quản trị nội bộ doanh nghiệp, gồm 6 nhóm nghiệp vụ:

| Nhóm | Nội dung chính |
|---|---|
| Nhân sự | Nhân viên, phòng ban, chức vụ, hợp đồng |
| Dự án & Công việc | Tạo dự án, giao task, theo dõi tiến độ |
| Chấm công | Đo thời gian làm việc qua presence realtime, đơn nghỉ phép |
| Lương | Bảng lương, phụ cấp, khấu trừ, phiếu lương |
| Realtime | Thông báo đẩy, chat 1-1 và chat nhóm |
| Quản trị | Phân quyền RBAC, nhật ký hệ thống, cấu hình công ty |

### Quyết định kiến trúc đã chốt

| Vấn đề | Lựa chọn |
|---|---|
| Kiểu kiến trúc | **Monolith theo module** — một codebase, một database, ranh giới module rõ ràng trong code |
| Số binary | Hai: `api` (HTTP + WebSocket) và `worker` (consumer RabbitMQ). Scale độc lập được |
| Giao tiếp nội bộ | Gọi hàm trực tiếp giữa các usecase. **RabbitMQ** cho việc chạy nền và sự kiện |
| Clean Architecture | Áp dụng cho toàn bộ backend: `delivery` → `usecase` → `domain` ← `repository` |
| Xác thực | JWT tự triển khai: access token ngắn hạn + refresh token lưu Redis |
| Phân quyền | RBAC theo vai trò + kiểm tra phạm vi dữ liệu (phòng ban / dự án) |
| Realtime | WebSocket hub tự viết bằng Go, RabbitMQ fan-out giữa các instance, Redis lưu presence |
| Chấm công | Presence-based: đo thời gian online thực tế qua WebSocket heartbeat |
| Lưu trữ tệp | **Cloudflare R2** (tương thích S3). Client tải THẲNG lên bằng presigned URL, không đi qua backend |
| Triển khai | Docker toàn bộ — dev và production đều chạy bằng container, deploy qua Docker Compose |
| Lộ trình | Chia phase, mỗi phase chạy được độc lập |

---

## 2. Stack kỹ thuật

**Backend**
- Go 1.27, router `go-chi/chi/v5`
- PostgreSQL 16 — dữ liệu nghiệp vụ
- Redis 7 — session, refresh token, presence, cache, rate limit
- RabbitMQ 3.13 — hàng đợi sự kiện, fan-out realtime, job nền
- `pgx/v5` (driver), `golang-migrate` (migration), `sqlc` hoặc query thủ công
- `gorilla/websocket`, `go-playground/validator`, `zerolog`, `viper`, `golang-jwt/jwt/v5`

**Frontend**
- Next.js 16 (App Router), TypeScript, React 19
- TailwindCSS + shadcn/ui
- TanStack Query cho dữ liệu từ server (cache, làm mới, optimistic update)
- Trạng thái client dùng React Context. **Chưa dùng Zustand** — trạng thái toàn cục duy nhất hiện nay là phiên đăng nhập, Context xử lý đủ. Thêm khi nào có trạng thái dùng chung thật sự (bộ lọc chia sẻ giữa màn hình, bong bóng chat ở Phase 5)
- `react-hook-form` + `zod`
- WebSocket client tự viết, có auto-reconnect + exponential backoff

**Hạ tầng**
- Docker + Docker Compose v2 — toàn bộ dịch vụ chạy trong container ở mọi môi trường
- Nginx (container) làm reverse proxy, hỗ trợ nâng cấp WebSocket, kết thúc TLS
- Cloudflare R2 (tương thích S3) cho tệp đính kèm và ảnh đại diện
- GitHub Actions cho CI/CD, build và đẩy image lên GitHub Container Registry (GHCR)
- Prometheus + Grafana + Loki (container) cho giám sát và log

---

## 3. Cấu trúc thư mục Clean Architecture

```
manage/
├── doc/                          # Tài liệu
├── docker/                       # Mọi thứ liên quan container
│   ├── backend/
│   │   └── Dockerfile            # DÙNG CHUNG api + worker, chọn bằng arg BINARY
│   ├── frontend/
│   │   └── Dockerfile            # Multi-stage, dùng Next.js standalone output
│   ├── nginx/
│   │   ├── nginx.conf
│   │   └── conf.d/app.conf       # Reverse proxy + WebSocket upgrade + TLS
│   ├── postgres/
│   │   └── init/                 # Chạy lần đầu: bật extension uuid, pg_trgm, unaccent
│   └── monitoring/               # Phase 6: prometheus.yml, grafana/
├── backend/
│   ├── go.mod
│   ├── cmd/
│   │   ├── api/main.go           # Binary 1: HTTP REST + WebSocket
│   │   └── worker/main.go        # Binary 2: consumer RabbitMQ (mail, thông báo, tính lương)
│   ├── .air.api.toml             # Cấu hình hot reload cho api
│   ├── .air.worker.toml          # Cấu hình hot reload cho worker
│   ├── migrations/               # *.up.sql / *.down.sql — MỘT bộ cho cả hệ thống
│   ├── internal/
│   │   ├── domain/               # TẦNG 1 — Entity + interface, KHÔNG import framework
│   │   │   ├── employee/         # entity.go, repository.go, errors.go
│   │   │   ├── department/
│   │   │   ├── project/
│   │   │   ├── task/
│   │   │   ├── attendance/
│   │   │   ├── payroll/
│   │   │   ├── chat/
│   │   │   └── notification/
│   │   ├── usecase/              # TẦNG 2 — Nghiệp vụ, chỉ phụ thuộc domain
│   │   │   ├── auth/
│   │   │   ├── employee/
│   │   │   ├── project/
│   │   │   ├── attendance/
│   │   │   ├── payroll/
│   │   │   └── chat/
│   │   ├── repository/           # TẦNG 3 — Hiện thực interface của domain
│   │   │   ├── postgres/
│   │   │   ├── redis/
│   │   │   └── rabbitmq/
│   │   └── delivery/             # TẦNG 4 — Cổng vào
│   │       ├── http/             # handler/, middleware/, router/, dto/
│   │       ├── ws/               # hub, client, room, event (Phase 5)
│   │       └── consumer/         # Handler cho message RabbitMQ (worker)
│   ├── pkg/                      # Tiện ích không gắn nghiệp vụ
│   │   ├── config/ logger/ apperror/
│   │   ├── postgres/ redis/ rabbitmq/
│   │   ├── httpx/                # Chuẩn hoá response
│   │   └── jwt/ hash/ token/ validator/ pagination/
│   └── tests/                    # Integration test, testcontainers
├── frontend/
│   └── src/
│       ├── app/                  # App Router: (auth)/ (dashboard)/
│       ├── components/           # ui/, layout/, feature/
│       ├── features/             # mỗi feature: types.ts, api.ts, queries.ts
│       │                         #   types.ts   — kiểu dữ liệu và nhãn hiển thị
│       │                         #   api.ts     — hàm gọi HTTP thuần, không biết react
│       │                         #   queries.ts — khoá cache + hook useQuery/useMutation
│       ├── lib/                  # api-client, ws-client, auth, utils
│       └── stores/
├── docker-compose.yml            # Định nghĩa gốc, dùng chung mọi môi trường
├── docker-compose.override.yml   # Dev: bind mount, hot reload, expose port ra ngoài
├── docker-compose.prod.yml       # Prod: image từ registry, replica, resource limit
├── .env.example                  # Biến môi trường cho compose
├── .gitattributes                # Ép LF cho Dockerfile, .sh, .conf — tránh lỗi CRLF
├── Makefile                      # Linux, macOS, WSL, CI
├── dev.ps1                       # Windows PowerShell (không cần cài make)
└── README.md
```

**Quy tắc phụ thuộc — bắt buộc tuân thủ:**
`delivery` → `usecase` → `domain` ← `repository`

- `domain` không import framework, driver database hay thư viện mạng. Standard library (`time`, `context`, `errors`) thì dùng thoải mái.
- `usecase` chỉ nhận interface, không biết đến PostgreSQL / Redis / HTTP.
- Struct HTTP request/response (DTO) không được rò rỉ xuống `usecase` — tầng `delivery` chịu trách nhiệm dịch.
- Hai binary `api` và `worker` dùng chung toàn bộ `internal/`, chỉ khác nhau ở tầng `delivery`: một bên nhận HTTP, một bên nhận message RabbitMQ.

**Ranh giới module — thứ giữ cho monolith không thành mớ hỗn độn:**

- Mỗi module (`employee`, `project`, `payroll`…) chỉ gọi module khác **qua tầng usecase**, không gọi thẳng repository của nhau.
- Module A cần dữ liệu của module B thì khai báo interface trong chính module A, B đáp ứng interface đó. Không import ngược.
- Ranh giới rõ ràng ngay từ đầu giúp sau này nếu thật sự cần tách service thì tách được — còn chưa cần thì không phải trả giá vận hành.

---

## 4. Kiến trúc triển khai Docker

### Topology container

```
                         ┌──────────────────────────────┐
      Internet  ──443───▶│  nginx  (reverse proxy, TLS) │   ← container DUY NHẤT
                         └───────┬──────────────┬───────┘      mở port ra ngoài
                                 │              │
                        / (SPA)  │              │  /api  ·  /ws
                                 ▼              ▼
                        ┌────────────────┐  ┌────────────────┐
     network `proxy`    │    frontend    │  │      api       │ ← scale nhiều replica
                        │   (Next.js)    │  │ (chi: REST+WS) │
                        └────────────────┘  └───────┬────────┘
   ═══════════════════════════════════════════════  │  ═══════════════════════
                        ┌────────────────┐          │
     network `backend`  │     worker     │          │   worker và api dùng chung
                        │ (job nền, mail)│          │   toàn bộ code internal/
                        └───────┬────────┘          │
                                │                   │
                    ┌───────────┴───────┬───────────┴───────┐
                    ▼                   ▼                   ▼
             ┌────────────┐      ┌────────────┐     ┌────────────┐
             │  postgres  │      │   redis    │     │  rabbitmq  │
             │MỘT database│      │  session   │     │  job + sự  │
             │ có FK thật │      │  presence  │     │  kiện nền  │
             └────────────┘      └────────────┘     └────────────┘
   ═══════════════════════════════════════════════════════════════════════
                                                    ┌──────────────────┐
   Dịch vụ ngoài (không phải container)  ──HTTPS──▶ │  Cloudflare R2   │
   Client tải tệp THẲNG lên đây bằng                │  ảnh, tệp đính   │
   presigned URL, không đi qua backend.             │  kèm, phiếu lương│
                                                    └──────────────────┘

   Chỉ dev: adminer (xem DB) · mailhog (bắt email)
   Giám sát (network riêng): prometheus · grafana · loki · promtail · cadvisor
```

`api` và `worker` build từ **cùng một Dockerfile**, chọn binary nào bằng build arg `BINARY`.

### Nguyên tắc container

| Nguyên tắc | Lý do |
|---|---|
| Một tiến trình mỗi container | `api` và `worker` là hai container riêng dù dùng chung codebase, để scale độc lập |
| Migration tách thành job riêng | Chạy một lần trước khi `api` khởi động — nhiều replica cùng migrate sẽ xung đột |
| Multi-stage build | Image production chỉ chứa binary, không chứa toolchain. Mục tiêu backend < 30MB, frontend < 200MB |
| Chạy bằng user không phải root | Tạo user `app` (UID 1001) trong Dockerfile, hạn chế thiệt hại khi bị chiếm quyền |
| Chỉ nginx expose ra host | Postgres, Redis, RabbitMQ không mở port ra ngoài ở production |
| Mọi container có `healthcheck` | `depends_on: condition: service_healthy` bảo đảm thứ tự khởi động đúng |
| Dữ liệu nằm ở named volume | Không dùng bind mount cho database ở production (tránh vấn đề quyền và hiệu năng) |
| Cấu hình qua biến môi trường | Image không chứa cấu hình; cùng một image chạy được ở staging và production |
| Image gắn tag theo git SHA | Không dùng `latest` ở production — cần biết chính xác đang chạy commit nào để rollback |

### Ba file compose

| File | Dùng khi nào | Nội dung |
|---|---|---|
| `docker-compose.yml` | Luôn luôn | Định nghĩa service, network, volume, healthcheck, dependency |
| `docker-compose.override.yml` | Dev (tự động nạp) | Build tại chỗ, bind mount source, hot reload, mở port DB ra host để debug, thêm adminer + mailhog |
| `docker-compose.prod.yml` | Production | Kéo image từ GHCR, `restart: unless-stopped`, giới hạn CPU/RAM, cấu hình log rotation, bỏ mọi port thừa |

Lệnh chạy:
- Dev: `docker compose up -d` (tự gộp file override)
- Prod: `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d`

### Những điểm dễ sai cần xử lý ngay

- **WebSocket qua nginx** cần `proxy_set_header Upgrade` / `Connection "upgrade"`, `proxy_http_version 1.1` và `proxy_read_timeout` đủ dài (≥ 3600s), nếu không kết nối sẽ bị ngắt mỗi 60 giây.
- **Hot reload trong container trên Windows**: bind mount của Docker Desktop không phát sự kiện file tin cậy — cần bật polling cho `air` (backend) và `WATCHPACK_POLLING=true` (Next.js).
- **Migration khi có nhiều replica `api`**: không để mỗi container tự chạy migrate. Tách thành job `migrate` chạy một lần trước khi `api` khởi động.
- **Thứ tự khởi động**: `depends_on` mặc định chỉ chờ container *started*, không chờ *ready*. Bắt buộc dùng healthcheck kèm `condition: service_healthy`.
- **Graceful shutdown**: Go binary phải chạy ở PID 1 và bắt `SIGTERM`; dùng `exec` trong entrypoint script, đặt `stop_grace_period` đủ dài để đóng hết kết nối WebSocket.

---

## 5. Mô hình dữ liệu (bảng chính)

> Toàn bộ nằm trong **một database**, có foreign key thật để PostgreSQL bảo đảm tính toàn vẹn. Mỗi nhóm bảng thuộc về một module trong code; module khác muốn đọc thì gọi qua tầng usecase của module sở hữu, không truy vấn thẳng bảng của nhau.

| Bảng | Ghi chú |
|---|---|
| `companies` | Thông tin công ty, múi giờ, khung giờ làm việc mặc định |
| `departments` | Có `parent_id` để dựng cây phòng ban, `manager_id` |
| `positions` | Chức vụ, gắn với dải lương |
| `employees` | Hồ sơ nhân viên, `department_id`, `position_id`, `manager_id`, `work_mode` (onsite/remote/hybrid) |
| `users` | Tài khoản đăng nhập, 1-1 với `employees`, `password_hash` |
| `roles`, `permissions`, `role_permissions`, `user_roles` | RBAC |
| `projects` | Dự án, `owner_id`, ngày bắt đầu/kết thúc, trạng thái |
| `project_members` | Thành viên dự án + vai trò trong dự án |
| `tasks` | Công việc, `assignee_id`, `parent_task_id`, độ ưu tiên, deadline, ước lượng giờ |
| `task_comments`, `task_attachments`, `task_activities` | Bình luận, tệp, nhật ký thay đổi |
| `work_schedules` | Ca làm việc / khung giờ theo phòng ban hoặc cá nhân |
| `attendance_sessions` | Mỗi phiên online: `started_at`, `ended_at`, `source` (ws/manual) |
| `attendance_days` | Tổng hợp theo ngày: tổng phút online, phút idle, trạng thái duyệt |
| `leave_requests` | Đơn nghỉ phép, loại, số ngày, luồng duyệt |
| `salary_structures` | Lương cơ bản + phụ cấp theo nhân viên, có hiệu lực theo thời gian |
| `payroll_periods` | Kỳ lương (tháng), trạng thái: draft → locked → paid |
| `payslips` | Phiếu lương từng nhân viên trong kỳ |
| `payslip_items` | Dòng chi tiết: phụ cấp, khấu trừ, thuế, BHXH |
| `conversations` | Hội thoại: `type` = direct / group |
| `conversation_members` | Thành viên + `last_read_message_id` |
| `messages` | Tin nhắn, `reply_to_id`, `edited_at`, `deleted_at` |
| `message_attachments` | Tệp đính kèm tin nhắn |
| `notifications` | Thông báo trong ứng dụng, `read_at` |
| `audit_logs` | Nhật ký thao tác nhạy cảm (sửa lương, xoá nhân viên...) |

---

## PHASE 0 — Nền tảng dự án

**Mục tiêu:** Dựng **walking skeleton** — bộ khung mỏng nhưng xuyên suốt: trình duyệt → nginx → api → PostgreSQL, và api → RabbitMQ → worker. Mọi phase sau chỉ việc lắp nghiệp vụ vào khuôn có sẵn.

> Hướng dẫn chi tiết từng bước kèm mã nguồn đầy đủ: **[PHASE-0-SETUP.md](./PHASE-0-SETUP.md)**

### Hạ tầng & công cụ
- [ ] Khởi tạo repo git, `.gitignore`, `.editorconfig`
- [ ] File `.env.example` gốc cho compose + `.env.example` riêng cho backend và frontend
- [ ] `Makefile`: `make up`, `make down`, `make logs`, `make migrate`, `make test`, `make lint`, `make seed`, `make sh-api`

### Docker — môi trường dev
- [ ] `docker-compose.yml` gốc: postgres, redis, rabbitmq, api, worker, frontend, nginx
- [ ] Khai báo `healthcheck` cho cả 3 dịch vụ hạ tầng (`pg_isready`, `redis-cli ping`, `rabbitmq-diagnostics ping`)
- [ ] Dùng `depends_on: condition: service_healthy` cho api và worker
- [ ] Named volume cho dữ liệu: `pgdata`, `redisdata`, `rabbitmqdata`
- [ ] Hai network tách biệt: `backend` (nội bộ) và `proxy` (nginx ↔ api/frontend)
- [ ] `docker/backend/Dockerfile` **dùng chung cho `api` và `worker`**, chọn binary bằng build arg `BINARY`, có target `dev` / `builder` / `prod`
- [ ] `.air.api.toml` và `.air.worker.toml` — hai cấu hình hot reload riêng
- [ ] Air bật `poll = true` (bind mount trên Windows không phát sự kiện inotify) và `send_interrupt = true` (để code graceful shutdown thật sự được chạy khi reload)
- [ ] Volume ẩn danh cho `/app/tmp` để binary Linux của Air không làm bẩn repo trên host
- [ ] `docker/frontend/Dockerfile` multi-stage, bật `output: "standalone"` trong `next.config.js`
- [ ] `docker-compose.override.yml`: bind mount source, bật polling cho hot reload trên Windows, expose port 5432/6379/15672 ra host để debug
- [ ] Thêm adminer + mailhog vào file override (chỉ dev, không có ở production)
- [ ] `docker/postgres/init/01-extensions.sql`: bật `uuid-ossp`, `pg_trgm`, `unaccent` (phục vụ tìm kiếm tiếng Việt)
- [ ] `docker/nginx/conf.d/app.conf`: proxy `/api` và `/ws` sang api, còn lại sang frontend, có cấu hình nâng cấp WebSocket đầy đủ
- [ ] Service `migrate` chạy một lần rồi thoát, `api` chờ nó hoàn tất
- [ ] `docker/backend/entrypoint.sh` dùng `exec` để binary giữ PID 1, nhận đúng `SIGTERM`
- [ ] `.dockerignore` cho cả backend và frontend (loại `node_modules`, `.git`, `tmp`, file test)
- [ ] Kiểm chứng: clone repo sạch → `make up` → mở `http://localhost` thấy trang chạy, không cần cài Go hay Node trên máy

### Tiện ích nền (`backend/pkg/`)
- [ ] `config`: đọc env bằng viper, validate lúc khởi động, chết ngay nếu thiếu cấu hình
- [ ] `logger`: zerolog, dev in dạng đọc được, production in JSON, gắn `request_id` vào mọi log
- [ ] `postgres`: pgxpool + retry khi khởi động + healthcheck
- [ ] `redis`: kết nối có retry, healthcheck
- [ ] `rabbitmq`: kết nối tự reconnect khi rớt, khai báo exchange + queue lúc khởi động
- [ ] `httpx`: chuẩn hoá `{ data, meta }` / `{ error: { code, message, details } }`
- [ ] `apperror`: ánh xạ lỗi domain → HTTP status, không để lộ chi tiết lỗi nội bộ ra ngoài
- [ ] Thiết lập golangci-lint + cấu hình

### Binary `api`
- [ ] Router chi + middleware: RequestID, RealIP, Logger, Recoverer, Timeout, CORS, Compress
- [ ] `GET /health` (sống chưa) và `GET /ready` (kiểm tra cả postgres, redis, rabbitmq)
- [ ] `GET /api/v1/ping` — đọc giờ từ PostgreSQL, bắn một job sang worker, endpoint nghiệm thu Phase 0
- [ ] Bộ khung Clean Architecture mẫu: một module đi hết 4 tầng `domain` → `usecase` → `repository` → `delivery`
- [ ] Graceful shutdown: bắt `SIGTERM`, đóng HTTP server, đóng pool, `stop_grace_period` 60s (chuẩn bị cho WebSocket ở Phase 5)

### Binary `worker`
- [ ] Consumer RabbitMQ: nhận message, ack/nack đúng cách, có dead-letter queue
- [ ] Đăng ký handler theo tên job, dễ thêm job mới ở phase sau
- [ ] Xử lý một job mẫu (ghi log) để kiểm chứng đường truyền api → RabbitMQ → worker
- [ ] Graceful shutdown: xử lý xong message đang cầm rồi mới thoát, không bỏ dở

### Khung frontend
- [ ] `create-next-app` với TypeScript, Tailwind, App Router
- [ ] Cài shadcn/ui, dựng theme (light/dark)
- [ ] API client bọc `fetch`: tự gắn token, tự refresh khi 401, xử lý lỗi tập trung
- [ ] Layout khung: sidebar, topbar, breadcrumb, khu vực thông báo
- [ ] ESLint + Prettier + husky pre-commit

### CI
- [ ] GitHub Actions: lint + test + build cho cả backend và frontend
- [ ] Migration chạy tự động trong CI trên PostgreSQL service container
- [ ] Job build Docker image cho `api`, `worker`, `frontend` — có cache layer (`docker/build-push-action` + GHA cache theo scope riêng)
- [ ] Kiểm tra `go mod tidy` không tạo thay đổi — chặn merge nếu `go.mod` chưa sạch
- [ ] Gắn tag image theo git SHA và theo nhánh; chỉ đẩy lên GHCR khi merge vào `main`
- [ ] Quét lỗ hổng image bằng Trivy, chặn merge nếu có lỗi mức HIGH/CRITICAL

---

## PHASE 1 — Xác thực, phân quyền, nhân sự

**Mục tiêu:** Đăng nhập được, phân quyền hoạt động, quản lý đầy đủ nhân viên và phòng ban.

> **Trạng thái: đã xong.** Kiểm chứng bằng hai script trong `scripts/`:
> `smoke-auth.sh` (23 mục bảo mật) và `smoke-hr.sh` (26 mục nghiệp vụ), đều đạt toàn bộ.
>
> Hai việc còn để lại có chủ ý: nhập nhân viên từ Excel (cần hạ tầng báo tiến
> độ của Phase 5) và sơ đồ tổ chức dạng đồ hoạ (API cây đã có, chỉ thiếu phần vẽ).

### Domain & migration
- [x] Migration: `companies`, `departments`, `positions`, `employees`, `users`
- [x] Migration RBAC: `roles`, `permissions`, `role_permissions`, `user_roles`
- [x] Entity domain + interface repository cho employee, department, position
- [x] Seed dữ liệu: công ty mẫu, 5 vai trò (`admin`, `director`, `manager`, `hr`, `employee`), tài khoản admin đầu tiên

### Xác thực
- [x] Băm mật khẩu bằng bcrypt (cost ≥ 12)
- [x] Package JWT: phát hành / xác minh access token (15 phút), refresh token (7 ngày)
- [x] Lưu refresh token vào Redis kèm khoá theo thiết bị, hỗ trợ thu hồi
- [x] `POST /api/v1/auth/login`
- [x] `POST /api/v1/auth/refresh` — có xoay vòng refresh token (rotation)
- [x] `POST /api/v1/auth/logout` — thu hồi token của thiết bị hiện tại
- [x] `POST /api/v1/auth/logout-all` — thu hồi mọi thiết bị
- [x] `GET /api/v1/auth/me`
- [x] Đổi mật khẩu, quên mật khẩu (gửi mail qua RabbitMQ worker)
- [x] Rate limit theo IP + theo tài khoản cho endpoint login (chống brute force)

### Phân quyền
- [x] Middleware `RequireAuth` — giải mã token, nạp thông tin user vào context
- [x] Middleware `RequirePermission("employee:update")` — kiểm tra quyền chi tiết
- [x] Kiểm tra phạm vi dữ liệu: trưởng phòng chỉ xem được nhân viên phòng mình; giám đốc xem toàn bộ
- [x] API quản trị vai trò: gán / gỡ vai trò cho người dùng

### Nhân sự
- [x] CRUD phòng ban, hỗ trợ cấu trúc cây, chặn tạo vòng lặp cha-con
- [x] CRUD chức vụ
- [x] CRUD nhân viên: tạo, sửa, xem, vô hiệu hoá (soft delete, không xoá cứng)
- [x] Tìm kiếm + lọc nhân viên: theo phòng ban, chức vụ, trạng thái, hình thức làm việc
- [x] Phân trang chuẩn (cursor hoặc offset) áp dụng cho mọi API danh sách
- [x] Tải lên avatar, lưu Cloudflare R2, trả về presigned URL
- [ ] Nhập nhân viên hàng loạt từ CSV/Excel (xử lý nền qua RabbitMQ, báo kết quả qua thông báo)
- [ ] Xem sơ đồ tổ chức (org chart) dạng cây

### Frontend Phase 1
- [x] Trang đăng nhập + xử lý refresh token ngầm
- [x] Middleware Next.js bảo vệ route, chuyển hướng khi chưa đăng nhập
- [x] Hiển thị/ẩn thành phần UI theo quyền (`usePermission` hook)
- [x] Trang danh sách nhân viên: bảng, lọc, tìm kiếm, phân trang
- [x] Form tạo/sửa nhân viên, validate bằng zod
- [x] Trang chi tiết nhân viên (hồ sơ, phòng ban, cấp trên)
- [x] Trang quản lý phòng ban + sơ đồ tổ chức
- [x] Trang hồ sơ cá nhân, đổi mật khẩu

---

## PHASE 2 — Dự án & Công việc

**Mục tiêu:** Tạo dự án, giao việc, theo dõi tiến độ bằng bảng Kanban.

### Backend
- [ ] Migration: `projects`, `project_members`, `tasks`, `task_comments`, `task_attachments`, `task_activities`
- [ ] Domain + usecase cho project và task
- [ ] CRUD dự án; chỉ chủ dự án hoặc giám đốc được sửa/đóng
- [ ] Thêm/xoá thành viên dự án, gán vai trò trong dự án (owner / member / viewer)
- [ ] CRUD task: tiêu đề, mô tả, người thực hiện, độ ưu tiên, deadline, ước lượng giờ
- [ ] Task con (`parent_task_id`), chặn lồng quá 2 cấp
- [ ] Chuyển trạng thái task: `todo → in_progress → review → done`, chặn bước nhảy không hợp lệ
- [ ] Sắp xếp thứ tự task trong cột Kanban (dùng số thực hoặc chuỗi lexo để chèn giữa)
- [ ] Bình luận task, hỗ trợ `@mention` → sinh thông báo
- [ ] Đính kèm tệp vào task (Cloudflare R2)
- [ ] Ghi nhật ký thay đổi task (`task_activities`) — ai đổi gì, lúc nào
- [ ] Ghi nhận thời gian làm việc theo task (timelog), phục vụ báo cáo
- [ ] API báo cáo: tiến độ dự án, task quá hạn, khối lượng việc theo nhân viên
- [ ] Phát sự kiện lên RabbitMQ khi: giao task, đổi trạng thái, sắp đến hạn, bị mention

### Frontend
- [ ] Trang danh sách dự án (dạng thẻ + dạng bảng)
- [ ] Trang tổng quan dự án: tiến độ, thành viên, task gần đây
- [ ] Bảng Kanban kéo-thả (`dnd-kit`), cập nhật lạc quan (optimistic update)
- [ ] Bảng danh sách task có lọc: người thực hiện, trạng thái, độ ưu tiên, deadline
- [ ] Panel chi tiết task: mô tả, bình luận, tệp đính kèm, lịch sử thay đổi
- [ ] Biểu đồ Gantt hoặc timeline đơn giản cho dự án
- [ ] Trang "Việc của tôi" tổng hợp task xuyên dự án

---

## PHASE 3 — Chấm công & Nghỉ phép

**Mục tiêu:** Tự động ghi nhận thời gian làm việc dựa trên presence realtime, không cần bấm nút.

> **Nguyên tắc thiết kế:** Kết nối WebSocket vốn đã có sẵn cho chat và thông báo được tận dụng làm nguồn dữ liệu chấm công. Khi nhân viên mở ứng dụng, client gửi heartbeat định kỳ; server ghi nhận phiên online và cộng dồn thời gian trong khung giờ làm việc.
>
> **Giới hạn cần ý thức:** mở tab không đồng nghĩa với đang làm việc. Vì vậy hệ thống phân biệt rõ *thời gian online* và *thời gian hoạt động* (có tương tác), đồng thời vẫn cho phép check-in thủ công và để quản lý duyệt/điều chỉnh. Không dùng dữ liệu này làm căn cứ kỷ luật tự động.

### Cơ chế presence
- [ ] Client gửi heartbeat mỗi 30 giây qua WebSocket (kèm cờ `is_active`)
- [ ] Phát hiện idle phía client: không có chuột/bàn phím > 5 phút → `is_active = false`
- [ ] Bắt sự kiện `visibilitychange` — chuyển tab / thu nhỏ cửa sổ → đánh dấu không hoạt động
- [ ] Server lưu presence vào Redis: `presence:{user_id}` với TTL 90 giây
- [ ] Job nền quét Redis mỗi phút, ghi các khoảng online vào `attendance_sessions`
- [ ] Gộp các phiên rời rạc cách nhau dưới 5 phút thành một phiên liền mạch
- [ ] Job cuối ngày tổng hợp `attendance_sessions` → `attendance_days`
- [ ] Xử lý đúng múi giờ công ty, không dùng giờ máy chủ trực tiếp

### Nghiệp vụ chấm công
- [ ] Migration: `work_schedules`, `attendance_sessions`, `attendance_days`, `leave_requests`
- [ ] Cấu hình khung giờ làm việc theo công ty / phòng ban / cá nhân
- [ ] Tính các chỉ số theo ngày: giờ online, giờ hoạt động, đi muộn, về sớm, thiếu giờ
- [ ] Check-in / check-out thủ công cho trường hợp ngoại lệ (mất mạng, họp ngoài)
- [ ] Nhân viên gửi yêu cầu điều chỉnh công, quản lý duyệt
- [ ] Luồng đơn nghỉ phép: tạo → quản lý duyệt/từ chối → trừ quỹ phép
- [ ] Quản lý quỹ ngày phép năm, phép tồn
- [ ] Đánh dấu ngày lễ, ngày nghỉ theo lịch công ty
- [ ] API báo cáo chấm công: theo nhân viên, theo phòng ban, theo tháng
- [ ] Xuất báo cáo chấm công ra Excel (xử lý nền)

### Frontend
- [ ] Widget trạng thái làm việc trên topbar: đang online, tổng giờ hôm nay
- [ ] Trang chấm công cá nhân: dòng thời gian trong ngày, lịch tháng
- [ ] Trang chấm công phòng ban (dành cho quản lý): ai đang online, ai nghỉ
- [ ] Form gửi yêu cầu điều chỉnh công
- [ ] Trang đơn nghỉ phép: tạo đơn, theo dõi trạng thái, số phép còn lại
- [ ] Hàng đợi duyệt đơn cho quản lý
- [ ] Thông báo rõ cho nhân viên rằng thời gian online đang được ghi nhận (minh bạch dữ liệu)

---

## PHASE 4 — Lương

**Mục tiêu:** Tạo bảng lương theo kỳ, sinh phiếu lương, xuất file.

### Backend
- [ ] Migration: `salary_structures`, `payroll_periods`, `payslips`, `payslip_items`
- [ ] Cấu hình lương theo nhân viên, có hiệu lực theo khoảng thời gian (lịch sử tăng lương)
- [ ] Định nghĩa thành phần lương: lương cơ bản, phụ cấp, thưởng, khấu trừ, BHXH, thuế TNCN
- [ ] Bảng thuế TNCN luỹ tiến cấu hình được (không hard-code)
- [ ] Tạo kỳ lương, lấy dữ liệu công từ `attendance_days`
- [ ] Máy tính lương: chạy qua từng nhân viên, sinh `payslips` + `payslip_items`
- [ ] Chạy tính lương trong worker RabbitMQ (kỳ lương lớn không được chặn HTTP request)
- [ ] Vòng đời kỳ lương: `draft → locked → paid`; đã khoá thì không sửa được
- [ ] Sinh phiếu lương PDF
- [ ] Gửi phiếu lương qua email (hàng đợi worker)
- [ ] Kiểm soát truy cập nghiêm ngặt: chỉ HR, kế toán, giám đốc và chính chủ xem được
- [ ] Ghi `audit_logs` cho mọi thao tác xem/sửa dữ liệu lương
- [ ] Mã hoá hoặc hạn chế hiển thị thông tin tài khoản ngân hàng

### Frontend
- [ ] Trang cấu hình lương nhân viên (chỉ HR)
- [ ] Trang danh sách kỳ lương, tạo kỳ mới
- [ ] Bảng lương chi tiết theo kỳ, có thể sửa khi còn `draft`
- [ ] Trang phiếu lương cá nhân, tải PDF
- [ ] Biểu đồ chi phí nhân sự theo phòng ban / theo tháng

---

## PHASE 5 — Realtime: Thông báo & Chat

**Mục tiêu:** Chat 1-1 và chat nhóm hoạt động ổn định, thông báo đẩy tức thì, hoạt động đúng khi chạy nhiều instance backend.

### Hạ tầng WebSocket
- [ ] Hub WebSocket: quản lý client, đăng ký/huỷ đăng ký, broadcast
- [ ] Xác thực khi bắt tay WebSocket (token qua query hoặc subprotocol, không qua cookie)
- [ ] Một người dùng có thể mở nhiều thiết bị — hub phải hỗ trợ nhiều kết nối / 1 user
- [ ] Cơ chế ping/pong, tự đóng kết nối chết
- [ ] Định dạng bản tin chuẩn: `{ type, payload, ts, trace_id }`
- [ ] Fan-out qua RabbitMQ: mỗi instance backend là một consumer, exchange kiểu fanout
- [ ] Redis lưu bản đồ `user_id → instance_id` để định tuyến tin nhắn
- [ ] Giới hạn tốc độ gửi tin nhắn mỗi kết nối (chống spam)
- [ ] Giới hạn kích thước bản tin, đóng kết nối khi vượt ngưỡng

### Thông báo
- [ ] Migration: `notifications`
- [ ] Danh mục loại thông báo: giao task, mention, duyệt đơn, đến hạn, tin nhắn mới
- [ ] Usecase thông báo: ghi DB + đẩy WebSocket nếu online
- [ ] Đánh dấu đã đọc / đọc tất cả
- [ ] Trang danh sách thông báo, có phân trang vô hạn
- [ ] Chuông thông báo hiển thị số chưa đọc, cập nhật realtime
- [ ] Cấu hình nhận thông báo theo loại (bật/tắt từng loại)
- [ ] Gửi email cho thông báo quan trọng khi người dùng offline quá 15 phút

### Chat
- [ ] Migration: `conversations`, `conversation_members`, `messages`, `message_attachments`
- [ ] Tạo hội thoại 1-1 — tự động tái sử dụng nếu đã tồn tại giữa 2 người
- [ ] Tạo nhóm chat: đặt tên, thêm thành viên, phân quyền quản trị nhóm
- [ ] Tạo nhóm tự động theo phòng ban hoặc theo dự án
- [ ] Gửi tin nhắn qua WebSocket, kèm `client_message_id` để chống trùng
- [ ] Lưu tin nhắn bền vững vào PostgreSQL trước khi phát đi
- [ ] Tải lịch sử tin nhắn theo cursor (cuộn ngược lên)
- [ ] Trạng thái đã đọc: cập nhật `last_read_message_id`, hiển thị số chưa đọc
- [ ] Chỉ báo "đang nhập..." (throttle, không lưu DB)
- [ ] Trạng thái online/offline/lần cuối hoạt động (dùng chung dữ liệu presence của Phase 3)
- [ ] Trả lời tin nhắn (reply), sửa và thu hồi tin nhắn (soft delete)
- [ ] Gửi tệp và ảnh (Cloudflare R2), hiển thị xem trước ảnh
- [ ] Tìm kiếm tin nhắn trong hội thoại (PostgreSQL full-text search)
- [ ] Ghim hội thoại, tắt thông báo hội thoại

### Frontend chat
- [ ] Layout chat: danh sách hội thoại + khung tin nhắn
- [ ] `ws-client` tự kết nối lại, backoff tăng dần, xếp hàng tin nhắn khi mất mạng
- [ ] Hiển thị tin nhắn lạc quan (gửi → hiện ngay → xác nhận hoặc báo lỗi)
- [ ] Cuộn vô hạn ngược lên để tải lịch sử
- [ ] Bong bóng chat nổi, mở nhanh từ mọi trang
- [ ] Giao diện quản lý nhóm: thêm/xoá thành viên, đổi tên, rời nhóm
- [ ] Đồng bộ trạng thái đọc giữa nhiều tab (BroadcastChannel)

---

## PHASE 6 — Hoàn thiện & Vận hành

### Chất lượng
- [ ] Unit test cho toàn bộ tầng `usecase` (mục tiêu ≥ 70% coverage)
- [ ] Integration test cho repository bằng testcontainers (PostgreSQL thật)
- [ ] Test API end-to-end cho các luồng chính
- [ ] Test tải cho WebSocket (mục tiêu: 500 kết nối đồng thời)
- [ ] Test E2E frontend bằng Playwright cho 5 luồng quan trọng nhất

### Bảo mật
- [ ] Rà soát toàn bộ endpoint: mọi route đều có kiểm tra quyền, không sót route công khai
- [ ] Chống IDOR: luôn kiểm tra quyền trên bản ghi cụ thể, không chỉ trên loại tài nguyên
- [ ] Ngăn SQL injection (chỉ dùng tham số hoá), XSS (escape đầu ra), CSRF
- [ ] Kiểm tra kiểu tệp tải lên bằng magic bytes, giới hạn dung lượng
- [ ] Đặt security header: HSTS, CSP, X-Frame-Options
- [ ] Quản lý secret bằng biến môi trường, tuyệt đối không commit vào git
- [ ] Ghi audit log cho thao tác nhạy cảm: lương, xoá nhân viên, đổi quyền
- [ ] Bảo mật container: chạy non-root, `read_only: true` cho container không cần ghi, `cap_drop: ALL`, `no-new-privileges`
- [ ] Không nhúng secret vào image (kiểm tra bằng `docker history`); dùng Docker secrets hoặc file env ngoài repo
- [ ] Ghim phiên bản base image theo digest, không dùng tag trôi nổi như `alpine:latest`
- [ ] Quét image định kỳ bằng Trivy, có lịch cập nhật base image khi có CVE mới

### Docker — production
- [ ] Hoàn thiện target `prod` trong Dockerfile backend: build tĩnh (`CGO_ENABLED=0`), `-ldflags="-s -w"`, base image `gcr.io/distroless/static` hoặc `alpine`
- [ ] Hoàn thiện Dockerfile frontend: chỉ copy `.next/standalone` + `.next/static` + `public` sang stage cuối
- [ ] Tạo user không phải root trong cả hai image, khai báo `USER app`
- [ ] Nhúng thông tin build vào binary (version, git SHA, build time) và trả ra ở `/health`
- [ ] `docker-compose.prod.yml`: kéo image từ GHCR theo tag, `restart: unless-stopped`
- [ ] Đặt `deploy.resources.limits` CPU/RAM cho từng service, tránh một container ăn hết máy
- [ ] Cấu hình log driver `json-file` kèm `max-size` và `max-file` (không để log phình đầy ổ đĩa)
- [ ] `stop_grace_period: 60s` cho api để đóng sạch kết nối WebSocket khi deploy
- [ ] Nginx: bật TLS bằng Let's Encrypt (certbot container hoặc Caddy), tự gia hạn
- [ ] Nginx: bật gzip/brotli, cache asset tĩnh, đặt `client_max_body_size` khớp giới hạn upload
- [ ] Kiểm chứng scale: `docker compose up -d --scale api=3` → chat và thông báo vẫn hoạt động đúng (xác nhận fan-out RabbitMQ chạy chuẩn)
- [ ] Script deploy: pull image mới → chạy job `migrate` → rolling restart từng container api
- [ ] Quy trình rollback: đổi tag image về SHA trước đó và khởi động lại
- [ ] Container backup: `pg_dump` theo lịch, nén, đẩy lên Cloudflare R2, xoá bản cũ theo chính sách lưu trữ
- [ ] Diễn tập khôi phục: dựng lại toàn bộ hệ thống từ bản backup trên máy sạch, ghi lại thời gian thực tế

### Giám sát
- [ ] Endpoint `/metrics` ở backend (Prometheus client): số kết nối WebSocket, độ trễ request, độ sâu hàng đợi
- [ ] Stack giám sát bằng container: prometheus, grafana, loki, promtail, cadvisor, node-exporter
- [ ] Dashboard Grafana định nghĩa sẵn dưới dạng file (provisioning), commit vào repo
- [ ] Cảnh báo: container restart liên tục, ổ đĩa > 80%, hàng đợi RabbitMQ ứ đọng, số kết nối WS tụt đột ngột
- [ ] Log tập trung qua Loki, có `trace_id` xuyên suốt request
- [ ] Runbook xử lý sự cố thường gặp (kèm lệnh docker cụ thể cho từng tình huống)

### Tài liệu
- [ ] Sinh tài liệu OpenAPI/Swagger cho REST API
- [ ] Tài liệu giao thức WebSocket: danh sách sự kiện và payload
- [ ] Sơ đồ ERD cơ sở dữ liệu
- [ ] Hướng dẫn cài đặt môi trường dev (chỉ cần Docker, không cần cài Go/Node trên máy)
- [ ] Tài liệu vận hành Docker: danh sách service, biến môi trường, lệnh thường dùng
- [ ] Sổ tay hướng dẫn sử dụng cho người dùng cuối

---

## 6. Thứ tự ưu tiên và phụ thuộc

```
Phase 0  ──>  Phase 1  ──┬──>  Phase 2 (Dự án/Task)
                         │
                         ├──>  Phase 5 (Realtime)  ──>  Phase 3 (Chấm công)
                         │                                    │
                         └────────────────────────────────────┴──>  Phase 4 (Lương)
```

Toàn bộ các phase đều chạy trong Docker ngay từ Phase 0 — không có giai đoạn nào code chạy trực tiếp trên máy rồi mới "đóng gói vào container sau". Việc đóng gói muộn là nguồn gốc của phần lớn lỗi "chạy trên máy tôi thì được".

**Lưu ý phụ thuộc quan trọng:** Phase 3 (chấm công) phụ thuộc vào hạ tầng WebSocket của Phase 5. Có hai cách xử lý:

- **Khuyến nghị:** làm phần *hạ tầng WebSocket + presence* của Phase 5 trước, rồi mới làm Phase 3, để dành phần chat đầy đủ làm sau.
- Hoặc: tạm dùng check-in thủ công ở Phase 3, chuyển sang presence tự động khi Phase 5 xong.

Phase 4 (lương) cần dữ liệu công từ Phase 3 để tính lương theo giờ; nếu chỉ tính lương cố định thì có thể làm độc lập.

---

## 7. Rủi ro và điểm cần lưu ý

| Rủi ro | Hướng xử lý |
|---|---|
| Chấm công qua presence bị hiểu nhầm là giám sát | Minh bạch với nhân viên, chỉ đo thời gian online, không chụp màn hình, không theo dõi ứng dụng khác. Quản lý luôn có quyền điều chỉnh thủ công. |
| Mất kết nối mạng làm mất dữ liệu chấm công | Client đệm heartbeat khi offline và gửi bù khi kết nối lại; cho phép gửi yêu cầu điều chỉnh. |
| WebSocket không mở rộng được khi chạy nhiều instance | Bắt buộc fan-out qua RabbitMQ ngay từ đầu, không giữ trạng thái chỉ trong bộ nhớ một instance. |
| Tin nhắn bị trùng hoặc mất khi reconnect | Dùng `client_message_id` để khử trùng, client gửi lại tin chưa được xác nhận. |
| Tính lương sai do sửa dữ liệu công sau khi chốt | Khoá kỳ lương (`locked`), mọi thay đổi sau đó phải tạo bút toán điều chỉnh riêng. |
| Rò rỉ dữ liệu lương | Phân quyền chặt ở tầng usecase (không chỉ ở UI), ghi audit log mọi lượt truy cập. |
| Vi phạm quy tắc phụ thuộc Clean Architecture | Thêm kiểm tra tự động trong CI (ví dụ `go-arch-lint`) để chặn import sai tầng. |
| Hot reload trong Docker chậm hoặc không nhận thay đổi trên Windows | Bật polling cho `air` và Next.js; nếu vẫn chậm, để `node_modules` trong named volume thay vì bind mount. |
| Nginx ngắt kết nối WebSocket sau 60 giây | Đặt `proxy_read_timeout` và `proxy_send_timeout` ≥ 3600s ngay từ Phase 0, kiểm chứng bằng test kết nối dài. |
| Nhiều replica `api` cùng chạy migration gây xung đột | Tách migration thành job riêng chạy một lần trước khi api khởi động. |
| Mất dữ liệu do xoá nhầm volume (`docker compose down -v`) | Không bao giờ dùng cờ `-v` ở production; backup tự động hằng ngày và có diễn tập khôi phục. |
| Image production phình to, deploy chậm | Multi-stage build, `.dockerignore` đầy đủ, kiểm tra kích thước image trong CI và đặt ngưỡng cảnh báo. |
| Monolith dần thành mớ hỗn độn, module gọi chéo lung tung | Module chỉ gọi nhau qua tầng usecase. Thêm `go-arch-lint` vào CI để chặn import sai tầng ngay từ Phase 0. |
| Một module lỗi làm sập cả `api` | Middleware `Recoverer` bắt panic ở tầng HTTP; worker có dead-letter queue cho message lỗi. |
| Job nặng (tính lương, xuất Excel) làm nghẽn request HTTP | Đẩy sang `worker` qua RabbitMQ, `api` trả về ngay và báo kết quả qua thông báo. |

---

## 8. Bước tiếp theo

1. ~~Viết kế hoạch triển khai chi tiết cho Phase 0~~ → xong: [PHASE-0-SETUP.md](./PHASE-0-SETUP.md)
2. Code Phase 0 theo tài liệu đó.
3. Nghiệm thu: `make init` → `make smoke` trả về JSON có `database_time` và `job_queued: true`, đồng thời log worker hiện job vừa nhận — chứng minh cả hai chuỗi HTTP → PostgreSQL và api → RabbitMQ → worker đã thông.
4. Sang Phase 1: xác thực JWT, RBAC, CRUD nhân viên và phòng ban.
