# Hệ thống Quản lý Công ty — Danh sách nhiệm vụ

> Tài liệu lộ trình triển khai. Cập nhật lần cuối: 2026-09-21 (Phase 4 xong)
> Trạng thái: `[ ]` chưa làm · `[~]` đang làm · `[x]` xong
>
> Hướng dẫn chi tiết: [PHASE-0-SETUP.md](./PHASE-0-SETUP.md) · [PHASE-1-SETUP.md](./PHASE-1-SETUP.md)

---

## 1. Tổng quan

Hệ thống quản trị nội bộ doanh nghiệp, gồm 7 nhóm nghiệp vụ:

| Nhóm | Nội dung chính |
|---|---|
| Nhân sự | Nhân viên, phòng ban, chức vụ, hợp đồng |
| Dự án & Công việc | Tạo dự án, giao task, theo dõi tiến độ |
| Chấm công | Đo thời gian làm việc qua presence realtime, đơn nghỉ phép |
| Lương | Bảng lương, phụ cấp, khấu trừ, phiếu lương |
| Realtime | Thông báo đẩy, chat 1-1 và chat nhóm |
| Họp trực tuyến | Gọi video 1-1 và gọi nhóm, trình chiếu màn hình |
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
| Gọi video & trình chiếu | **WebRTC** truyền media, **SFU (LiveKit)** cho gọi nhóm, **TURN** bắt buộc kèm fallback TCP/443. WebSocket chỉ làm signaling — TCP không tải được media realtime |
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
- Zustand cho trạng thái client (phiên đăng nhập). Chọn Zustand thay Context vì store đọc ghi được **cả ngoài React** — `api-client` không phải component nên không dùng Context được, và việc tách đôi trạng thái đã từng gây lỗi thật (xem PHASE-1-SETUP.md)
- `react-hook-form` + `zod`
- WebSocket client tự viết, có auto-reconnect + exponential backoff

**Hạ tầng**
- Docker + Docker Compose v2 — toàn bộ dịch vụ chạy trong container ở mọi môi trường
- Nginx (container) làm reverse proxy, hỗ trợ nâng cấp WebSocket, kết thúc TLS
- Cloudflare R2 (tương thích S3) cho tệp đính kèm và ảnh đại diện
- GitHub Actions cho CI/CD, build và đẩy image lên GitHub Container Registry (GHCR)
- Prometheus + Grafana + Loki (container) cho giám sát và log

**Realtime media (Phase 7)**
- WebRTC (SRTP trên UDP) — truyền hình/tiếng, có sẵn trong trình duyệt, không phải cài gì
- LiveKit (container, viết bằng Go) — SFU cho gọi nhóm, kèm sẵn TURN server
- coturn — chỉ dựng riêng nếu sau này tách TURN khỏi LiveKit
- `livekit-client` + `@livekit/components-react` cho frontend

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
> `smoke-auth.sh` (34 mục bảo mật) và `smoke-hr.sh` (31 mục nghiệp vụ), đều đạt toàn bộ.
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
- [x] Đăng nhập hai bước: mã OTP 6 chữ số gửi qua email, lưu Redis (`AUTH_OTP_ENABLED`)

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

> **Trạng thái: đã xong.** Kiểm chứng bằng `scripts/smoke-project.sh`
> (57 mục, đạt toàn bộ), chạy trên stack Docker thật.
>
> Hai điểm đáng ghi nhớ, phát hiện khi nghiệm thu chứ không phải khi đọc lại code:
>
> - **Tham số enum phải ép kiểu tường minh trong SQL.** Câu `UPDATE` của
>   thao tác kéo-thả dùng `$2` ở hai chỗ: `status = $2` và `CASE WHEN $2 = 'todo'`.
>   PostgreSQL suy ra `$2` là `text` theo vế thứ hai, rồi vế thứ nhất hỏng vì
>   không có toán tử `task_status = text`. Viết `$2::task_status` ở MỌI chỗ.
> - **UUID toàn số 0 không phải là "không truyền".** `owner_id` kiểu `uuid.UUID`
>   trần khiến hai trường hợp đó trùng nhau, và nhánh "không truyền thì lấy
>   người tạo" âm thầm nuốt luôn một id sai. Đổi sang con trỏ để phân biệt.

### Backend
- [x] Migration: `projects`, `project_members`, `tasks`, `task_comments`, `task_attachments`, `task_activities`
- [x] Domain + usecase cho project và task
- [x] CRUD dự án; chỉ chủ dự án hoặc giám đốc được sửa/đóng
- [x] Thêm/xoá thành viên dự án, gán vai trò trong dự án (owner / member / viewer)
- [x] CRUD task: tiêu đề, mô tả, người thực hiện, độ ưu tiên, deadline, ước lượng giờ
- [x] Task con (`parent_task_id`), chặn lồng quá 2 cấp
- [x] Chuyển trạng thái task: `todo → in_progress → review → done`, chặn bước nhảy không hợp lệ
- [x] Sắp xếp thứ tự task trong cột Kanban (dùng số thực hoặc chuỗi lexo để chèn giữa)
- [x] Bình luận task, hỗ trợ `@mention` → sinh thông báo
- [x] Đính kèm tệp vào task (Cloudflare R2)
- [x] Ghi nhật ký thay đổi task (`task_activities`) — ai đổi gì, lúc nào
- [x] Ghi nhận thời gian làm việc theo task (timelog), phục vụ báo cáo
- [x] API báo cáo: tiến độ dự án, task quá hạn, khối lượng việc theo nhân viên
- [x] Phát sự kiện lên RabbitMQ khi: giao task, đổi trạng thái, sắp đến hạn, bị mention

### Frontend
- [x] Trang danh sách dự án (dạng thẻ + dạng bảng)
- [x] Trang tổng quan dự án: tiến độ, thành viên, task gần đây
- [x] Bảng Kanban kéo-thả (`dnd-kit`), cập nhật lạc quan (optimistic update)
- [x] Bảng danh sách task có lọc: người thực hiện, trạng thái, độ ưu tiên, deadline
- [x] Panel chi tiết task: mô tả, bình luận, tệp đính kèm, lịch sử thay đổi
- [x] Biểu đồ Gantt hoặc timeline đơn giản cho dự án
- [x] Trang "Việc của tôi" tổng hợp task xuyên dự án

---

## PHASE 3 — Chấm công & Nghỉ phép

**Mục tiêu:** Tự động ghi nhận thời gian làm việc dựa trên presence realtime, không cần bấm nút.

> **Trạng thái: đã xong.** Kiểm chứng bằng `scripts/smoke-attendance.sh`
> (48 mục, đạt toàn bộ, chạy lại được). Chuỗi hoàn chỉnh đã chứng minh trên
> stack thật: heartbeat WebSocket → presence Redis → job quét mỗi phút →
> `attendance_sessions`, với cơ chế nối phiện hoạt động đúng.
>
> Ba điểm đáng ghi nhớ:
>
> - **Ghi presence vào Redis, không ghi thẳng database.** Heartbeat đến 30
>   giây một lần từ mọi người online; với 200 nhân viên là 400 lượt ghi mỗi
>   phút để thu được đúng lượng thông tin mà một lần quét gom lại được.
> - **Job tổng hợp chạy cho MỌI nhân viên, không chỉ người có phiện.** Người
>   vắng không có phiện nào, nên duyệt bảng phiện thì họ không xuất hiện —
>   và "không có dòng" rất khác "có dòng ghi vắng" khi tính lương.
> - **Mọi phép đổi ngày đi qua múi giờ CÔNG TY.** Dùng giờ máy chủ sẽ đẩy
>   một phần ca làm sang sai ngày, và kế toán sẽ không công nhận con số.
>
> Còn lại có chủ ý: xuất báo cáo ra Excel — cần hạ tầng báo tiến độ của
> Phase 5, cùng lý do với việc nhập nhân viên từ Excel ở Phase 1.

> **Nguyên tắc thiết kế:** Kết nối WebSocket vốn đã có sẵn cho chat và thông báo được tận dụng làm nguồn dữ liệu chấm công. Khi nhân viên mở ứng dụng, client gửi heartbeat định kỳ; server ghi nhận phiên online và cộng dồn thời gian trong khung giờ làm việc.
>
> **Giới hạn cần ý thức:** mở tab không đồng nghĩa với đang làm việc. Vì vậy hệ thống phân biệt rõ *thời gian online* và *thời gian hoạt động* (có tương tác), đồng thời vẫn cho phép check-in thủ công và để quản lý duyệt/điều chỉnh. Không dùng dữ liệu này làm căn cứ kỷ luật tự động.

### Cơ chế presence
- [x] Client gửi heartbeat mỗi 30 giây qua WebSocket (kèm cờ `is_active`)
- [x] Phát hiện idle phía client: không có chuột/bàn phím > 5 phút → `is_active = false`
- [x] Bắt sự kiện `visibilitychange` — chuyển tab / thu nhỏ cửa sổ → đánh dấu không hoạt động
- [x] Server lưu presence vào Redis: `presence:{user_id}` với TTL 90 giây
- [x] Job nền quét Redis mỗi phút, ghi các khoảng online vào `attendance_sessions`
- [x] Gộp các phiên rời rạc cách nhau dưới 5 phút thành một phiên liền mạch
- [x] Job cuối ngày tổng hợp `attendance_sessions` → `attendance_days`
- [x] Xử lý đúng múi giờ công ty, không dùng giờ máy chủ trực tiếp

### Nghiệp vụ chấm công
- [x] Migration: `work_schedules`, `attendance_sessions`, `attendance_days`, `leave_requests`
- [x] Cấu hình khung giờ làm việc theo công ty / phòng ban / cá nhân
- [x] Tính các chỉ số theo ngày: giờ online, giờ hoạt động, đi muộn, về sớm, thiếu giờ
- [x] Check-in / check-out thủ công cho trường hợp ngoại lệ (mất mạng, họp ngoài)
- [x] Nhân viên gửi yêu cầu điều chỉnh công, quản lý duyệt
- [x] Luồng đơn nghỉ phép: tạo → quản lý duyệt/từ chối → trừ quỹ phép
- [x] Quản lý quỹ ngày phép năm, phép tồn
- [x] Đánh dấu ngày lễ, ngày nghỉ theo lịch công ty
- [x] API báo cáo chấm công: theo nhân viên, theo phòng ban, theo tháng
- [ ] Xuất báo cáo chấm công ra Excel (xử lý nền)

### Frontend
- [x] Widget trạng thái làm việc trên topbar: đang online, tổng giờ hôm nay
- [x] Trang chấm công cá nhân: dòng thời gian trong ngày, lịch tháng
- [x] Trang chấm công phòng ban (dành cho quản lý): ai đang online, ai nghỉ
- [x] Form gửi yêu cầu điều chỉnh công
- [x] Trang đơn nghỉ phép: tạo đơn, theo dõi trạng thái, số phép còn lại
- [x] Hàng đợi duyệt đơn cho quản lý
- [x] Thông báo rõ cho nhân viên rằng thời gian online đang được ghi nhận (minh bạch dữ liệu)

---

## PHASE 4 — Lương

**Mục tiêu:** Tạo bảng lương theo kỳ, sinh phiếu lương, xuất file.

> **Trạng thái: đã xong.** Kiểm chứng bằng `scripts/smoke-payroll.sh`
> (51 mục, đạt toàn bộ, chạy lại được) và 11 nhóm unit test cho máy tính
> lương — bộ test đầu tiên của dự án.
>
> Bốn điểm đáng ghi nhớ:
>
> - **Tiền lưu bằng BIGINT đơn vị đồng, không dùng số thực.** Lương Việt
>   Nam không có đơn vị nhỏ hơn đồng, và số thực tích luỹ sai số — bảng
>   lương lệch một đồng là bảng lương sai.
> - **Máy tính lương là hàm THUẦN KHIẾT.** Không chạm database, không đọc
>   đồng hồ. Đây là đoạn code kế toán sẽ chất vấn từng dòng, nên nó phải
>   đọc được như một công thức và kiểm chứng được bằng số cụ thể.
> - **Phiếu lương là ẢNH CHỤP, không phải khung nhìn tính lại.** Lương đã
>   trả thì con số phải đứng yên vĩnh viễn, kể cả khi cấu hình lương, biểu
>   thuế hay dữ liệu công thay đổi sau đó.
> - **Ghi nhật ký MỌI lượt XEM, không chỉ lượt sửa.** Rò rỉ bảng lương
>   thường là do đọc chứ không phải do ghi.
>
> **Phần còn lại cần bạn quyết:** sinh PDF Ở SERVER. Phiếu lương hiện là
> một tài liệu HTML hoàn chỉnh có CSS in ấn — "In / Lưu thành PDF" của trình
> duyệt cho ra PDF đúng như nhìn thấy, và cùng HTML đó dùng luôn làm nội dung
> email. Sinh PDF ở server cần NHÚNG một font TTF hỗ trợ Latin Extended
> Additional (ạ ả ấ ầ...); bộ font lõi của PDF chỉ có Latin-1 và mọi dấu
> tiếng Việt sẽ thành ô vuông. Thêm một tệp font vào repo là quyết định về
> giấy phép và dung lượng, nên nó phải là lựa chọn có ý thức của chủ dự án.

### Backend
- [x] Migration: `salary_structures`, `payroll_periods`, `payslips`, `payslip_items`
- [x] Cấu hình lương theo nhân viên, có hiệu lực theo khoảng thời gian (lịch sử tăng lương)
- [x] Định nghĩa thành phần lương: lương cơ bản, phụ cấp, thưởng, khấu trừ, BHXH, thuế TNCN
- [x] Bảng thuế TNCN luỹ tiến cấu hình được (không hard-code)
- [x] Tạo kỳ lương, lấy dữ liệu công từ `attendance_days`
- [x] Máy tính lương: chạy qua từng nhân viên, sinh `payslips` + `payslip_items`
- [x] Chạy tính lương trong worker RabbitMQ (kỳ lương lớn không được chặn HTTP request)
- [x] Vòng đời kỳ lương: `draft → locked → paid`; đã khoá thì không sửa được
- [ ] Sinh phiếu lương PDF ở server (xem ghi chú trạng thái)
- [x] Gửi phiếu lương qua email (hàng đợi worker)
- [x] Kiểm soát truy cập nghiêm ngặt: chỉ HR, kế toán, giám đốc và chính chủ xem được
- [x] Ghi `audit_logs` cho mọi thao tác xem/sửa dữ liệu lương
- [x] Mã hoá hoặc hạn chế hiển thị thông tin tài khoản ngân hàng

### Frontend
- [x] Trang cấu hình lương nhân viên (chỉ HR)
- [x] Trang danh sách kỳ lương, tạo kỳ mới
- [x] Bảng lương chi tiết theo kỳ, có thể sửa khi còn `draft`
- [x] Trang phiếu lương cá nhân, tải PDF
- [x] Biểu đồ chi phí nhân sự theo phòng ban / theo tháng

---

## PHASE 5 — Realtime: Thông báo & Chat

**Mục tiêu:** Chat 1-1 và chat nhóm hoạt động ổn định, thông báo đẩy tức thì, hoạt động đúng khi chạy nhiều instance backend.

### Hạ tầng WebSocket

> **Trạng thái: đã xong**, làm trước để Phase 3 dùng được presence. Kiểm
> chứng fan-out bằng hai instance api thật: client nối vào instance 2 nhận
> được bản tin phát từ instance 1.
>
> Bản đồ `user_id → instance_id` nằm trong chính bản ghi presence trên
> Redis (trường `instance_id` của mỗi kết nối). Việc GIAO bản tin thì dùng
> fan-out cho mọi instance rồi lọc tại chỗ, thay vì định tuyến theo bản đồ:
> định tuyến cần bản đồ luôn đúng, và một instance bị `kill -9` sẽ để lại
> bản đồ cũ trỏ tới nơi không còn ai — bản tin biến mất trong im lặng.

- [x] Hub WebSocket: quản lý client, đăng ký/huỷ đăng ký, broadcast
- [x] Xác thực khi bắt tay WebSocket (token qua query hoặc subprotocol, không qua cookie)
- [x] Một người dùng có thể mở nhiều thiết bị — hub phải hỗ trợ nhiều kết nối / 1 user
- [x] Cơ chế ping/pong, tự đóng kết nối chết
- [x] Định dạng bản tin chuẩn: `{ type, payload, ts, trace_id }`
- [x] Fan-out qua RabbitMQ: mỗi instance backend là một consumer, exchange kiểu fanout
- [x] Redis lưu bản đồ `user_id → instance_id` để định tuyến tin nhắn
- [x] Giới hạn tốc độ gửi tin nhắn mỗi kết nối (chống spam)
- [x] Giới hạn kích thước bản tin, đóng kết nối khi vượt ngưỡng

### Thông báo

> **Trạng thái: đã xong.** Nghiệm thu bằng `scripts/smoke-chat.sh` (66 mục,
> chung với phần chat). Chuỗi đầy đủ đã chứng minh trên stack thật:
> api → RabbitMQ → worker → bảng `notifications` → RabbitMQ fan-out →
> hub của instance đang giữ người nhận → WebSocket.
>
> Hai điểm đáng ghi lại:
>
> - **Thông báo cố ý KHÔNG có quyền nào.** Mỗi người chỉ đọc được của chính
>   mình, usecase khoá cứng theo actor và điều kiện `employee_id` nằm ngay
>   trong câu `UPDATE` đánh dấu đã đọc. Thêm một mã quyền ở đây chỉ tạo ảo
>   giác rằng có thể cấp nó cho người khác.
> - **Bản tin realtime và dữ liệu REST dùng CHUNG một kiểu Go có json tag.**
>   Lần đầu viết, usecase đẩy thẳng entity domain (không tag) nên client nhận
>   được `{"ID":...}` thay vì `{"id":...}` và im lặng bỏ qua. Lỗi này không
>   làm hỏng bất cứ phép thử REST nào — chỉ lộ ra khi nối một client
>   WebSocket thật và đọc bản tin.

- [x] Migration: `notifications`
- [x] Danh mục loại thông báo: giao task, mention, duyệt đơn, đến hạn, tin nhắn mới
- [x] Usecase thông báo: ghi DB + đẩy WebSocket nếu online
- [x] Đánh dấu đã đọc / đọc tất cả
- [x] Trang danh sách thông báo, có phân trang vô hạn
- [x] Chuông thông báo hiển thị số chưa đọc, cập nhật realtime
- [x] Cấu hình nhận thông báo theo loại (bật/tắt từng loại)
- [x] Gửi email cho thông báo quan trọng khi người dùng offline quá 15 phút

### Chat

> **Trạng thái: đã xong.** Nghiệm thu bằng `scripts/smoke-chat.sh` (66 mục).
> Đã kiểm chứng trên hai instance api thật: client nối vào instance 2 nhận
> được cả tin nhắn lẫn chỉ báo "đang nhập" phát từ instance 1.
>
> Những chỗ đáng ghi lại:
>
> - **`direct_key` là hai id đã SẮP XẾP rồi ghép.** Không sắp xếp thì (A,B)
>   và (B,A) ra hai khoá, hai người cùng bấm "nhắn tin" một lúc tạo ra hai
>   hội thoại song song, và mỗi người chỉ thấy một nửa lịch sử.
> - **Gửi tin đi qua WebSocket là NGOẠI LỆ có cân nhắc** với nguyên tắc
>   "nghiệp vụ đi qua REST" của hạ tầng WebSocket. Chỉ ba thao tác được đi
>   lối này — gửi tin, đang nhập, đã đọc — vì chúng nhạy cảm với độ trễ và
>   xảy ra liên tục. Quyền vẫn kiểm tra y như REST: Actor dựng một lần lúc
>   bắt tay và `chat:read` được kiểm ở mọi bản tin, nếu không WebSocket
>   thành cửa sau đi vòng qua toàn bộ phân quyền.
> - **Nhóm phòng ban / dự án đồng bộ bằng job định kỳ**, không móc vào từng
>   thao tác thêm-bớt thành viên. Móc vào từng thao tác nghĩa là mỗi module
>   phải nhớ gọi chat, và chỉ cần một đường quên gọi là nhóm lệch vĩnh viễn
>   mà không ai biết. Job tự sửa mọi sai lệch, dù chúng đến từ đâu.
>   Nguồn rỗng thì BỎ QUA: một phòng ban tạm thời không còn ai gần như luôn
>   là dữ liệu đang dở, và đồng bộ theo nó sẽ xoá sạch thành viên khỏi nhóm.
> - **Không phải thành viên thì trả 404, không phải 403.** 403 xác nhận hội
>   thoại đó có tồn tại, và với chat thì chính sự tồn tại của một cuộc trò
>   chuyện đã là thông tin không nên rò ra.
> - **Thu hồi tin nhắn xoá luôn `content`**, không chỉ đặt `deleted_at`.
>   Xoá mềm là để giữ dấu vết cho quản trị, không phải để nội dung vẫn nằm
>   nguyên trong database và rò ra qua một đường đọc nào đó quên kiểm tra cờ.

- [x] Migration: `conversations`, `conversation_members`, `messages`, `message_attachments`
- [x] Tạo hội thoại 1-1 — tự động tái sử dụng nếu đã tồn tại giữa 2 người
- [x] Tạo nhóm chat: đặt tên, thêm thành viên, phân quyền quản trị nhóm
- [x] Tạo nhóm tự động theo phòng ban hoặc theo dự án
- [x] Gửi tin nhắn qua WebSocket, kèm `client_message_id` để chống trùng
- [x] Lưu tin nhắn bền vững vào PostgreSQL trước khi phát đi
- [x] Tải lịch sử tin nhắn theo cursor (cuộn ngược lên)
- [x] Trạng thái đã đọc: cập nhật `last_read_message_id`, hiển thị số chưa đọc
- [x] Chỉ báo "đang nhập..." (throttle, không lưu DB)
- [x] Trạng thái online/offline/lần cuối hoạt động (dùng chung dữ liệu presence của Phase 3)
- [x] Trả lời tin nhắn (reply), sửa và thu hồi tin nhắn (soft delete)
- [x] Gửi tệp và ảnh (Cloudflare R2), hiển thị xem trước ảnh
- [x] Tìm kiếm tin nhắn trong hội thoại (PostgreSQL full-text search)
- [x] Ghim hội thoại, tắt thông báo hội thoại

> **Chưa nghiệm thu được trên môi trường dev:** phần tải tệp và ảnh. Mã đã
> xong ở cả hai đầu (ký URL PUT/GET, tải thẳng lên R2 không qua api, xem
> trước ảnh), nhưng `.env` hiện chưa có khoá Cloudflare R2 — api khởi động
> với cảnh báo "chưa cấu hình Cloudflare R2". Cấu hình khoá xong là dùng
> được ngay, dùng chung đúng lớp lưu trữ với ảnh đại diện và tệp công việc.

### Frontend chat
- [x] Layout chat: danh sách hội thoại + khung tin nhắn
- [x] `ws-client` tự kết nối lại, backoff tăng dần, xếp hàng tin nhắn khi mất mạng
- [x] Hiển thị tin nhắn lạc quan (gửi → hiện ngay → xác nhận hoặc báo lỗi)
- [x] Cuộn vô hạn ngược lên để tải lịch sử
- [x] Bong bóng chat nổi, mở nhanh từ mọi trang
- [x] Giao diện quản lý nhóm: thêm/xoá thành viên, đổi tên, rời nhóm
- [x] Đồng bộ trạng thái đọc giữa nhiều tab (BroadcastChannel)

---

## PHASE 6 — Hoàn thiện & Vận hành

### Chất lượng

> **Trạng thái: XONG.** Trước Phase 6 cả dự án có 3 tệp test (501 dòng, chỉ
> phủ máy tính lương). Nay có bốn tầng kiểm thử: unit, integration trên
> PostgreSQL thật, API end-to-end, và trình duyệt thật.
>
> **Việc kiểm thử đã tìm ra 11 lỗi thật**, và phần lớn không bắt được bằng
> cách đọc mã. Đáng nhớ nhất:
>
> | Lỗi | Bộ nào tìm ra |
> |---|---|
> | CSP chặn script Next.js — **không ai đăng nhập được qua nginx** | Playwright |
> | Đường sao lưu — khôi phục **chưa bao giờ chạy được** (3 lỗi) | Diễn tập khôi phục |
> | Công ty mới **không tính được lương** | e2e từ cài đặt sạch |
> | nginx không nhận bản api mới sau khi scale hoặc triển khai | Kiểm chứng scale |
> | Migration không tự tạo extension — CI chưa bao giờ chạy được | testcontainers |
> | Nhãn form không gắn với ô nhập (trợ năng) | Playwright |
> | `Session.Minutes()` trả số phút âm | Unit test |
> | `sanitizeName` giữ nguyên `..` trong tên tệp | Unit test |
> | Bản giả lập nói sai hợp đồng `ListAncestorIDs` | Integration test |
>
> Coverage hiện tại:
>
> | Gói | Coverage | | Gói | Coverage |
> |---|---|---|---|---|
> | `usecase/system` | 100% | | `delivery/http/router` | 95% |
> | `usecase/payroll` | 91% | | `domain/project` | 90% |
> | `usecase/attendance` | 87% | | `domain/notification` | 86% |
> | `usecase/hr` | 82% | | `domain/auth` | 82% |
> | `usecase/project` | 81% | | `domain/hr` | 77% |
> | `usecase/notification` | 77% | | `domain/chat` | 73% |
> | `usecase/chat` | 75% | | `domain/attendance` | 67% |
> | `usecase/auth` | 74% | | `domain/payroll` | 18% |
>
> **Cả tám gói usecase đều ≥ 70%.** `domain/payroll` thấp vì phần lớn tệp
> entity là khai báo kiểu và hằng số; phép tính thật nằm ở `usecase/payroll`
> và được phủ tới 91%.
>
> Các bộ kiểm thử không đuổi theo con số coverage mà nhắm vào **những luật
> mà sai thì không có triệu chứng gì**: phạm vi dữ liệu, cắt phiên khi đổi
> quyền, trừ và hoàn quỹ phép, biểu thuế bị hở, thứ tự đóng khoảng hiệu lực
> lương. Mỗi luật đều có thêm một phép thử **phía ngược lại**, vì một bộ chỉ
> kiểm phía "từ chối" sẽ vẫn đạt với một hàm từ chối tất cả.

- [x] Unit test cho toàn bộ tầng `usecase` (mục tiêu ≥ 70% coverage)
- [x] Integration test cho repository bằng testcontainers (PostgreSQL thật)
> 15 phép thử, chạy bằng `go test -tags=integration
> ./internal/repository/postgres/...`. Build tag là cần thiết: `go test ./...`
> phải chạy được trên máy không có Docker và xong trong vài giây.
>
> Container chạy **chính** các tệp migration và **chính** tệp init extension
> của dự án, không dựng schema riêng cho kiểm thử — một schema riêng sẽ
> trôi khỏi schema thật mà không ai nhận ra.
>
> Chỉ kiểm những thứ bản giả lập không kiểm được: truy vấn WITH RECURSIVE
> trên cây phòng ban, chỉ mục unique MỘT PHẦN (nhân viên nghỉ rồi quay lại
> phải dùng được email cũ), hành vi xoá mềm, tìm kiếm tiếng Việt không dấu
> qua unaccent, và phạm vi dữ liệu ở tầng SQL.
>
> **Nó tìm ra ngay một bản giả lập nói sai hợp đồng**: `ListAncestorIDs`
> thật trả về chính nút đó kèm tổ tiên, còn bản giả lập ở tầng usecase trả
> tổ tiên chặt. Luật nghiệp vụ không đổi theo, nhưng bản giả lập đã được
> sửa cho khớp — đó chính là loại lệch mà integration test sinh ra để bắt.
- [x] Test API end-to-end cho các luồng chính, tự động trong CI
> `scripts/ci-e2e.sh` dựng stack sạch, seed, rồi chạy cả 6 bộ smoke — **286
> phép kiểm, đạt toàn bộ từ một cài đặt hoàn toàn mới**. Job `e2e` trong
> ci.yml chỉ gọi script đó.
>
> Gói vào script chứ không viết thành các bước YAML: bước YAML chỉ chạy
> được trên runner, nên mỗi lần sửa là một lần đẩy commit rồi chờ xem CI đỏ
> hay xanh. Script thì chạy được ngay ở máy — và chính nhờ vậy nó đã tìm
> ra ba lỗi trước khi chạm tới CI.
- [x] Test tải cho WebSocket (mục tiêu: 500 kết nối đồng thời)
> Đo trên stack thật bằng `backend/cmd/wsload`: **500/500 kết nối thành
> công, 0 thất bại, 0 rớt giữa chừng**, giữ 60 giây. Bắt tay p50 2,7 ms —
> p95 4,2 ms — p99 21,2 ms. Chỉ số phía máy chủ khớp chính xác:
> `manage_ws_connections` đạt đúng 500, `manage_ws_dropped_total` bằng 0.
- [x] Test E2E frontend bằng Playwright cho 5 luồng quan trọng nhất
> 8 phép thử phủ năm luồng: đăng nhập (kể cả bước nhập mã qua MailHog),
> chấm công, thêm nhân viên, tạo dự án, và gửi tin nhắn chat. **8/8 đạt.**
> Chạy bằng `pnpm e2e` trong thư mục frontend, job `playwright` trong ci.yml
> dùng lại đúng phần chuẩn bị của bộ e2e.
>
> **Bộ này tìm ra lỗi nghiêm trọng nhất của Phase 6**: header CSP trong nginx
> chặn script nội tuyến của Next.js, React không hydrate, và MỌI trang thành
> HTML tĩnh khi đi qua nginx — không ai đăng nhập được. Chi tiết trong
> `frontend/src/middleware.ts`.
>
> Những bộ kiểm khác không thể thấy lỗi đó: API vẫn trả 200, nginx vẫn trả
> 200, trang vẫn hiện đúng bố cục, không dòng lỗi nào trong log. Chỉ một
> trình duyệt thật BẤM vào nút mới phát hiện ra.

### Bảo mật

> **Việc rà soát endpoint nay là một BỘ TEST, không phải một lần rà bằng mắt.**
> `router_test.go` liệt kê mọi route chi đang phục vụ rồi bắn request thật:
> không token phải 401 (125 endpoint), token hợp lệ mà không có quyền phải 403
> (114 endpoint). Thêm route mà quên middleware là làm test đỏ ngay.
>
> Nó đã chứng minh tác dụng trong chính Phase 6: bắt được `/metrics` ngay khi
> endpoint đó được thêm vào.
>
> Hai danh sách trắng nằm trong tệp test, mỗi dòng kèm lý do, cùng một phép
> thử chống danh sách trắng lỗi thời.

- [x] Rà soát toàn bộ endpoint: mọi route đều có kiểm tra quyền, không sót route công khai
- [x] Chống IDOR: luôn kiểm tra quyền trên bản ghi cụ thể, không chỉ trên loại tài nguyên
- [x] Ngăn SQL injection (chỉ dùng tham số hoá), XSS (escape đầu ra), CSRF
- [x] Kiểm tra kiểu tệp tải lên bằng magic bytes, giới hạn dung lượng
- [x] Đặt security header: HSTS, CSP, X-Frame-Options
- [x] Quản lý secret bằng biến môi trường, tuyệt đối không commit vào git
- [x] Ghi audit log cho thao tác nhạy cảm: lương, xoá nhân viên, đổi quyền
- [x] Bảo mật container: chạy non-root, `read_only: true` cho container không cần ghi, `cap_drop: ALL`, `no-new-privileges`
- [x] Không nhúng secret vào image (kiểm tra bằng `docker history`); dùng Docker secrets hoặc file env ngoài repo
- [x] Ghim phiên bản base image theo digest, không dùng tag trôi nổi như `alpine:latest`
> Ba image đã ghim: `golang:1.27-alpine`, `alpine:3.21`, `node:22-alpine`.
> Giữ cả thẻ lẫn digest — Docker dùng digest, thẻ để người đọc biết nó ứng
> với phiên bản nào.
>
> **Ghim rồi bỏ quên còn tệ hơn thẻ trôi nổi**: image đóng băng luôn cả
> những lỗ hổng đã được vá ở thượng nguồn, trong khi thẻ trôi nổi ít ra còn
> tự nhận bản vá. Vì vậy kèm `scripts/update-base-digests.sh`: chế độ mặc
> định chỉ xem, `--write` mới ghi. Script cố ý KHÔNG tự commit và không
> chạy trong CI — đổi base image có thể đổi phiên bản thư viện hệ thống, và
> có những thứ chỉ hỏng lúc chạy chứ không hỏng lúc build.
- [x] Quét image định kỳ bằng Trivy, có lịch cập nhật base image khi có CVE mới → `.github/workflows/security.yml`, 08:00 thứ hai hằng tuần

### Docker — production

> Phần lớn mục ở đây đã làm từ Phase 0 (Dockerfile nhiều stage, non-root,
> hạn mức tài nguyên, xoay vòng log). Phase 6 thêm TLS tự gia hạn, container
> sao lưu, và ba script triển khai — quay lui — khôi phục.
>
> **Đã chạy thử trên stack thật.** `nginx -t` đạt cho cả cấu hình dev lẫn
> cấu hình production (phải chạy trong mạng `manage_proxy` vì upstream `api`
> và `frontend` phân giải bằng DNS của Docker). `promtool check config` đạt,
> 13 quy tắc cảnh báo. 15 service lên đủ, 6/6 target Prometheus xanh, cả 13
> quy tắc nạp đúng và không quy tắc nào kêu oan.
>
> Việc chạy thật tìm ra ba lỗi mà kiểm cú pháp không bắt được: node-exporter
> không dựng được trên Docker Desktop vì bind propagation, lệnh bật giám sát
> trong tài liệu làm hỏng api vì bỏ mất override, và nginx không nhận bản api
> mới sau khi scale hoặc triển khai.

- [x] Hoàn thiện target `prod` trong Dockerfile backend: build tĩnh (`CGO_ENABLED=0`), `-ldflags="-s -w"`, base image `gcr.io/distroless/static` hoặc `alpine`
- [x] Hoàn thiện Dockerfile frontend: chỉ copy `.next/standalone` + `.next/static` + `public` sang stage cuối
- [x] Tạo user không phải root trong cả hai image, khai báo `USER app`
- [x] Nhúng thông tin build vào binary (version, git SHA, build time) và trả ra ở `/health` — *và cả ở `/metrics` qua `manage_build_info`*
- [x] `docker-compose.prod.yml`: kéo image từ GHCR theo tag, `restart: unless-stopped`
- [x] Đặt `deploy.resources.limits` CPU/RAM cho từng service, tránh một container ăn hết máy
- [x] Cấu hình log driver `json-file` kèm `max-size` và `max-file` (không để log phình đầy ổ đĩa)
- [x] `stop_grace_period: 60s` cho api để đóng sạch kết nối WebSocket khi deploy
- [x] Nginx: bật TLS bằng Let's Encrypt (certbot container hoặc Caddy), tự gia hạn — *chưa chạy thử; lần cấp chứng chỉ đầu cần domain thật, xem doc/OPERATIONS.md*
- [x] Nginx: bật gzip/brotli, cache asset tĩnh, đặt `client_max_body_size` khớp giới hạn upload
- [x] Kiểm chứng scale: `--scale api=3` → chat và thông báo vẫn đúng (fan-out RabbitMQ)
> Ba kết quả đo trên stack thật:
>
> - **Lưu lượng REST chia đều**: 30 request → 10/10/10 trên ba bản.
> - **Kết nối WebSocket chia đều**: 30 kết nối → 10/10/10.
> - **Fan-out đúng**: 6 kết nối rải trên ba bản, gửi một tin nhắn bằng REST
>   qua một bản → **6/6 kết nối nhận được**.
>
> Công cụ kiểm chứng lặp lại được: `backend/cmd/wsfanout`. Nó tự mở hội
> thoại, gắn một mốc duy nhất vào tin nhắn rồi đếm số kết nối nhận được, và
> thoát với mã khác 0 khi thiếu dù một kết nối.
>
> **Hai lỗi chặn đường đã phải sửa trước đó** — xem `docker-compose.scale.yml`
> và ghi chú upstream trong `docker/nginx/conf.d/app.conf`: cổng cố định
> trong override chặn bản thứ hai khởi động, và nginx không tự tra lại DNS
> nên bản mới không nhận được request nào cho tới khi nạp lại cấu hình.
- [x] Script deploy: pull image mới → chạy job `migrate` → rolling restart từng container api
- [x] Quy trình rollback: đổi tag image về SHA trước đó và khởi động lại
- [x] Container backup: `pg_dump` theo lịch, nén, đẩy lên Cloudflare R2, xoá bản cũ theo chính sách lưu trữ — *tự kiểm tra bản dump đọc được; phần đẩy lên R2 cần khoá R2 chưa có*
- [x] Diễn tập khôi phục, ghi lại thời gian thực tế — *đã diễn tập trên máy dev; lần trên MÁY SẠCH vẫn cần làm để có RTO thật*
> **Lần diễn tập đầu tiên phát hiện đường sao lưu — khôi phục chưa bao giờ
> chạy được**: ba lỗi trong kịch bản, không lỗi nào lộ ra nếu chỉ đọc mã.
> Chi tiết và phân rã thời gian: doc/OPERATIONS.md, mục "Diễn tập khôi phục".
> Sau khi sửa: sao lưu 1 giây, khôi phục **37 giây**, đối chiếu 39/39 bảng
> khớp số dòng với bản gốc.

### Giám sát

> **Chưa chạy thử trên stack thật** (Docker Desktop tắt). Cấu hình đã rà cú
> pháp; đã tự sửa hai lỗi khi đối chiếu schema Prometheus: khoá đúng là
> `alertmanagers` chứ không phải `alertmanager_configs` (Prometheus sẽ từ chối
> khởi động), và một khối `relabel_configs` không làm gì.

- [x] Endpoint `/metrics` ở backend (Prometheus client): số kết nối WebSocket, độ trễ request, độ sâu hàng đợi
- [x] Stack giám sát bằng container: prometheus, grafana, loki, promtail, cadvisor, node-exporter
- [x] Dashboard Grafana định nghĩa sẵn dưới dạng file (provisioning), commit vào repo
- [x] Cảnh báo: container restart liên tục, ổ đĩa > 80%, hàng đợi RabbitMQ ứ đọng, số kết nối WS tụt đột ngột
- [x] Log tập trung qua Loki, có `trace_id` xuyên suốt request
- [x] Runbook xử lý sự cố thường gặp (kèm lệnh docker cụ thể cho từng tình huống)

### Tài liệu
- [x] Sinh tài liệu OpenAPI/Swagger cho REST API → `doc/openapi.yaml` (132 operation, 45 schema)

> Đặc tả được **đối chiếu tự động** với router bằng ba phép thử trong
> `openapi_test.go`: thiếu endpoint, thừa endpoint, và endpoint công khai phải
> khai `security: []`. Tài liệu API rời khỏi thực tế là chuyện xảy ra âm thầm —
> thêm một endpoint thì nhớ, nhưng cập nhật một file YAML 3800 dòng thì quên.
- [x] Tài liệu giao thức WebSocket: danh sách sự kiện và payload → `doc/WEBSOCKET.md`
- [x] Sơ đồ ERD cơ sở dữ liệu → `doc/ERD.md` (37 bảng, kèm lý do của các quyết định schema)
- [x] Hướng dẫn cài đặt môi trường dev (chỉ cần Docker, không cần cài Go/Node trên máy) → `README.md` + `doc/PHASE-1-SETUP.md`
- [x] Tài liệu vận hành Docker: danh sách service, biến môi trường, lệnh thường dùng → `doc/OPERATIONS.md`, kèm `doc/RUNBOOK.md` để xử lý sự cố
- [x] Sổ tay hướng dẫn sử dụng cho người dùng cuối → `doc/HUONG-DAN-SU-DUNG.md`
> Viết cho nhân viên, không cho lập trình viên: mỗi mục bắt đầu bằng **việc
> người dùng muốn làm**, không bằng tên chức năng. Phần chấm công đặt lên đầu
> vì đó là thứ ảnh hưởng tới lương và là nơi hiểu nhầm gây thiệt hại thật —
> đóng tab là ngừng ghi nhận, và không ai đoán được điều đó nếu không nói ra.

---

## PHASE 7 — Gọi video & Trình chiếu màn hình

**Mục tiêu:** Gọi video 1-1 và gọi nhóm ngay trong hội thoại chat, kèm trình chiếu màn hình. Chạy được cả trên mạng công ty chặn UDP.

> **Đọc trước khi code:** WebSocket **không** truyền được media. Nó chạy trên TCP, mà TCP đảm bảo thứ tự — mất một gói là cả luồng phía sau phải chờ gửi lại, video đứng hình vài giây thay vì lướt qua một frame xấu. Media thời gian thực cần UDP và chấp nhận mất gói: đó là **WebRTC**.
>
> Hub WebSocket của Phase 5 vẫn dùng, nhưng đổi vai: nó là kênh **signaling** — nơi hai bên trao đổi SDP và ICE candidate để bắt tay. Bắt tay xong, media đi thẳng qua WebRTC, **không qua nginx, không qua hub**.
>
> Phụ thuộc: Phase 5 (hub WebSocket) và phần TLS của Phase 6 — xem mục Hạ tầng để biết vì sao TLS thành bắt buộc.

### Quyết định chốt trước khi code

| Vấn đề | Lựa chọn | Lý do |
|---|---|---|
| Truyền media | WebRTC (SRTP trên UDP) | Cách duy nhất khả thi cho realtime trên trình duyệt |
| Signaling | Hub WebSocket có sẵn của Phase 5 | Không dựng thêm hạ tầng; đã có xác thực và định tuyến theo user |
| Gọi nhóm | **SFU**, không dùng mesh | Mesh N người = mỗi máy gửi N-1 luồng. Từ 5 người trở lên, đường upload của nhân viên không tải nổi |
| Gọi 1-1 | P2P trực tiếp, SFU làm dự phòng | Không tốn băng thông server khi đục được NAT |
| Phần mềm SFU | **LiveKit** (self-host bằng container) | Viết bằng Go cùng stack backend; kèm sẵn TURN; hỗ trợ UDP mux một cổng; có SDK JS/React |
| TURN | Bắt buộc, kèm fallback TCP/443 | Firewall công ty chặn UDP tuỳ tiện — thiếu TURN thì một phần nhân viên không gọi được |
| Ghép hình phía server (MCU) | **Không làm** | Phải transcode, tốn CPU khủng khiếp. Chỉ cân nhắc nếu sau này cần ghi hình ghép hoặc đẩy RTMP |

### Trạng thái hiện tại

> **Đã xong phần lõi backend, chưa nối dây và chưa có giao diện.**
>
> | Tầng | Trạng thái |
> |---|---|
> | Migration `calls`, `call_participants`, quyền | xong |
> | Domain `internal/domain/call` | xong |
> | Usecase `internal/usecase/call` | xong — **40 unit test, 81.2% coverage** |
> | Repository PostgreSQL | xong — **14 integration test trên database thật** |
> | Adapter LiveKit (SFU + TURN) | **chưa làm** |
> | Container LiveKit trong compose | **chưa làm** |
> | Endpoint HTTP và route | **chưa làm** |
> | Nối vào hub WebSocket | **chưa làm** |
> | Job dọn cuộc gọi quá hạn ở worker | **chưa làm** |
> | Giao diện | **chưa làm** |
>
> Những mục bên dưới đã có đủ luật nghiệp vụ nhưng **chưa gọi tới được**
> vì thiếu route và chưa nối hub. Chúng vẫn để trống để không nói quá sự thật.

### Quyết định đã chốt

| Vấn đề | Chốt | Lý do |
|---|---|---|
| Người nhận đang bận cuộc khác | **Từ chối ngay, báo "đang bận"** | Người gọi biết ngay thay vì chờ 45 giây; người đang họp không bị làm phiền; ít trạng thái phải quản lý nhất |
| Ghi hình cuộc gọi | **Chưa làm** | Đặc tả ghi là tuỳ chọn; nó kéo theo container Egress, khoá R2, chính sách lưu trữ, màn hình xin đồng ý và audit log — một khối riêng |

### Signaling — mở rộng hub WebSocket của Phase 5
- [x] Bổ sung nhóm bản tin `call.*` vào định dạng chuẩn `{ type, payload, ts, trace_id }`
- [ ] `call.invite` / `call.accept` / `call.reject` / `call.cancel` / `call.end`
- [ ] `call.sdp` và `call.ice` — chuyển tiếp SDP offer/answer và ICE candidate giữa hai đầu
- [ ] Định tuyến bản tin gọi qua bản đồ `user_id → instance_id` trên Redis (dùng lại cơ chế Phase 5)
- [ ] Đổ chuông trên **mọi thiết bị** của người nhận; ai bắt máy trước thì các thiết bị còn lại nhận `call.cancelled`
- [ ] Tự huỷ cuộc gọi nếu không ai bắt máy sau 45 giây
- [ ] Kiểm tra quyền khi mời: chỉ thành viên hội thoại mới gọi được vào hội thoại đó
- [ ] Người gọi rớt mạng giữa chừng: hub phát `call.end` khi kết nối WS đóng, không để cuộc gọi treo
- [x] Chốt hành vi khi người nhận đang bận cuộc khác — **từ chối ngay**, xem bảng quyết định ở trên

### Migration & lưu trữ
- [x] Migration: `calls` — hội thoại, kiểu, người khởi tạo, mốc bắt đầu/kết thúc, lý do
> Kèm chỉ mục một phần `uq_calls_active_per_conversation`: mỗi hội thoại chỉ
> được có MỘT cuộc gọi đang diễn ra. Không có nó thì hai người cùng bấm gọi
> trong một giây sẽ tạo hai phòng, mỗi người vào một phòng, và cả hai ngồi
> nhìn màn hình trống.
- [x] Migration: `call_participants` — ai vào, vào lúc nào, rời lúc nào, có bật mic/camera/chia sẻ màn hình không
- [x] Sinh tin nhắn hệ thống khi cuộc gọi kết thúc ("Cuộc gọi video · 12 phút") — dùng lại bảng `messages`
- [ ] Thống kê vận hành: số cuộc gọi, thời lượng trung bình, **tỉ lệ phải relay qua TURN** (chỉ số quyết định chi phí băng thông)

### TURN / NAT traversal — làm SỚM, đừng để cuối phase
- [ ] Dựng TURN server (dùng bản tích hợp trong LiveKit, hoặc coturn riêng nếu muốn tách)
- [ ] Xác thực TURN bằng credential tạm thời (HMAC theo thời gian). **Không dùng user/pass tĩnh** — lộ ra là bị dùng chùa băng thông
- [ ] `GET /api/v1/calls/ice-servers` — cấp credential TURN ngắn hạn (TTL khoảng 10 phút) cho người đã đăng nhập
- [ ] Bật listener TURN trên **TCP cổng 443** làm đường cuối cho mạng chặn UDP
- [ ] Kiểm chứng trên **mạng 4G**, **mạng công ty** và **máy sau VPN** — ba môi trường này hỏng theo ba kiểu khác nhau
- [ ] Đo và ghi log tỉ lệ kết nối phải đi qua TURN

### SFU — gọi nhóm
- [ ] Dựng LiveKit bằng container, đặt khoá API riêng, không để khoá mặc định
- [ ] Backend cấp **access token** vào phòng: nhúng `room`, `identity`, quyền publish/subscribe, TTL ngắn
- [ ] `POST /api/v1/calls/{id}/token` — kiểm tra người gọi là thành viên hội thoại rồi mới phát token. **Không để client tự chọn phòng**
- [ ] Đặt tên phòng theo `call_id`, không theo `conversation_id` (một hội thoại có nhiều cuộc gọi theo thời gian)
- [ ] Bật **simulcast**: mỗi người gửi 3 mức 180p/360p/720p, server chọn mức phù hợp cho từng người nhận
- [ ] Chỉ hiện người đang nói ở độ nét cao, phần còn lại hạ xuống 180p
- [ ] Giới hạn số người mỗi phòng (đề xuất khởi điểm: 16) và số phòng chạy song song
- [ ] Phát hiện người đang nói (active speaker) để làm nổi khung trên giao diện
- [ ] Webhook từ SFU về backend: người vào/rời phòng, phòng đóng → cập nhật `call_participants`
- [ ] Dọn phòng rác: tự đóng phòng không còn ai sau 30 giây

### Gọi 1-1
- [ ] Đi P2P trực tiếp khi đục được NAT, tự chuyển qua TURN/SFU khi thất bại
- [ ] Khởi tạo `RTCPeerConnection` với danh sách ICE lấy từ `/calls/ice-servers`
- [ ] Xử lý **renegotiation** khi bật/tắt camera hoặc thêm luồng màn hình giữa cuộc gọi
- [ ] Xử lý **ICE restart** khi đổi mạng (WiFi sang 4G) thay vì để rớt cuộc gọi
- [ ] Gọi thoại (chỉ audio) dùng chung luồng này, khác ở chỗ không xin quyền camera

### Trình chiếu màn hình
- [ ] `getDisplayMedia()` — thêm track màn hình vào cuộc gọi **đang chạy**, không mở cuộc gọi mới
- [ ] Đặt `contentHint`: `text` cho màn hình tĩnh (ưu tiên nét chữ), `motion` khi chiếu video. Khác biệt rõ rệt, đừng bỏ qua
- [ ] Giới hạn bitrate riêng cho luồng màn hình: 1080p chữ tĩnh khoảng 0.5 Mbps nhưng chiếu video có thể vọt lên 3 Mbps
- [ ] Mỗi lúc chỉ một người chiếu; người sau muốn chiếu phải được nhường hoặc thay thế
- [ ] Bắt sự kiện `track.onended` để đồng bộ khi người dùng bấm "Dừng chia sẻ" của **trình duyệt** thay vì nút trong app
- [ ] Ghi nhận trong `call_participants` ai đã chiếu màn hình (phục vụ audit)
- [ ] Nói rõ trên giao diện: chia sẻ **tab trình duyệt** mới kèm được âm thanh, chia sẻ **toàn màn hình** thì không — giới hạn của trình duyệt, đừng để người dùng tưởng lỗi

### Ghi hình (tuỳ chọn — chỉ làm khi có nhu cầu thật)
- [ ] Ghi hình bằng LiveKit Egress, xuất thẳng lên Cloudflare R2
- [ ] **Xin đồng ý trước khi ghi**: hiện cảnh báo cho mọi người trong phòng, lưu lại sự đồng ý
- [ ] Phân quyền xem bản ghi ở tầng usecase (không chỉ ẩn nút trên giao diện), ghi audit log mỗi lượt xem
- [ ] Chính sách lưu trữ và tự xoá sau N ngày — video ăn dung lượng rất nhanh

### Frontend
- [ ] Nút gọi trong khung chat: gọi thoại / gọi video, cho cả hội thoại 1-1 và nhóm
- [ ] Giao diện đổ chuông: chấp nhận / từ chối, có âm thanh, hiện cả khi đang ở trang khác
- [ ] Màn hình cuộc gọi: lưới video tự đổi bố cục theo số người, ghim người đang nói
- [ ] Thanh điều khiển: tắt/bật mic, camera, chia sẻ màn hình, rời cuộc gọi
- [ ] Màn hình kiểm tra thiết bị trước khi vào: chọn mic/camera/loa, xem trước hình, đo mức âm thanh
- [ ] Xử lý khi người dùng **từ chối quyền** camera/mic: hướng dẫn bật lại, không để màn hình trắng
- [ ] Hiện chất lượng kết nối (tốt / yếu / mất kết nối) dựa trên thống kê WebRTC
- [ ] Cửa sổ nổi (picture-in-picture) khi rời khỏi trang cuộc gọi
- [ ] Chặn mở cuộc gọi ở nhiều tab cùng lúc (dùng BroadcastChannel như Phase 5)
- [ ] Báo rõ khi trình duyệt không hỗ trợ (Safari cũ, trình duyệt nhúng trong app Facebook/Zalo)

### Hạ tầng & Docker — phần dễ sai nhất
- [ ] **HTTPS trở thành bắt buộc, không còn là tuỳ chọn.** `getUserMedia` và `getDisplayMedia` chỉ chạy trong secure context. `localhost` được miễn, nhưng test qua IP LAN là hỏng ngay → **kéo phần TLS của Phase 6 lên làm trước phase này**
- [ ] Media **không** đi qua nginx. Nginx chỉ còn proxy signaling (`/ws`) và REST — không cấu hình proxy cho cổng media
- [ ] Dùng **UDP mux một cổng** của LiveKit (ví dụ `7881/udp`). **Không** map dải cổng UDP rộng trong Docker: `docker-proxy` sinh một tiến trình cho mỗi cổng, khởi động cực chậm hoặc treo luôn
- [ ] Không dùng `network_mode: host` cho SFU — mất cách ly mạng, và **không chạy được trên Docker Desktop Windows**, tức là hỏng luôn môi trường dev
- [ ] Bật `use_external_ip` cho SFU khi chạy sau NAT (VPS, cloud)
- [ ] Mở UDP ở firewall / security group; nhiều nhà cung cấp chặn UDP mặc định
- [ ] Đặt `deploy.resources.limits` riêng cho SFU — nó ăn CPU và băng thông khác hẳn `api`
- [ ] Thêm SFU và TURN vào stack giám sát Phase 6: băng thông vào/ra, số phòng, số người, tỉ lệ relay

### Ước lượng băng thông — tính trước khi mở cho toàn công ty

Băng thông **gửi đi của server tăng theo bình phương** số người trong phòng:

| Số người | Server nhận | Server gửi (không tối ưu) | Server gửi (có simulcast) |
|---|---|---|---|
| 4 | 6 Mbps | 18 Mbps | khoảng 6 Mbps |
| 10 | 15 Mbps | **135 Mbps** | khoảng 35 Mbps |
| 16 | 24 Mbps | **360 Mbps** | khoảng 70 Mbps |

*Giả định 720p tương đương 1.5 Mbps mỗi luồng. Cột simulcast giả định chỉ 1–2 người hiện ở độ nét cao, còn lại 180p.*

Kết luận thực dụng: **simulcast không phải là tối ưu hoá, nó là điều kiện để chạy được.** Làm ngay từ đầu, đừng để dành.

### Nghiệm thu Phase 7
- [ ] Gọi 1-1 giữa hai máy khác mạng (một WiFi, một 4G) — thông, hình và tiếng ổn định
- [ ] Gọi nhóm 6 người qua SFU — không ai vỡ hình, CPU máy client không quá tải
- [ ] Trình chiếu màn hình giữa lúc đang gọi — chữ trên màn hình đọc được rõ
- [ ] Gọi được từ **mạng công ty chặn UDP** — chứng minh fallback TURN qua TCP/443 hoạt động
- [ ] Đổi WiFi sang 4G giữa cuộc gọi — ICE restart chạy, cuộc gọi không rớt
- [ ] `docker compose up -d --scale api=3` — signaling vẫn đúng khi hai người nối vào hai instance khác nhau

---

## 6. Thứ tự ưu tiên và phụ thuộc

```
Phase 0  ──>  Phase 1  ──┬──>  Phase 2 (Dự án/Task)
                         │
                         ├──>  Phase 5 (Realtime)  ──┬──>  Phase 3 (Chấm công)  ──┐
                         │                           │                            │
                         │                           └──>  Phase 7 (Gọi video)    │
                         │                                                        │
                         └────────────────────────────────────────────────────────┴──>  Phase 4 (Lương)
```

Toàn bộ các phase đều chạy trong Docker ngay từ Phase 0 — không có giai đoạn nào code chạy trực tiếp trên máy rồi mới "đóng gói vào container sau". Việc đóng gói muộn là nguồn gốc của phần lớn lỗi "chạy trên máy tôi thì được".

**Lưu ý phụ thuộc quan trọng:** Phase 3 (chấm công) phụ thuộc vào hạ tầng WebSocket của Phase 5. Có hai cách xử lý:

- **Khuyến nghị:** làm phần *hạ tầng WebSocket + presence* của Phase 5 trước, rồi mới làm Phase 3, để dành phần chat đầy đủ làm sau.
- Hoặc: tạm dùng check-in thủ công ở Phase 3, chuyển sang presence tự động khi Phase 5 xong.

Phase 4 (lương) cần dữ liệu công từ Phase 3 để tính lương theo giờ; nếu chỉ tính lương cố định thì có thể làm độc lập.

**Phase 7 (gọi video) có hai phụ thuộc, một trong số đó dễ bị bỏ sót:** hub WebSocket của Phase 5 làm kênh signaling, và **phần TLS vốn nằm ở Phase 6**. `getUserMedia` và `getDisplayMedia` chỉ chạy trong secure context, nên nếu làm Phase 7 trước Phase 6 thì phải kéo riêng phần bật HTTPS lên trước — không thì ngay khi test qua IP LAN là hỏng.

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
| Gọi video không kết nối được ở mạng công ty chặn UDP | Bắt buộc có TURN kèm listener TCP/443. Kiểm chứng trên mạng công ty thật ngay từ bước đầu của Phase 7, không để cuối phase mới phát hiện. |
| Băng thông SFU phình theo bình phương số người trong phòng | Bật simulcast ngay từ đầu (điều kiện để chạy được, không phải tối ưu hoá), giới hạn số người mỗi phòng, chỉ hiện người đang nói ở độ nét cao. |
| Map dải cổng UDP rộng trong Docker làm container khởi động treo | Dùng UDP mux một cổng của LiveKit. Không dùng `network_mode: host` vì hỏng môi trường dev trên Docker Desktop Windows. |
| Ghi hình cuộc gọi vi phạm quyền riêng tư nhân viên | Xin đồng ý trước khi ghi và hiện cảnh báo cho cả phòng; phân quyền xem ở tầng usecase; audit log mỗi lượt xem; tự xoá theo chính sách lưu trữ. |
| Một module lỗi làm sập cả `api` | Middleware `Recoverer` bắt panic ở tầng HTTP; worker có dead-letter queue cho message lỗi. |
| Job nặng (tính lương, xuất Excel) làm nghẽn request HTTP | Đẩy sang `worker` qua RabbitMQ, `api` trả về ngay và báo kết quả qua thông báo. |

---

## 8. Bước tiếp theo

1. ~~Viết kế hoạch triển khai chi tiết cho Phase 0~~ → xong: [PHASE-0-SETUP.md](./PHASE-0-SETUP.md)
2. Code Phase 0 theo tài liệu đó.
3. Nghiệm thu: `make init` → `make smoke` trả về JSON có `database_time` và `job_queued: true`, đồng thời log worker hiện job vừa nhận — chứng minh cả hai chuỗi HTTP → PostgreSQL và api → RabbitMQ → worker đã thông.
4. Sang Phase 1: xác thực JWT, RBAC, CRUD nhân viên và phòng ban.
