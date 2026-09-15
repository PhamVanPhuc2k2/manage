# PHASE 0 — Thiết lập nền tảng dự án

> Tài liệu hướng dẫn chi tiết từng bước. Đọc kèm [TASKS.md](./TASKS.md).
> Cập nhật: 2026-09-15 — **Phase 0 đã dựng xong và chạy được.**

## Trạng thái

Toàn bộ code trong tài liệu này đã được viết, build và kiểm chứng thật. Kết quả đo được:

| Hạng mục | Kết quả |
|---|---|
| 10 container | tất cả `healthy` |
| `GET /api/v1/ping` | trả về `database_time`, `redis_ok: true`, `job_queued: true` |
| Worker nhận job | có, `request_id` khớp chính xác với response HTTP |
| Hot reload (Air) | rebuild sau 4 giây kể từ lúc lưu file |
| Graceful shutdown | `docker stop` → log `nhận tín hiệu dừng` → `api đã dừng` |
| Dead-letter queue | message hỏng rơi sang `manage.jobs.dead`, không lặp vô hạn |
| `golangci-lint` | 0 issues |
| Image production | api 32.5MB · worker 27.1MB · frontend 313MB, chạy bằng user `app` (uid 1001) |

Những chỗ bản dựng thật khác với kế hoạch ban đầu đều đã được sửa lại trong tài liệu, và lý do ghi ở [mục 18](#18-lỗi-thường-gặp).

---

## 1. Mục tiêu Phase 0

Xây **walking skeleton** — bộ khung mỏng nhưng xuyên suốt toàn hệ thống, chạy được thật, để mọi phase sau chỉ việc lắp thêm nghiệp vụ vào khuôn có sẵn.

Cuối Phase 0 phải thông cả hai chuỗi:

```
Trình duyệt ─▶ nginx ─▶ api ─▶ PostgreSQL          (đồng bộ, trả kết quả ngay)
                         └──▶ RabbitMQ ─▶ worker    (bất đồng bộ, chạy nền)
```

### Tiêu chí nghiệm thu

- [x] Clone repo sạch trên máy mới, chỉ cài Docker, chạy `make init` (hoặc `.\dev.ps1 init`) → hệ thống lên
- [x] `curl $PUBLIC_BASE_URL/api/v1/ping` trả về JSON có `database_time`, `redis_ok`, `job_queued`
- [x] Log của `worker` hiện job vừa nhận, kèm đúng `request_id` của request HTTP ở trên
- [x] Sửa một dòng code Go → Air tự build lại trong container, không cần restart tay
- [x] `docker compose ps` thấy mọi container ở trạng thái `healthy`
- [x] `make lint` chạy xanh (0 issues)
- [x] Build được image production cho cả ba: api, worker, frontend
- [ ] CI trên GitHub chạy xanh — chưa kiểm chứng được vì repo chưa đẩy lên remote

---

## 2. Kiến trúc: Monolith theo module

Một codebase, một database, hai binary.

| Binary | Nhiệm vụ | Vì sao tách |
|---|---|---|
| `api` | HTTP REST + WebSocket (từ Phase 5) | Cần phản hồi nhanh, scale theo số người dùng đang online |
| `worker` | Consumer RabbitMQ: gửi mail, tính lương, xuất Excel, tổng hợp chấm công | Job nặng không được làm nghẽn request HTTP. Scale theo khối lượng job |

Hai binary **dùng chung toàn bộ `internal/`**. Chúng chỉ khác nhau ở tầng `delivery`: một bên nhận HTTP request, một bên nhận message RabbitMQ. Cùng usecase, cùng repository, cùng domain.

### Quy tắc phụ thuộc — Clean Architecture

```
  delivery  ──▶  usecase  ──▶  domain  ◀──  repository
  (http, ws)     (nghiệp vụ)   (entity     (postgres,
  (consumer)                    + port)     redis, mq)
```

| Tầng | Được import gì | Không được import gì |
|---|---|---|
| `domain` | Standard library (`context`, `time`, `errors`) | Framework, driver database, thư viện HTTP |
| `usecase` | `domain`, standard library | `chi`, `pgx`, `net/http`, struct JSON |
| `repository` | `domain`, driver (`pgx`, `go-redis`, `amqp091`) | `usecase`, `delivery` |
| `delivery` | `usecase`, `domain`, framework | `repository` (phải đi qua usecase) |

Điểm hay gây hiểu nhầm: quy tắc "domain không phụ thuộc bên ngoài" **nhắm vào framework và driver, không nhắm vào standard library**. `time.Time` và `context.Context` dùng thoải mái ở mọi tầng — né tránh chúng chỉ tạo ra code rối rắm vô ích.

### Ranh giới module — thứ giữ cho monolith không thành mớ hỗn độn

Monolith hỏng không phải vì nó là monolith, mà vì không ai giữ ranh giới. Ba quy tắc:

1. **Module chỉ gọi nhau qua tầng usecase.** `project/usecase` muốn biết nhân viên có tồn tại không thì gọi `employee/usecase`, tuyệt đối không gọi `employee/repository`.
2. **Interface khai báo ở phía người dùng.** `project` cần gì từ `employee` thì `project` tự khai báo interface đó, `employee` đáp ứng. Đây là cách Go làm dependency inversion, và nó giúp module `project` test được mà không cần module `employee` thật.
3. **Không import vòng.** Nếu A cần B và B cần A thì ranh giới đang sai — tách phần chung ra module thứ ba.

Giữ được ba điều này thì sau này nếu thật sự cần tách service, việc tách sẽ khả thi. Còn chưa cần thì không phải trả giá vận hành.

---

## 3. Cấu trúc thư mục

```
manage/
├── doc/
│   ├── TASKS.md
│   └── PHASE-0-SETUP.md
│
├── backend/
│   ├── go.mod
│   ├── .air.api.toml                   # Cấu hình hot reload cho api
│   ├── .air.worker.toml                # Cấu hình hot reload cho worker
│   ├── .dockerignore
│   ├── .golangci.yml
│   │
│   ├── cmd/
│   │   ├── api/main.go                 # Binary 1
│   │   └── worker/main.go              # Binary 2
│   │
│   ├── migrations/                     # MỘT bộ migration cho cả hệ thống
│   │   ├── 000001_init.up.sql
│   │   └── 000001_init.down.sql
│   │
│   ├── internal/
│   │   ├── domain/                     # TẦNG 1
│   │   │   └── system/                 # Module mẫu của Phase 0
│   │   │       ├── entity.go
│   │   │       └── port.go             # Interface mà tầng dưới phải đáp ứng
│   │   │
│   │   ├── usecase/                    # TẦNG 2
│   │   │   └── system/
│   │   │       └── ping.go
│   │   │
│   │   ├── repository/                 # TẦNG 3
│   │   │   ├── postgres/
│   │   │   ├── redis/
│   │   │   └── rabbitmq/
│   │   │
│   │   └── delivery/                   # TẦNG 4
│   │       ├── http/
│   │       │   ├── handler/
│   │       │   ├── middleware/
│   │       │   └── router/router.go
│   │       └── consumer/               # Handler cho message RabbitMQ
│   │           ├── dispatcher.go
│   │           └── system.go
│   │
│   ├── pkg/                            # Tiện ích không gắn nghiệp vụ
│   │   ├── config/
│   │   ├── logger/
│   │   ├── apperror/
│   │   ├── httpx/
│   │   ├── postgres/
│   │   ├── redis/
│   │   └── rabbitmq/
│   │
│   └── tests/                          # Integration test
│
├── frontend/
│   ├── .dockerignore
│   ├── next.config.js
│   └── src/app/
│
├── docker/
│   ├── backend/Dockerfile              # DÙNG CHUNG cho api và worker
│   ├── frontend/Dockerfile
│   ├── nginx/
│   │   ├── nginx.conf
│   │   └── conf.d/app.conf
│   └── postgres/init/01-extensions.sql
│
├── .github/workflows/ci.yml
├── docker-compose.yml
├── docker-compose.override.yml
├── docker-compose.prod.yml
├── .env.example
├── .gitignore
└── Makefile
```

---

## 4. Bước 1 — Khởi tạo repo

```bash
cd D:/Projects/manage
git init
```

### `.gitignore`

```gitignore
# Go
/backend/tmp/
*.exe
*.test
coverage.out

# Node
node_modules/
.next/
out/

# Môi trường — KHÔNG BAO GIỜ commit
.env
.env.local
*.local

# IDE
.vscode/
.idea/

# Chứng chỉ TLS
docker/nginx/certs/
```

### `.env.example`

File này commit, `.env` thật thì không.

```bash
# ---------- Chung ----------
COMPOSE_PROJECT_NAME=manage
ENV=development
LOG_LEVEL=debug

# ---------- PostgreSQL ----------
POSTGRES_USER=manage
POSTGRES_PASSWORD=manage_dev_password
POSTGRES_DB=manage
POSTGRES_PORT=5432

# ---------- Redis ----------
REDIS_PASSWORD=redis_dev_password
REDIS_PORT=6379

# ---------- RabbitMQ ----------
RABBITMQ_USER=manage
RABBITMQ_PASSWORD=rabbit_dev_password
RABBITMQ_PORT=5672
RABBITMQ_UI_PORT=15672

# ---------- MinIO ----------
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=minio_dev_password
MINIO_API_PORT=9000
MINIO_UI_PORT=9001

# ---------- JWT ----------
# Sinh bằng: openssl rand -base64 48
JWT_SECRET=doi_chuoi_nay_truoc_khi_len_production
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h

# ---------- Cổng expose ra host (chỉ dev) ----------
# Nếu port nào bị chiếm trên máy bạn (IIS, Laravel Herd, XAMPP hay dự án khác),
# đổi số ở đây là đủ — không phải sửa file nào khác.
NGINX_PORT=80
API_PORT=8080
FRONTEND_PORT=3000
ADMINER_PORT=8081
MAILHOG_UI_PORT=8025

# ---------- Địa chỉ công khai ----------
# Địa chỉ trình duyệt dùng để vào hệ thống. Phải KHỚP với NGINX_PORT ở trên.
# Cổng 80 thì bỏ số cổng đi: trình duyệt gửi Origin không kèm ":80",
# ghi thừa sẽ làm CORS chặn request.
#   NGINX_PORT=80    →  PUBLIC_BASE_URL=http://localhost
#   NGINX_PORT=8088  →  PUBLIC_BASE_URL=http://localhost:8088
PUBLIC_BASE_URL=http://localhost
PUBLIC_WS_URL=ws://localhost
```

> **Kiểm tra port trước khi chạy lần đầu.** Trên Windows, IIS và Laravel Herd hay chiếm sẵn cổng 80 và 9001. Kiểm tra bằng PowerShell:
>
> ```powershell
> foreach ($p in 80,3000,5432,6379,5672,15672,9000,9001,8080,8081,8025) {
>   $c = Get-NetTCPConnection -LocalPort $p -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1
>   if ($c) { "{0,-6} BỊ CHIẾM bởi {1}" -f $p, (Get-Process -Id $c.OwningProcess).ProcessName }
>   else    { "{0,-6} trống" -f $p }
> }
> ```

```bash
cp .env.example .env
```

---

## 5. Bước 2 — Tiện ích nền (`backend/pkg/`)

```bash
cd backend
go mod init github.com/yourorg/manage
```

> Đổi `yourorg` thành tên GitHub org/user thật của bạn.

```bash
go get github.com/go-chi/chi/v5
go get github.com/go-chi/cors
go get github.com/jackc/pgx/v5
go get github.com/redis/go-redis/v9
go get github.com/rabbitmq/amqp091-go
go get github.com/rs/zerolog
go get github.com/spf13/viper
go get github.com/google/uuid
go get github.com/go-playground/validator/v10
go get golang.org/x/crypto/bcrypt
```

### `pkg/config/config.go`

```go
// Package config đọc cấu hình từ biến môi trường và validate ngay lúc khởi động.
// Nguyên tắc: thà chết ngay lúc start còn hơn chạy được rồi lỗi lúc 2 giờ sáng.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	AppName         string        `mapstructure:"APP_NAME"`
	Env             string        `mapstructure:"ENV"`
	LogLevel        string        `mapstructure:"LOG_LEVEL"`
	HTTPPort        int           `mapstructure:"HTTP_PORT"`
	ShutdownTimeout time.Duration `mapstructure:"SHUTDOWN_TIMEOUT"`

	PostgresDSN     string `mapstructure:"POSTGRES_DSN"`
	PostgresMaxConn int32  `mapstructure:"POSTGRES_MAX_CONN"`

	RedisAddr     string `mapstructure:"REDIS_ADDR"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD"`
	RedisDB       int    `mapstructure:"REDIS_DB"`

	RabbitMQURL string `mapstructure:"RABBITMQ_URL"`
	// Số message worker xử lý song song. Đặt 1 khi cần giữ đúng thứ tự.
	WorkerConcurrency int `mapstructure:"WORKER_CONCURRENCY"`

	JWTSecret     string        `mapstructure:"JWT_SECRET"`
	JWTAccessTTL  time.Duration `mapstructure:"JWT_ACCESS_TTL"`
	JWTRefreshTTL time.Duration `mapstructure:"JWT_REFRESH_TTL"`

	CORSAllowedOrigins []string `mapstructure:"CORS_ALLOWED_ORIGINS"`
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load đọc cấu hình. appName dùng làm nhãn trong log ("api" hoặc "worker").
func Load(appName string) (*Config, error) {
	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// SetDefault đồng thời làm nhiệm vụ ĐĂNG KÝ key với viper.
	// Thiếu dòng default thì Unmarshal sẽ bỏ qua biến môi trường tương ứng —
	// đây là cái bẫy hay gặp nhất khi dùng viper với env.
	v.SetDefault("APP_NAME", appName)
	v.SetDefault("ENV", "development")
	v.SetDefault("LOG_LEVEL", "debug")
	v.SetDefault("HTTP_PORT", 8080)
	v.SetDefault("SHUTDOWN_TIMEOUT", "30s")

	v.SetDefault("POSTGRES_DSN", "")
	v.SetDefault("POSTGRES_MAX_CONN", 20)

	v.SetDefault("REDIS_ADDR", "redis:6379")
	v.SetDefault("REDIS_PASSWORD", "")
	v.SetDefault("REDIS_DB", 0)

	v.SetDefault("RABBITMQ_URL", "")
	v.SetDefault("WORKER_CONCURRENCY", 5)

	v.SetDefault("JWT_SECRET", "")
	v.SetDefault("JWT_ACCESS_TTL", "15m")
	v.SetDefault("JWT_REFRESH_TTL", "168h")

	v.SetDefault("CORS_ALLOWED_ORIGINS", []string{"http://localhost", "http://localhost:3000"})

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("giải mã cấu hình: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c Config) validate() error {
	var missing []string

	if c.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if c.RabbitMQURL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	if c.IsProduction() {
		if c.JWTSecret == "" || strings.HasPrefix(c.JWTSecret, "doi_chuoi_nay") {
			missing = append(missing, "JWT_SECRET (không được dùng giá trị mặc định ở production)")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("thiếu cấu hình bắt buộc: %s", strings.Join(missing, ", "))
	}
	return nil
}
```

### `pkg/logger/logger.go`

```go
// Package logger cung cấp log có cấu trúc, gắn kèm request_id để lần vết
// một request từ lúc vào HTTP tới lúc worker xử lý xong job nền.
package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
)

type ctxKey struct{}

// New tạo logger gốc.
// Dev: in ra dạng người đọc được. Production: JSON để Loki thu thập.
func New(appName, level, env string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	if env == "production" {
		return zerolog.New(os.Stdout).
			Level(lvl).
			With().Timestamp().Str("app", appName).Logger()
	}

	w := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	return zerolog.New(w).
		Level(lvl).
		With().Timestamp().Str("app", appName).Logger()
}

func WithContext(ctx context.Context, l zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// FromContext lấy logger ra. Không có thì trả logger rỗng, không panic.
func FromContext(ctx context.Context) zerolog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		return l
	}
	return zerolog.Nop()
}
```

### `pkg/postgres/postgres.go`

```go
// Package postgres quản lý connection pool.
// Có retry vì khi docker compose khởi động, database có thể chưa nhận kết nối
// ngay cả khi healthcheck đã báo xanh.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

type DB struct {
	*pgxpool.Pool
}

func New(ctx context.Context, dsn string, maxConn int32, log zerolog.Logger) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("phân tích DSN postgres: %w", err)
	}
	cfg.MaxConns = maxConn
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("tạo pool postgres: %w", err)
	}

	const maxAttempts = 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()

		if err == nil {
			log.Info().Int32("max_conns", maxConn).Msg("đã kết nối postgres")
			return &DB{Pool: pool}, nil
		}

		wait := time.Duration(attempt) * 500 * time.Millisecond
		log.Warn().Err(err).
			Int("attempt", attempt).
			Dur("retry_in", wait).
			Msg("chưa kết nối được postgres, thử lại")

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		}
	}

	pool.Close()
	return nil, fmt.Errorf("không kết nối được postgres sau %d lần: %w", maxAttempts, err)
}

func (db *DB) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return db.Ping(ctx)
}
```

### `pkg/rabbitmq/rabbitmq.go`

```go
// Package rabbitmq quản lý kết nối và khai báo topology.
//
// Topology của dự án:
//
//	manage.jobs (exchange direct)
//	  └─ routing key = tên job ─▶ manage.jobs.queue
//	                                   │ (message bị nack hoặc quá hạn)
//	                                   ▼
//	                             manage.jobs.dlx ─▶ manage.jobs.dead
//
// Dead-letter queue rất quan trọng: không có nó, một message lỗi sẽ bị
// requeue vô hạn và ăn hết CPU của worker.
package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const (
	ExchangeJobs    = "manage.jobs"
	ExchangeJobsDLX = "manage.jobs.dlx"
	QueueJobs       = "manage.jobs.queue"
	QueueJobsDead   = "manage.jobs.dead"
)

type Client struct {
	url  string
	log  zerolog.Logger
	mu   sync.RWMutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func New(ctx context.Context, url string, log zerolog.Logger) (*Client, error) {
	c := &Client{url: url, log: log}
	if err := c.connect(ctx); err != nil {
		return nil, err
	}
	go c.watchAndReconnect()
	return c, nil
}

func (c *Client) connect(ctx context.Context) error {
	const maxAttempts = 10
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := amqp.Dial(c.url)
		if err == nil {
			ch, chErr := conn.Channel()
			if chErr == nil {
				c.mu.Lock()
				c.conn, c.ch = conn, ch
				c.mu.Unlock()

				if err := c.declareTopology(); err != nil {
					return err
				}
				c.log.Info().Msg("đã kết nối rabbitmq")
				return nil
			}
			_ = conn.Close()
			lastErr = chErr
		} else {
			lastErr = err
		}

		wait := time.Duration(attempt) * time.Second
		c.log.Warn().Err(lastErr).
			Int("attempt", attempt).
			Dur("retry_in", wait).
			Msg("chưa kết nối được rabbitmq, thử lại")

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("không kết nối được rabbitmq sau %d lần: %w", maxAttempts, lastErr)
}

// declareTopology tạo exchange và queue. Gọi mỗi lần kết nối lại —
// các lệnh declare đều idempotent nên chạy nhiều lần vô hại.
func (c *Client) declareTopology() error {
	ch := c.Channel()

	if err := ch.ExchangeDeclare(ExchangeJobs, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo exchange %s: %w", ExchangeJobs, err)
	}
	if err := ch.ExchangeDeclare(ExchangeJobsDLX, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo exchange %s: %w", ExchangeJobsDLX, err)
	}

	// Queue chính, có gắn dead-letter exchange.
	if _, err := ch.QueueDeclare(QueueJobs, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": ExchangeJobsDLX,
	}); err != nil {
		return fmt.Errorf("khai báo queue %s: %w", QueueJobs, err)
	}
	if _, err := ch.QueueDeclare(QueueJobsDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo queue %s: %w", QueueJobsDead, err)
	}
	if err := ch.QueueBind(QueueJobsDead, "", ExchangeJobsDLX, false, nil); err != nil {
		return fmt.Errorf("bind queue chết: %w", err)
	}
	return nil
}

// BindJob gắn một tên job vào queue chính. Gọi lúc khởi động worker
// cho mỗi loại job mà worker biết xử lý.
func (c *Client) BindJob(jobName string) error {
	return c.Channel().QueueBind(QueueJobs, jobName, ExchangeJobs, false, nil)
}

// watchAndReconnect lắng nghe sự kiện đóng kết nối và tự kết nối lại.
// RabbitMQ restart hoặc mạng chớp là chuyện bình thường, không được để chết theo.
func (c *Client) watchAndReconnect() {
	for {
		c.mu.RLock()
		conn := c.conn
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		reason, ok := <-conn.NotifyClose(make(chan *amqp.Error))
		if !ok {
			return // đóng chủ động, không phải lỗi
		}

		c.log.Error().Str("reason", reason.Error()).Msg("rabbitmq mất kết nối, đang kết nối lại")
		if err := c.connect(context.Background()); err != nil {
			c.log.Error().Err(err).Msg("kết nối lại rabbitmq thất bại")
			return
		}
	}
}

func (c *Client) Channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ch
}

func (c *Client) HealthCheck(context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil || c.conn.IsClosed() {
		return fmt.Errorf("rabbitmq không có kết nối")
	}
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		_ = c.ch.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
```

### `pkg/httpx/response.go`

```go
// Package httpx chuẩn hoá định dạng response JSON cho toàn bộ REST API.
// Mọi endpoint đều trả về cùng một hình dạng, frontend chỉ cần viết một
// bộ xử lý lỗi duy nhất.
package httpx

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Data  any        `json:"data,omitempty"`
	Meta  any        `json:"meta,omitempty"`
	Error *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Details   any    `json:"details,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func JSON(w http.ResponseWriter, status int, payload Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Envelope{Data: data})
}

func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, Envelope{Data: data})
}

func Paginated(w http.ResponseWriter, data any, meta any) {
	JSON(w, http.StatusOK, Envelope{Data: data, Meta: meta})
}
```

### `pkg/apperror/apperror.go`

```go
// Package apperror định nghĩa loại lỗi của ứng dụng và cách dịch chúng
// sang HTTP status.
//
// Nhờ package này, tầng usecase chỉ cần trả lỗi đúng ngữ nghĩa
// (NotFound, Conflict, Forbidden...) mà không cần biết gì về HTTP.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type Kind string

const (
	KindInvalid      Kind = "INVALID_ARGUMENT"
	KindUnauthorized Kind = "UNAUTHORIZED"
	KindForbidden    Kind = "FORBIDDEN"
	KindNotFound     Kind = "NOT_FOUND"
	KindConflict     Kind = "CONFLICT"
	KindUnprocessable Kind = "UNPROCESSABLE"
	KindRateLimited  Kind = "RATE_LIMITED"
	KindInternal     Kind = "INTERNAL"
)

type Error struct {
	Kind    Kind
	Message string
	Details any
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

func New(kind Kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

func Wrap(kind Kind, message string, cause error) *Error {
	return &Error{Kind: kind, Message: message, cause: cause}
}

// Các hàm tạo lỗi thường dùng, viết cho gọn ở chỗ gọi.
func NotFound(what string) *Error {
	return New(KindNotFound, fmt.Sprintf("không tìm thấy %s", what))
}

func Invalid(message string, details any) *Error {
	return &Error{Kind: KindInvalid, Message: message, Details: details}
}

func Internal(cause error) *Error {
	return Wrap(KindInternal, "lỗi nội bộ", cause)
}

// HTTPStatus dịch lỗi sang mã HTTP. Lỗi lạ mặc định là 500.
func HTTPStatus(err error) (int, *Error) {
	var appErr *Error
	if !errors.As(err, &appErr) {
		return http.StatusInternalServerError, Internal(err)
	}

	status := map[Kind]int{
		KindInvalid:       http.StatusBadRequest,
		KindUnauthorized:  http.StatusUnauthorized,
		KindForbidden:     http.StatusForbidden,
		KindNotFound:      http.StatusNotFound,
		KindConflict:      http.StatusConflict,
		KindUnprocessable: http.StatusUnprocessableEntity,
		KindRateLimited:   http.StatusTooManyRequests,
		KindInternal:      http.StatusInternalServerError,
	}[appErr.Kind]

	if status == 0 {
		status = http.StatusInternalServerError
	}
	return status, appErr
}
```

---

## 6. Bước 3 — Module mẫu đi hết 4 tầng

Module `system` không có giá trị nghiệp vụ. Nó tồn tại để **chứng minh bộ khung chạy được** và làm mẫu cho mọi module nghiệp vụ sau này sao chép cấu trúc.

### TẦNG 1 — `internal/domain/system/entity.go`

```go
// Package system chứa entity và port của module kiểm tra hệ thống.
package system

import "time"

// HealthSnapshot mô tả tình trạng hạ tầng tại một thời điểm.
type HealthSnapshot struct {
	DatabaseTime time.Time
	RedisOK      bool
	JobQueued    bool
	RequestID    string
}

// Job là một việc cần xử lý nền.
type Job struct {
	Name      string         `json:"name"`
	RequestID string         `json:"request_id"`
	Payload   map[string]any `json:"payload"`
}

// Tên các job. Khai báo hằng để tránh gõ sai chuỗi ở hai nơi.
const JobSystemPing = "system.ping"
```

### TẦNG 1 — `internal/domain/system/port.go`

```go
package system

import (
	"context"
	"time"
)

// Đây là các CỔNG (port) ra thế giới bên ngoài.
//
// Chúng được khai báo ở tầng domain nhưng ĐƯỢC HIỆN THỰC ở tầng repository.
// Nhờ vậy usecase chỉ biết interface, không biết đằng sau là PostgreSQL hay
// cái gì khác — và test được bằng bản giả mà không cần chạy database thật.

type Clock interface {
	Now(ctx context.Context) (time.Time, error)
}

type Cache interface {
	Ping(ctx context.Context) error
}

type JobPublisher interface {
	Publish(ctx context.Context, job Job) error
}
```

### TẦNG 2 — `internal/usecase/system/ping.go`

```go
// Package system là tầng nghiệp vụ của module system.
package system

import (
	"context"

	"github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/apperror"
	"github.com/yourorg/manage/pkg/logger"
)

type PingUsecase struct {
	clock     system.Clock
	cache     system.Cache
	publisher system.JobPublisher
}

func NewPingUsecase(
	clock system.Clock,
	cache system.Cache,
	publisher system.JobPublisher,
) *PingUsecase {
	return &PingUsecase{clock: clock, cache: cache, publisher: publisher}
}

// Ping chạm vào cả ba thành phần hạ tầng rồi báo cáo kết quả.
//
// Chỉ PostgreSQL là bắt buộc — hỏng thì trả lỗi. Redis và RabbitMQ hỏng thì
// vẫn trả về được, chỉ đánh dấu là false. Cách phân biệt này về sau áp dụng
// cho nghiệp vụ thật: mất Redis thì chậm chứ không sai, mất database thì sai.
func (u *PingUsecase) Ping(ctx context.Context, requestID string) (system.HealthSnapshot, error) {
	log := logger.FromContext(ctx)

	dbTime, err := u.clock.Now(ctx)
	if err != nil {
		return system.HealthSnapshot{}, apperror.Wrap(
			apperror.KindInternal, "không đọc được giờ từ database", err)
	}

	redisOK := true
	if err := u.cache.Ping(ctx); err != nil {
		log.Warn().Err(err).Msg("redis không phản hồi")
		redisOK = false
	}

	jobQueued := true
	if err := u.publisher.Publish(ctx, system.Job{
		Name:      system.JobSystemPing,
		RequestID: requestID,
		Payload:   map[string]any{"source": "api"},
	}); err != nil {
		log.Warn().Err(err).Msg("không đẩy được job lên hàng đợi")
		jobQueued = false
	}

	return system.HealthSnapshot{
		DatabaseTime: dbTime,
		RedisOK:      redisOK,
		JobQueued:    jobQueued,
		RequestID:    requestID,
	}, nil
}
```

### TẦNG 3 — `internal/repository/postgres/clock.go`

```go
package postgres

import (
	"context"
	"time"

	"github.com/yourorg/manage/pkg/postgres"
)

type ClockRepository struct {
	db *postgres.DB
}

func NewClockRepository(db *postgres.DB) *ClockRepository {
	return &ClockRepository{db: db}
}

// Now đọc giờ từ chính database — kiểm chứng kết nối còn sống thật,
// không phải chỉ còn trong pool.
func (r *ClockRepository) Now(ctx context.Context) (time.Time, error) {
	var t time.Time
	if err := r.db.QueryRow(ctx, "SELECT NOW()").Scan(&t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}
```

### TẦNG 3 — `internal/repository/redis/cache.go`

```go
package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

type CacheRepository struct {
	client *goredis.Client
}

func NewCacheRepository(client *goredis.Client) *CacheRepository {
	return &CacheRepository{client: client}
}

func (r *CacheRepository) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
```

### TẦNG 3 — `internal/repository/rabbitmq/publisher.go`

```go
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/yourorg/manage/internal/domain/system"
	mq "github.com/yourorg/manage/pkg/rabbitmq"
)

type JobPublisher struct {
	client *mq.Client
}

func NewJobPublisher(client *mq.Client) *JobPublisher {
	return &JobPublisher{client: client}
}

func (p *JobPublisher) Publish(ctx context.Context, job system.Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("mã hoá job: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return p.client.Channel().PublishWithContext(
		ctx,
		mq.ExchangeJobs,
		job.Name, // routing key = tên job
		false,    // mandatory
		false,    // immediate
		amqp.Publishing{
			ContentType: "application/json",
			// DeliveryMode 2 = persistent: message được ghi xuống đĩa,
			// không mất khi RabbitMQ restart. Chậm hơn nhưng với job
			// nghiệp vụ thì mất message là không chấp nhận được.
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			MessageId:    job.RequestID,
			Body:         body,
		},
	)
}
```

### TẦNG 4 — `internal/delivery/http/handler/system.go`

```go
package handler

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	domainsystem "github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/apperror"
	"github.com/yourorg/manage/pkg/httpx"
)

// PingUsecase là interface mà handler cần. Khai báo ở ĐÂY, phía người dùng,
// chứ không phải ở package usecase — đó là cách Go làm dependency inversion.
type PingUsecase interface {
	Ping(ctx context.Context, requestID string) (domainsystem.HealthSnapshot, error)
}

type SystemHandler struct {
	uc      PingUsecase
	version string
}

func NewSystemHandler(uc PingUsecase, version string) *SystemHandler {
	return &SystemHandler{uc: uc, version: version}
}

// Ping là endpoint nghiệm thu của Phase 0.
func (h *SystemHandler) Ping(w http.ResponseWriter, r *http.Request) {
	requestID := middleware.GetReqID(r.Context())

	snapshot, err := h.uc.Ping(r.Context(), requestID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	httpx.OK(w, map[string]any{
		"database_time": snapshot.DatabaseTime,
		"redis_ok":      snapshot.RedisOK,
		"job_queued":    snapshot.JobQueued,
		"request_id":    snapshot.RequestID,
		"version":       h.version,
	})
}

// Error là điểm duy nhất dịch lỗi ứng dụng thành HTTP response.
func Error(w http.ResponseWriter, err error, requestID string) {
	status, appErr := apperror.HTTPStatus(err)

	message := appErr.Message
	// Không để lộ chi tiết lỗi nội bộ ra ngoài — đó là thông tin cho attacker.
	if status == http.StatusInternalServerError {
		message = "Đã có lỗi xảy ra, vui lòng thử lại"
	}

	httpx.JSON(w, status, httpx.Envelope{Error: &httpx.ErrorBody{
		Code:      string(appErr.Kind),
		Message:   message,
		Details:   appErr.Details,
		RequestID: requestID,
	}})
}
```

### TẦNG 4 — `internal/delivery/consumer/system.go`

```go
// Package consumer là tầng delivery của worker: nhận message RabbitMQ
// rồi gọi xuống usecase. Đối xứng với tầng http của api.
package consumer

import (
	"context"

	"github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/logger"
)

// HandleSystemPing xử lý job system.ping.
//
// Ở Phase 0 nó chỉ ghi log — nhưng chính dòng log đó là bằng chứng
// chuỗi api → RabbitMQ → worker đã thông, và request_id truyền được qua.
func HandleSystemPing(ctx context.Context, job system.Job) error {
	// Gán ra biến trước rồi mới gọi Info().
	//
	// zerolog.Logger có method với POINTER receiver, mà giá trị trả về từ một
	// lời gọi hàm thì không lấy địa chỉ được. Viết liền
	// `logger.FromContext(ctx).Info()` sẽ không biên dịch được:
	//   cannot call pointer method Info on zerolog.Logger
	log := logger.FromContext(ctx)
	log.Info().
		Str("job", job.Name).
		Str("request_id", job.RequestID).
		Interface("payload", job.Payload).
		Msg("worker đã xử lý job")
	return nil
}
```

### TẦNG 4 — `internal/delivery/consumer/dispatcher.go`

```go
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	"github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/logger"
	mq "github.com/yourorg/manage/pkg/rabbitmq"
)

type HandlerFunc func(ctx context.Context, job system.Job) error

// Dispatcher định tuyến message tới handler theo tên job.
// Thêm loại job mới ở phase sau chỉ là gọi thêm một dòng Register.
type Dispatcher struct {
	client   *mq.Client
	log      zerolog.Logger
	handlers map[string]HandlerFunc
}

func NewDispatcher(client *mq.Client, log zerolog.Logger) *Dispatcher {
	return &Dispatcher{
		client:   client,
		log:      log,
		handlers: make(map[string]HandlerFunc),
	}
}

func (d *Dispatcher) Register(jobName string, fn HandlerFunc) error {
	d.handlers[jobName] = fn
	return d.client.BindJob(jobName)
}

// Run nhận message cho tới khi ctx bị huỷ.
func (d *Dispatcher) Run(ctx context.Context, concurrency int) error {
	ch := d.client.Channel()

	// Prefetch: mỗi worker chỉ giữ tối đa `concurrency` message chưa ack.
	// Không đặt QoS thì RabbitMQ đẩy toàn bộ queue vào một worker,
	// các worker khác ngồi không.
	if err := ch.Qos(concurrency, 0, false); err != nil {
		return fmt.Errorf("đặt QoS: %w", err)
	}

	deliveries, err := ch.Consume(
		mq.QueueJobs,
		"",    // consumer tag, để rỗng cho RabbitMQ tự sinh
		false, // autoAck = false: TỰ ack sau khi xử lý xong.
		//       Bật autoAck sẽ mất message khi worker chết giữa chừng.
		false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("đăng ký consume: %w", err)
	}

	sem := make(chan struct{}, concurrency)

	for {
		select {
		case <-ctx.Done():
			d.log.Info().Msg("dừng nhận message mới")
			return nil

		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("kênh consume đã đóng")
			}

			sem <- struct{}{}
			go func(msg amqp.Delivery) {
				defer func() { <-sem }()
				d.handle(ctx, msg)
			}(delivery)
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, msg amqp.Delivery) {
	start := time.Now()

	var job system.Job
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		d.log.Error().Err(err).Bytes("body", msg.Body).Msg("message hỏng, đẩy sang dead-letter")
		// requeue = false → message đi thẳng sang dead-letter queue.
		// Requeue một message hỏng chỉ tạo vòng lặp vô tận.
		_ = msg.Nack(false, false)
		return
	}

	handler, found := d.handlers[job.Name]
	if !found {
		d.log.Error().Str("job", job.Name).Msg("không có handler cho job này")
		_ = msg.Nack(false, false)
		return
	}

	log := d.log.With().
		Str("job", job.Name).
		Str("request_id", job.RequestID).
		Logger()

	jobCtx, cancel := context.WithTimeout(logger.WithContext(ctx, log), 5*time.Minute)
	defer cancel()

	if err := handler(jobCtx, job); err != nil {
		log.Error().Err(err).Msg("xử lý job thất bại")
		_ = msg.Nack(false, false)
		return
	}

	log.Debug().Dur("duration", time.Since(start)).Msg("xử lý job xong")
	_ = msg.Ack(false)
}
```

> **Về việc thử lại job lỗi:** hiện tại job lỗi đi thẳng sang dead-letter, không thử lại. Đúng cho Phase 0. Khi Phase 4 có job gửi email (lỗi mạng tạm thời rất hay xảy ra), cần thêm cơ chế thử lại có giãn cách — cách thông dụng là dùng queue trung gian có TTL. Đã ghi vào mục 18.

---

## 7. Bước 4 — Migration đầu tiên

`backend/migrations/000001_init.up.sql`

```sql
-- Phase 0 chỉ tạo bảng ghi lịch sử migration của chính ứng dụng,
-- để chứng minh đường migration chạy được. Bảng nghiệp vụ thật
-- bắt đầu từ Phase 1.

CREATE TABLE IF NOT EXISTS system_info (
    key        VARCHAR(100) PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO system_info (key, value)
VALUES ('schema_initialized_at', NOW()::TEXT)
ON CONFLICT (key) DO NOTHING;
```

`backend/migrations/000001_init.down.sql`

```sql
DROP TABLE IF EXISTS system_info;
```

---

## 8. Bước 5 — `cmd/api`

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/yourorg/manage/internal/delivery/http/router"
	repopg "github.com/yourorg/manage/internal/repository/postgres"
	repomq "github.com/yourorg/manage/internal/repository/rabbitmq"
	reporedis "github.com/yourorg/manage/internal/repository/redis"
	ucsystem "github.com/yourorg/manage/internal/usecase/system"
	"github.com/yourorg/manage/pkg/config"
	"github.com/yourorg/manage/pkg/logger"
	"github.com/yourorg/manage/pkg/postgres"
	"github.com/yourorg/manage/pkg/rabbitmq"
)

// Ba biến này được nhúng lúc build qua -ldflags -X.
var (
	version   = "dev"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api dừng do lỗi: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}

	log := logger.New("api", cfg.LogLevel, cfg.Env)
	log.Info().
		Str("version", version).
		Str("git_sha", gitSHA).
		Str("build_time", buildTime).
		Str("env", cfg.Env).
		Msg("đang khởi động api")

	// Context này bị huỷ khi nhận SIGTERM (docker stop) hoặc SIGINT (Ctrl+C).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// --- Hạ tầng ---
	db, err := postgres.New(ctx, cfg.PostgresDSN, cfg.PostgresMaxConn, log)
	if err != nil {
		return err
	}
	defer db.Close()

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer func() { _ = rdb.Close() }()

	mqClient, err := rabbitmq.New(ctx, cfg.RabbitMQURL, log)
	if err != nil {
		return err
	}
	defer func() { _ = mqClient.Close() }()

	// --- Nối dây các tầng (composition root) ---
	//
	// Đây là NƠI DUY NHẤT biết cả interface lẫn bản hiện thực cụ thể.
	// Mọi tầng khác chỉ biết interface. Muốn đổi PostgreSQL sang thứ khác
	// thì sửa đúng ở đây, không đụng vào usecase.
	clockRepo := repopg.NewClockRepository(db)
	cacheRepo := reporedis.NewCacheRepository(rdb)
	publisher := repomq.NewJobPublisher(mqClient)

	pingUC := ucsystem.NewPingUsecase(clockRepo, cacheRepo, publisher)

	deps := router.Deps{
		Config:   cfg,
		Logger:   log,
		Version:  version,
		PingUC:   pingUC,
		Postgres: db,
		Redis:    rdb,
		RabbitMQ: mqClient,
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           router.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Int("port", cfg.HTTPPort).Msg("HTTP server đang chạy")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err

	case <-ctx.Done():
		log.Info().Msg("nhận tín hiệu dừng, đang tắt êm")

		// Dùng context mới, KHÔNG dùng ctx đã bị huỷ — nếu không thì
		// Shutdown trả về ngay lập tức và request đang dở bị cắt ngang.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("tắt HTTP server: %w", err)
		}
		log.Info().Msg("api đã dừng")
		return nil
	}
}
```

### `internal/delivery/http/router/router.go`

```go
package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/yourorg/manage/internal/delivery/http/handler"
	"github.com/yourorg/manage/pkg/config"
	"github.com/yourorg/manage/pkg/httpx"
	"github.com/yourorg/manage/pkg/postgres"
	"github.com/yourorg/manage/pkg/rabbitmq"
)

type Deps struct {
	Config   *config.Config
	Logger   zerolog.Logger
	Version  string
	PingUC   handler.PingUsecase
	Postgres *postgres.DB
	Redis    *goredis.Client
	RabbitMQ *rabbitmq.Client
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()

	// Thứ tự middleware chính là thứ tự chạy, đọc từ trên xuống.
	r.Use(middleware.RequestID) // sinh request_id trước tiên
	r.Use(middleware.RealIP)    // lấy IP thật phía sau nginx
	r.Use(middleware.Recoverer) // bắt panic, không để sập cả tiến trình
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(middleware.Compress(5))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	systemHandler := handler.NewSystemHandler(d.PingUC, d.Version)

	// /health trả lời ngay, không chạm vào dependency nào.
	// Docker dùng nó để biết container còn SỐNG.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.OK(w, map[string]string{"status": "ok", "version": d.Version})
	})

	// /ready kiểm tra dependency. Dùng để biết có nên GỬI TRAFFIC vào không.
	// Phân biệt hai cái này quan trọng: database sập thì container vẫn sống
	// (không cần restart) nhưng chưa sẵn sàng phục vụ.
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		ready := true

		if err := d.Postgres.HealthCheck(ctx); err != nil {
			checks["postgres"] = err.Error()
			ready = false
		} else {
			checks["postgres"] = "ok"
		}

		if err := d.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = err.Error()
			ready = false
		} else {
			checks["redis"] = "ok"
		}

		if err := d.RabbitMQ.HealthCheck(ctx); err != nil {
			checks["rabbitmq"] = err.Error()
			ready = false
		} else {
			checks["rabbitmq"] = "ok"
		}

		status := http.StatusOK
		if !ready {
			status = http.StatusServiceUnavailable
		}
		httpx.JSON(w, status, httpx.Envelope{Data: checks})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", systemHandler.Ping)

		// Phase 1 sẽ thêm:
		// r.Route("/auth", ...)
		// r.Route("/employees", ...)
		// r.Route("/departments", ...)
	})

	return r
}
```

---

## 9. Bước 6 — `cmd/worker`

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourorg/manage/internal/delivery/consumer"
	"github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/config"
	"github.com/yourorg/manage/pkg/logger"
	"github.com/yourorg/manage/pkg/postgres"
	"github.com/yourorg/manage/pkg/rabbitmq"
)

var (
	version   = "dev"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker dừng do lỗi: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("worker")
	if err != nil {
		return err
	}

	log := logger.New("worker", cfg.LogLevel, cfg.Env)
	log.Info().
		Str("version", version).
		Str("git_sha", gitSHA).
		Str("build_time", buildTime).
		Int("concurrency", cfg.WorkerConcurrency).
		Msg("đang khởi động worker")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Worker cũng cần database vì job thật sẽ đọc ghi dữ liệu
	// (tính lương, tổng hợp chấm công...).
	db, err := postgres.New(ctx, cfg.PostgresDSN, cfg.PostgresMaxConn, log)
	if err != nil {
		return err
	}
	defer db.Close()

	mqClient, err := rabbitmq.New(ctx, cfg.RabbitMQURL, log)
	if err != nil {
		return err
	}
	defer func() { _ = mqClient.Close() }()

	dispatcher := consumer.NewDispatcher(mqClient, log)

	// Đăng ký handler cho từng loại job.
	// Phase sau chỉ cần thêm dòng vào đây.
	if err := dispatcher.Register(system.JobSystemPing, consumer.HandleSystemPing); err != nil {
		return fmt.Errorf("đăng ký job %s: %w", system.JobSystemPing, err)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := dispatcher.Run(ctx, cfg.WorkerConcurrency); err != nil {
			errCh <- err
		}
	}()

	log.Info().Msg("worker đang chờ job")

	select {
	case err := <-errCh:
		return err

	case <-ctx.Done():
		log.Info().Msg("nhận tín hiệu dừng, chờ xử lý nốt job đang cầm")

		// Dispatcher đã ngừng nhận message mới khi ctx bị huỷ.
		// Chờ thêm một khoảng để các goroutine đang chạy kịp ack.
		// Bỏ bước này thì job đang dở sẽ bị RabbitMQ trả lại queue
		// và chạy lại từ đầu — với job gửi mail nghĩa là khách nhận 2 lần.
		time.Sleep(2 * time.Second)

		log.Info().Msg("worker đã dừng")
		return nil
	}
}
```

---

## 10. Bước 7 — Air (hot reload trong container)

### `backend/.air.api.toml`

```toml
root = "/app"
tmp_dir = "tmp/api"

[build]
  cmd = "go build -o ./tmp/api/server ./cmd/api"
  bin = "./tmp/api/server"

  include_ext = ["go", "sql", "yaml", "toml"]
  include_dir = ["cmd/api", "internal", "pkg"]
  exclude_dir = ["tmp", "node_modules", ".git", "migrations"]
  exclude_regex = ["_test\\.go"]
  exclude_unchanged = true
  follow_symlink = false

  # Đợi 500ms sau thay đổi cuối mới build — tránh build 5 lần
  # khi IDE lưu nhiều file cùng lúc.
  delay = 500

  stop_on_error = true

  # Air mặc định kill -9 tiến trình cũ. Như vậy code graceful shutdown
  # bạn vừa viết KHÔNG BAO GIỜ chạy trong lúc dev, và bạn chỉ phát hiện
  # điều đó khi deploy production. Hai dòng dưới khiến Air gửi SIGINT
  # rồi chờ 3 giây — giống hệt cách `docker stop` hành xử.
  send_interrupt = true
  kill_delay = "3s"

  # BẮT BUỘC trên Windows và macOS: bind mount của Docker Desktop không
  # phát sự kiện inotify tin cậy. Không bật polling thì Air không thấy
  # file thay đổi và bạn sẽ ngồi tự hỏi vì sao code không chạy.
  poll = true
  poll_interval = 500

[log]
  time = true
  main_only = false

[color]
  main = "magenta"
  watcher = "cyan"
  build = "yellow"
  runner = "green"

[misc]
  clean_on_exit = true

[screen]
  clear_on_rebuild = false
  keep_scroll = true
```

### `backend/.air.worker.toml`

Giống hệt, đổi bốn chỗ:

```toml
tmp_dir = "tmp/worker"

[build]
  cmd = "go build -o ./tmp/worker/server ./cmd/worker"
  bin = "./tmp/worker/server"
  include_dir = ["cmd/worker", "internal", "pkg"]
```

> Vì `include_dir` có `internal` và `pkg`, sửa code dùng chung sẽ khiến **cả hai** container build lại. Đúng như mong muốn: chúng dùng chung code nên phải cùng cập nhật.

---

## 11. Bước 8 — Dockerfile

### `docker/backend/Dockerfile`

Một Dockerfile dùng chung cho cả `api` và `worker`, chọn bằng build arg `BINARY`.

```dockerfile
# syntax=docker/dockerfile:1.7

# =============================================================================
# base — phần dùng chung: toolchain Go + dependency đã tải.
# Tách riêng để tầng cache dependency không bị xoá mỗi khi sửa code.
# =============================================================================
FROM golang:1.25-alpine AS base

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

# Copy go.mod/go.sum TRƯỚC, tải dependency, RỒI mới copy code.
# Nhờ thứ tự này, sửa code không làm mất cache dependency —
# khác biệt giữa build 5 giây và build 3 phút.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# =============================================================================
# dev — dùng cho docker-compose.override.yml. Có Air, có toolchain đầy đủ.
# Source code KHÔNG copy vào image mà bind mount lúc chạy.
# =============================================================================
FROM base AS dev

ARG BINARY
ENV BINARY=${BINARY}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/air-verse/air@v1.61.7

EXPOSE 8080

# Mỗi binary có file cấu hình Air riêng.
CMD ["sh", "-c", "air -c .air.${BINARY}.toml"]

# =============================================================================
# builder — biên dịch binary tĩnh cho production.
# =============================================================================
FROM base AS builder

ARG BINARY
ARG VERSION=dev
ARG GIT_SHA=unknown
ARG BUILD_TIME=unknown

COPY . .

# CGO_ENABLED=0  → binary tĩnh, chạy được trên image không có libc
# -trimpath       → bỏ đường dẫn máy build khỏi binary (build tái lập được)
# -s -w           → bỏ ký hiệu debug, giảm khoảng 30% dung lượng
# -X              → nhúng thông tin build vào biến trong package main
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.gitSHA=${GIT_SHA} \
        -X main.buildTime=${BUILD_TIME}" \
      -o /out/server \
      ./cmd/${BINARY}

# =============================================================================
# prod — image cuối cùng. Chỉ chứa binary, chứng chỉ CA và dữ liệu múi giờ.
# Kích thước thực tế đo được: api 32.5MB, worker 27.1MB.
# =============================================================================
FROM alpine:3.21 AS prod

# wget và pgrep đã có sẵn trong busybox của alpine nên healthcheck dùng được
# ngay, không cần cài thêm gói.
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1001 -S app && \
    adduser -u 1001 -S app -G app -H -D

COPY --from=builder /out/server /usr/local/bin/server

# Chạy bằng user thường. Nếu container bị chiếm quyền,
# kẻ tấn công không có root.
USER app

ENV TZ=Asia/Ho_Chi_Minh

# ENTRYPOINT dạng exec form → binary chạy ở PID 1 → nhận trực tiếp SIGTERM
# từ `docker stop`. Dùng shell form ("/bin/sh -c ...") sẽ khiến shell nuốt
# mất tín hiệu và graceful shutdown không bao giờ chạy.
ENTRYPOINT ["/usr/local/bin/server"]
```

> **Vì sao không dùng `distroless`?** Image nhỏ hơn chút ít nhưng không có shell — khi cần `docker exec` vào xem thì bó tay. Với dự án đang xây, alpine đáng giá hơn vài MB tiết kiệm được.

### `docker/frontend/Dockerfile`

```dockerfile
# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS base
RUN corepack enable && corepack prepare pnpm@9.15.0 --activate
WORKDIR /app
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH

# =============================================================================
# deps — chỉ cài dependency. Tầng này chỉ rebuild khi lockfile đổi.
# =============================================================================
FROM base AS deps
COPY package.json pnpm-lock.yaml ./
RUN --mount=type=cache,id=pnpm,target=/pnpm/store \
    pnpm install --frozen-lockfile

# =============================================================================
# dev — Next.js dev server, source bind mount từ ngoài vào.
# =============================================================================
FROM deps AS dev

# Bind mount trên Docker Desktop không phát sự kiện file tin cậy.
# Không có hai biến này thì sửa code xong trang không tự tải lại.
ENV WATCHPACK_POLLING=true
ENV CHOKIDAR_USEPOLLING=true
ENV NEXT_TELEMETRY_DISABLED=1

EXPOSE 3000
CMD ["pnpm", "dev"]

# =============================================================================
# builder — build production. Cần next.config.js có output: "standalone".
# =============================================================================
FROM deps AS builder
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

# =============================================================================
# prod — chỉ chứa server standalone và asset tĩnh. Thực tế đo được 313MB
# (riêng node:22-alpine đã 232MB, phần ứng dụng chỉ thêm khoảng 80MB).
#
# Kế thừa thẳng từ node:22-alpine chứ KHÔNG từ `base`: stage base có cài
# corepack/pnpm, mà image production không cần trình quản lý gói — nó chỉ
# chạy `node server.js`. Kế thừa từ base làm image phình thêm 25MB vô ích.
# =============================================================================
FROM node:22-alpine AS prod

WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

RUN addgroup -g 1001 -S nodejs && \
    adduser -u 1001 -S nextjs -G nodejs

# Thư mục standalone đã chứa sẵn node_modules tối thiểu mà server cần.
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=builder --chown=nextjs:nodejs /app/public ./public

USER nextjs
EXPOSE 3000

CMD ["node", "server.js"]
```

Bật standalone trong `frontend/next.config.js`:

```js
/** @type {import('next').NextConfig} */
const nextConfig = {
  output: "standalone",
  reactStrictMode: true,
};

module.exports = nextConfig;
```

### `.dockerignore`

`backend/.dockerignore`:

```
tmp/
**/*_test.go
**/testdata/
.git/
coverage.out
```

`frontend/.dockerignore`:

```
node_modules/
.next/
.git/
npm-debug.log*
.env*.local
```

---

## 12. Bước 9 — Docker Compose

### `docker-compose.yml` — định nghĩa gốc

```yaml
name: manage

# ---------------------------------------------------------------------------
# Khối YAML dùng lại. api và worker có cùng cách build và cùng biến môi trường.
# ---------------------------------------------------------------------------
x-go-build: &go-build
  context: ./backend
  dockerfile: ../docker/backend/Dockerfile

x-go-env: &go-env
  ENV: ${ENV:-development}
  LOG_LEVEL: ${LOG_LEVEL:-debug}
  POSTGRES_DSN: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable
  REDIS_ADDR: redis:6379
  REDIS_PASSWORD: ${REDIS_PASSWORD}
  RABBITMQ_URL: amqp://${RABBITMQ_USER}:${RABBITMQ_PASSWORD}@rabbitmq:5672/
  JWT_SECRET: ${JWT_SECRET}
  JWT_ACCESS_TTL: ${JWT_ACCESS_TTL:-15m}
  JWT_REFRESH_TTL: ${JWT_REFRESH_TTL:-168h}
  TZ: Asia/Ho_Chi_Minh

services:
  # =========================================================================
  # HẠ TẦNG
  # =========================================================================
  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
      # Bật checksum để phát hiện hỏng dữ liệu sớm.
      POSTGRES_INITDB_ARGS: "--data-checksums"
      TZ: Asia/Ho_Chi_Minh
    volumes:
      - pgdata:/var/lib/postgresql/data
      # Script trong thư mục này CHỈ chạy lần đầu, khi volume còn rỗng.
      # Sửa script rồi muốn chạy lại thì phải xoá volume (make destroy).
      - ./docker/postgres/init:/docker-entrypoint-initdb.d:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s
    networks: [backend]

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: >
      redis-server
      --requirepass ${REDIS_PASSWORD}
      --appendonly yes
      --appendfsync everysec
      --maxmemory 512mb
      --maxmemory-policy allkeys-lru
    volumes:
      - redisdata:/data
    healthcheck:
      test: ["CMD-SHELL", "redis-cli -a ${REDIS_PASSWORD} ping | grep PONG"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    networks: [backend]

  rabbitmq:
    image: rabbitmq:3.13-management-alpine
    restart: unless-stopped
    environment:
      RABBITMQ_DEFAULT_USER: ${RABBITMQ_USER}
      RABBITMQ_DEFAULT_PASS: ${RABBITMQ_PASSWORD}
    volumes:
      - rabbitmqdata:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 15s
      timeout: 10s
      retries: 5
      # RabbitMQ khởi động chậm, 60s là con số thực tế chứ không thừa.
      start_period: 60s
    networks: [backend]

  minio:
    image: quay.io/minio/minio:latest
    restart: unless-stopped
    command: server /data --console-address ":9001"
    environment:
      MINIO_ROOT_USER: ${MINIO_ROOT_USER}
      MINIO_ROOT_PASSWORD: ${MINIO_ROOT_PASSWORD}
    volumes:
      - miniodata:/data
    healthcheck:
      test: ["CMD", "mc", "ready", "local"]
      interval: 15s
      timeout: 10s
      retries: 5
      start_period: 20s
    networks: [backend]

  # Container chạy một lần để tạo bucket rồi thoát.
  minio-init:
    image: quay.io/minio/mc:latest
    depends_on:
      minio:
        condition: service_healthy
    entrypoint: >
      /bin/sh -c "
      mc alias set local http://minio:9000 ${MINIO_ROOT_USER} ${MINIO_ROOT_PASSWORD};
      mc mb --ignore-existing local/avatars;
      mc mb --ignore-existing local/attachments;
      mc mb --ignore-existing local/payslips;
      mc anonymous set download local/avatars;
      echo 'đã tạo xong bucket';
      "
    restart: "no"
    networks: [backend]

  # =========================================================================
  # MIGRATION — chạy một lần rồi thoát, TRƯỚC khi api và worker khởi động.
  #
  # Tách riêng để khi scale api lên nhiều replica, migration không chạy
  # đồng thời nhiều lần gây xung đột khoá.
  # =========================================================================
  migrate:
    image: migrate/migrate:v4.18.1
    volumes:
      - ./backend/migrations:/migrations:ro
    command:
      - "-path=/migrations"
      - "-database=postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
      - "up"
    depends_on:
      postgres:
        condition: service_healthy
    restart: "no"
    networks: [backend]

  # =========================================================================
  # ỨNG DỤNG
  # =========================================================================
  api:
    build:
      <<: *go-build
      target: dev
      args:
        BINARY: api
    restart: unless-stopped
    environment:
      <<: *go-env
      APP_NAME: api
      HTTP_PORT: 8080
      CORS_ALLOWED_ORIGINS: http://localhost,http://localhost:3000
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      rabbitmq:
        condition: service_healthy
      migrate:
        condition: service_completed_successfully
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 15s
      timeout: 5s
      retries: 5
      start_period: 40s
    # Dài vì Phase 5 sẽ giữ kết nối WebSocket cần đóng sạch.
    stop_grace_period: 60s
    networks: [backend, proxy]

  worker:
    build:
      <<: *go-build
      target: dev
      args:
        BINARY: worker
    restart: unless-stopped
    environment:
      <<: *go-env
      APP_NAME: worker
      WORKER_CONCURRENCY: 5
    depends_on:
      postgres:
        condition: service_healthy
      rabbitmq:
        condition: service_healthy
      migrate:
        condition: service_completed_successfully
    # Worker không có HTTP endpoint. Kiểm tra bằng cách xem tiến trình còn chạy không.
    healthcheck:
      test: ["CMD-SHELL", "pgrep -f 'tmp/worker/server' > /dev/null || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 40s
    # Đủ dài để xử lý nốt job đang cầm.
    stop_grace_period: 120s
    networks: [backend]

  frontend:
    build:
      context: ./frontend
      dockerfile: ../docker/frontend/Dockerfile
      target: dev
    restart: unless-stopped
    environment:
      NODE_ENV: development
      NEXT_PUBLIC_API_URL: http://localhost/api/v1
      NEXT_PUBLIC_WS_URL: ws://localhost/ws
    depends_on:
      api:
        condition: service_healthy
    networks: [proxy]

  # =========================================================================
  # CỔNG VÀO — container DUY NHẤT mở port ra host ở production
  # =========================================================================
  nginx:
    image: nginx:1.27-alpine
    restart: unless-stopped
    ports:
      - "${NGINX_PORT:-80}:80"
    volumes:
      - ./docker/nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ./docker/nginx/conf.d:/etc/nginx/conf.d:ro
    depends_on:
      api:
        condition: service_healthy
    healthcheck:
      # Dùng 127.0.0.1 chứ KHÔNG dùng localhost: `listen 80` của nginx chỉ
      # bind IPv4, trong khi localhost trong container phân giải ra ::1 trước.
      # Dùng localhost thì healthcheck luôn báo "connection refused" dù
      # nginx chạy hoàn toàn bình thường.
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://127.0.0.1/health"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 10s
    networks: [proxy]

networks:
  # Mạng nội bộ: database, cache, message broker, worker.
  backend:
    driver: bridge
  # Mạng biên: nginx, api, frontend.
  proxy:
    driver: bridge

volumes:
  pgdata:
  redisdata:
  rabbitmqdata:
  miniodata:
```

### `docker-compose.override.yml` — chỉ dev, tự động nạp

```yaml
# File này Docker Compose TỰ ĐỘNG nạp khi chạy `docker compose up`.
# Vì vậy dev là mặc định, còn production phải chỉ định file rõ ràng —
# đúng chiều an toàn: quên gõ lệnh thì ra dev, không phải ra production.

services:
  # ---------------------------------------------------------------------
  # Bind mount source code vào container để Air thấy được thay đổi.
  #
  # Volume ẩn danh cho /app/tmp rất quan trọng: nó ngăn thư mục build của
  # Air bên trong container ghi ngược ra host, tránh xung đột khi api và
  # worker cùng build, và tránh làm bẩn repo bằng binary Linux.
  # ---------------------------------------------------------------------
  api:
    volumes:
      - ./backend:/app
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/root/.cache/go-build
      - api-tmp:/app/tmp
    ports:
      - "${API_PORT:-8080}:8080"

  worker:
    volumes:
      - ./backend:/app
      - go-mod-cache:/go/pkg/mod
      - go-build-cache:/root/.cache/go-build
      - worker-tmp:/app/tmp

  frontend:
    volumes:
      - ./frontend:/app
      # node_modules để trong named volume, KHÔNG bind mount từ host.
      # Bind mount sẽ khiến node_modules của Windows đè lên bản Linux
      # trong container — các package có binary native sẽ hỏng.
      - frontend-node-modules:/app/node_modules
      - frontend-next:/app/.next
    ports:
      - "${FRONTEND_PORT:-3000}:3000"

  # ---------------------------------------------------------------------
  # Mở port hạ tầng ra host để debug bằng công cụ trên máy thật.
  # KHÔNG có ở production.
  # ---------------------------------------------------------------------
  postgres:
    ports:
      - "${POSTGRES_PORT:-5432}:5432"

  redis:
    ports:
      - "${REDIS_PORT:-6379}:6379"

  rabbitmq:
    ports:
      - "${RABBITMQ_PORT:-5672}:5672"
      - "${RABBITMQ_UI_PORT:-15672}:15672"   # giao diện quản trị

  minio:
    ports:
      - "${MINIO_API_PORT:-9000}:9000"
      - "${MINIO_UI_PORT:-9001}:9001"

  # ---------------------------------------------------------------------
  # Công cụ chỉ dùng khi dev
  # ---------------------------------------------------------------------
  adminer:
    image: adminer:latest
    restart: unless-stopped
    environment:
      ADMINER_DEFAULT_SERVER: postgres
      ADMINER_DESIGN: dracula
    ports:
      - "${ADMINER_PORT:-8081}:8080"
    depends_on:
      postgres:
        condition: service_healthy
    networks: [backend]

  # Máy chủ mail giả — bắt mọi email hệ thống gửi đi, xem trên giao diện web.
  # Nhờ nó, khi làm chức năng quên mật khẩu ở Phase 1 bạn không cần SMTP thật.
  mailhog:
    image: mailhog/mailhog:latest
    restart: unless-stopped
    ports:
      - "${MAILHOG_UI_PORT:-8025}:8025"
    networks: [backend]

volumes:
  go-mod-cache:
  go-build-cache:
  api-tmp:
  worker-tmp:
  frontend-node-modules:
  frontend-next:
```

### `docker-compose.prod.yml`

```yaml
# Chạy bằng:
#   docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
#
# Lưu ý: khi chỉ định -f, Compose KHÔNG tự nạp docker-compose.override.yml,
# nên mọi thiết lập dev ở file đó bị loại bỏ hoàn toàn.

x-logging: &logging
  driver: json-file
  options:
    max-size: "20m"
    max-file: "5"

x-prod-common: &prod-common
  restart: unless-stopped
  logging: *logging

services:
  postgres:
    <<: *prod-common
    # Không mở port ra host. Muốn truy cập thì SSH vào server rồi docker exec.
    ports: []
    deploy:
      resources:
        limits: { cpus: "2.0", memory: 2G }
        reservations: { cpus: "0.5", memory: 512M }

  redis:
    <<: *prod-common
    ports: []
    deploy:
      resources:
        limits: { cpus: "1.0", memory: 768M }

  rabbitmq:
    <<: *prod-common
    ports: []
    deploy:
      resources:
        limits: { cpus: "1.0", memory: 1G }

  minio:
    <<: *prod-common
    ports: []

  api:
    <<: *prod-common
    # Không build tại chỗ — kéo image đã build và quét bảo mật trong CI.
    build: null
    image: ghcr.io/yourorg/manage-api:${IMAGE_TAG:-latest}
    volumes: []
    ports: []
    read_only: true          # container không ghi được vào filesystem
    tmpfs: [/tmp]            # trừ /tmp
    security_opt:
      - no-new-privileges:true
    cap_drop: [ALL]
    environment:
      ENV: production
      LOG_LEVEL: info
    deploy:
      replicas: 2
      resources:
        limits: { cpus: "1.0", memory: 512M }

  worker:
    <<: *prod-common
    build: null
    image: ghcr.io/yourorg/manage-worker:${IMAGE_TAG:-latest}
    volumes: []
    read_only: true
    tmpfs: [/tmp]
    security_opt:
      - no-new-privileges:true
    cap_drop: [ALL]
    environment:
      ENV: production
      LOG_LEVEL: info
      WORKER_CONCURRENCY: 10
    deploy:
      replicas: 2
      resources:
        limits: { cpus: "1.0", memory: 512M }

  frontend:
    <<: *prod-common
    build: null
    image: ghcr.io/yourorg/manage-frontend:${IMAGE_TAG:-latest}
    volumes: []
    ports: []
    environment:
      NODE_ENV: production
      NEXT_PUBLIC_API_URL: https://${DOMAIN}/api/v1
      NEXT_PUBLIC_WS_URL: wss://${DOMAIN}/ws

  nginx:
    <<: *prod-common
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./docker/nginx/nginx.conf:/etc/nginx/nginx.conf:ro
      - ./docker/nginx/conf.d:/etc/nginx/conf.d:ro
      - ./docker/nginx/certs:/etc/nginx/certs:ro
```

---

## 13. Bước 10 — Nginx

### `docker/nginx/nginx.conf`

```nginx
user  nginx;
worker_processes  auto;

error_log  /var/log/nginx/error.log warn;
pid        /var/run/nginx.pid;

events {
    worker_connections  4096;
    use epoll;
    multi_accept on;
}

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;

    # Ghi kèm request_id để nối log nginx với log của api và worker.
    log_format main '$remote_addr - $remote_user [$time_local] "$request" '
                    '$status $body_bytes_sent "$http_referer" '
                    '"$http_user_agent" rt=$request_time '
                    'rid=$http_x_request_id';

    access_log  /var/log/nginx/access.log main;

    sendfile        on;
    tcp_nopush      on;
    tcp_nodelay     on;
    keepalive_timeout  65;
    server_tokens   off;

    client_max_body_size 25m;   # khớp với giới hạn upload tệp đính kèm

    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/json application/javascript
               application/x-javascript text/xml application/xml image/svg+xml;

    # Map này cần cho WebSocket: chuyển giá trị header Upgrade thành
    # "upgrade" hoặc "close" tuỳ request. Thiếu nó thì WS không hoạt động.
    map $http_upgrade $connection_upgrade {
        default upgrade;
        ''      close;
    }

    limit_req_zone $binary_remote_addr zone=api_limit:10m rate=30r/s;
    limit_req_zone $binary_remote_addr zone=auth_limit:10m rate=5r/m;

    include /etc/nginx/conf.d/*.conf;
}
```

### `docker/nginx/conf.d/app.conf`

```nginx
upstream api_upstream {
    # Docker DNS tự phân giải ra nhiều IP khi scale, nginx luân phiên gửi.
    server api:8080;
    keepalive 32;
}

upstream frontend_upstream {
    server frontend:3000;
    keepalive 16;
}

server {
    listen 80;
    server_name _;

    # --- Header bảo mật ---
    add_header X-Frame-Options           "SAMEORIGIN"    always;
    add_header X-Content-Type-Options    "nosniff"       always;
    add_header Referrer-Policy           "strict-origin-when-cross-origin" always;
    add_header X-XSS-Protection          "1; mode=block" always;

    # Health check của chính nginx, không chuyển tiếp đi đâu.
    location = /health {
        access_log off;
        # default_type chứ không phải add_header: chỉ thị `return` bỏ qua
        # Content-Type đặt bằng add_header, client sẽ nhận
        # application/octet-stream và một số HTTP client đọc ra mảng byte.
        default_type application/json;
        return 200 '{"status":"ok"}';
    }

    # --- REST API ---
    location /api/ {
        limit_req zone=api_limit burst=60 nodelay;

        proxy_pass http://api_upstream;
        proxy_http_version 1.1;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection        "";

        proxy_connect_timeout 5s;
        proxy_send_timeout    60s;
        proxy_read_timeout    60s;
    }

    # --- Đăng nhập: giới hạn chặt hơn để chống dò mật khẩu ---
    location = /api/v1/auth/login {
        limit_req zone=auth_limit burst=5 nodelay;

        proxy_pass http://api_upstream;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # =======================================================================
    # WEBSOCKET — phần dễ sai nhất trong toàn bộ cấu hình nginx.
    #
    # Thiếu proxy_read_timeout dài, nginx đóng kết nối sau 60 giây mặc định.
    # Với dự án này, đó cũng là lúc dữ liệu chấm công bắt đầu sai.
    # Cấu hình sẵn từ Phase 0 dù Phase 5 mới dùng tới.
    # =======================================================================
    location /ws {
        proxy_pass http://api_upstream;

        proxy_http_version 1.1;
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Giữ kết nối tối đa 1 giờ không có dữ liệu.
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;

        # Tắt buffer: tin nhắn phải đẩy tới client ngay, không gom lại.
        proxy_buffering off;
        proxy_cache off;
    }

    # --- Asset tĩnh của Next.js: cache lâu vì tên file có hash ---
    location /_next/static/ {
        proxy_pass http://frontend_upstream;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_cache_valid 200 365d;
        add_header Cache-Control "public, max-age=31536000, immutable";
    }

    # --- Mọi thứ còn lại về frontend ---
    location / {
        proxy_pass http://frontend_upstream;
        proxy_http_version 1.1;

        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Next.js dev dùng WebSocket cho hot reload — cần hai dòng này.
        proxy_set_header Upgrade    $http_upgrade;
        proxy_set_header Connection $connection_upgrade;
    }
}
```

### `docker/postgres/init/01-extensions.sql`

```sql
-- Chạy một lần duy nhất khi volume pgdata còn rỗng.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "unaccent";
```

> `unaccent` và `pg_trgm` phục vụ tìm kiếm tiếng Việt không dấu — gõ "nguyen van a" vẫn tìm ra "Nguyễn Văn A". Cài sẵn từ đầu để Phase 1 khỏi phải sửa.

---

## 14. Bước 11 — Makefile (và `dev.ps1` cho Windows)

**Windows không có sẵn `make`.** Git Bash cũng không kèm theo. Dự án vì vậy có hai file song song:

| File | Dùng ở đâu |
|---|---|
| `Makefile` | WSL, Linux, macOS, CI |
| `dev.ps1` | PowerShell trên Windows — không cần cài thêm gì |

Danh sách lệnh của hai file phải luôn khớp nhau. Cách dùng `dev.ps1`:

```powershell
.\dev.ps1 help
.\dev.ps1 up
.\dev.ps1 logs worker
.\dev.ps1 smoke
```

> **Khi viết `dev.ps1`, phải lưu file với UTF-8 BOM.** Windows PowerShell 5.1 đọc file `.ps1` không có BOM theo bảng mã ANSI, làm hỏng mọi ký tự tiếng Việt — và ký tự hỏng thường phá luôn cú pháp, cho ra lỗi parser rất khó hiểu. Kiểm tra và sửa:
>
> ```powershell
> $p = "dev.ps1"
> $c = [System.IO.File]::ReadAllText($p, [System.Text.UTF8Encoding]::new($false))
> [System.IO.File]::WriteAllText($p, $c, [System.Text.UTF8Encoding]::new($true))
> ```
>
> Ba byte đầu file sau đó phải là `EF BB BF`.

### Makefile

```makefile
.DEFAULT_GOAL := help
SHELL := /bin/bash

# Nạp .env vào môi trường của make.
# Thiếu dòng này thì mọi target dùng $(POSTGRES_USER), $(PUBLIC_BASE_URL)...
# sẽ nhận chuỗi rỗng và lặng lẽ chạy sai — make KHÔNG tự đọc .env như
# docker compose. Dấu - ở đầu để không lỗi khi chưa có .env.
-include .env
export

PUBLIC_BASE_URL ?= http://localhost

COMPOSE      := docker compose
COMPOSE_PROD := docker compose -f docker-compose.yml -f docker-compose.prod.yml

GIT_SHA    := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: help
help: ## Hiện danh sách lệnh
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- môi trường
.PHONY: init
init: ## Thiết lập lần đầu: tạo .env, build image, khởi động
	@test -f .env || cp .env.example .env
	@$(COMPOSE) build
	@$(COMPOSE) up -d
	@echo ""
	@echo "Xong. Mở http://localhost"

.PHONY: up
up: ## Khởi động toàn bộ (dev)
	$(COMPOSE) up -d

.PHONY: down
down: ## Dừng toàn bộ, GIỮ NGUYÊN dữ liệu
	$(COMPOSE) down

.PHONY: destroy
destroy: ## Dừng và XOÁ SẠCH dữ liệu (cẩn thận)
	@read -p "Xoá toàn bộ volume, mất hết dữ liệu. Gõ 'yes' để xác nhận: " ans; \
	[ "$$ans" = "yes" ] && $(COMPOSE) down -v || echo "Đã huỷ"

.PHONY: rebuild
rebuild: ## Build lại image của một service: make rebuild s=api
	$(COMPOSE) build $(s) && $(COMPOSE) up -d $(s)

.PHONY: restart
restart: ## Khởi động lại: make restart s=api
	$(COMPOSE) restart $(s)

.PHONY: logs
logs: ## Xem log: make logs s=api (bỏ s để xem tất cả)
	$(COMPOSE) logs -f --tail=100 $(s)

.PHONY: ps
ps: ## Trạng thái container
	$(COMPOSE) ps

.PHONY: sh
sh: ## Vào shell container: make sh s=api
	$(COMPOSE) exec $(s) sh

# ------------------------------------------------------------------ migration
.PHONY: migrate
migrate: ## Chạy migration
	$(COMPOSE) run --rm migrate

.PHONY: migrate-down
migrate-down: ## Lùi 1 bước migration
	$(COMPOSE) run --rm migrate \
		-path=/migrations \
		-database="postgres://$${POSTGRES_USER}:$${POSTGRES_PASSWORD}@postgres:5432/$${POSTGRES_DB}?sslmode=disable" \
		down 1

.PHONY: migrate-create
migrate-create: ## Tạo migration mới: make migrate-create n=create_users
	$(COMPOSE) run --rm --entrypoint migrate migrate \
		create -ext sql -dir /migrations -seq $(n)

# ----------------------------------------------------- kiểm thử & chất lượng
.PHONY: test
test: ## Chạy unit test
	$(COMPOSE) exec -T api sh -c "cd /app && go test ./... -race -count=1"

.PHONY: cover
cover: ## Chạy test kèm báo cáo coverage
	$(COMPOSE) exec -T api sh -c \
		"cd /app && go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out | tail -1"

.PHONY: lint
lint: ## Kiểm tra code Go
	docker run --rm -v "$$(pwd)/backend:/app" -w /app \
		golangci/golangci-lint:v2.13.2 golangci-lint run ./... --timeout 5m

.PHONY: tidy
tidy: ## Dọn go.mod
	$(COMPOSE) exec -T api sh -c "cd /app && go mod tidy"

# ----------------------------------------------------------------- tiện ích
.PHONY: psql
psql: ## Mở psql
	$(COMPOSE) exec postgres psql -U $${POSTGRES_USER} -d $${POSTGRES_DB}

.PHONY: redis-cli
redis-cli: ## Mở redis-cli
	$(COMPOSE) exec redis redis-cli -a $${REDIS_PASSWORD}

.PHONY: smoke
smoke: ## Kiểm tra nhanh toàn hệ thống sau khi up
	@echo "--- nginx ---"    && curl -fsS http://localhost/health      | head -c 200; echo
	@echo "--- api ready ---" && curl -fsS http://localhost:8080/ready  | head -c 300; echo
	@echo "--- ping ---"     && curl -fsS http://localhost/api/v1/ping | head -c 400; echo
	@echo ""
	@echo "--- log worker (job vừa nhận) ---"
	@$(COMPOSE) logs --tail=5 worker | grep "worker đã xử lý job" || echo "CHƯA THẤY JOB — kiểm tra RabbitMQ"

# -------------------------------------------------------------- production
.PHONY: prod-up
prod-up: ## Khởi động production
	$(COMPOSE_PROD) up -d

.PHONY: prod-logs
prod-logs: ## Log production
	$(COMPOSE_PROD) logs -f --tail=100 $(s)

.PHONY: build-images
build-images: ## Build image production tại máy
	docker build -f docker/backend/Dockerfile --target prod \
		--build-arg BINARY=api --build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t ghcr.io/yourorg/manage-api:$(GIT_SHA) ./backend
	docker build -f docker/backend/Dockerfile --target prod \
		--build-arg BINARY=worker --build-arg GIT_SHA=$(GIT_SHA) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t ghcr.io/yourorg/manage-worker:$(GIT_SHA) ./backend
	docker build -f docker/frontend/Dockerfile --target prod \
		-t ghcr.io/yourorg/manage-frontend:$(GIT_SHA) ./frontend
```

---

## 15. Bước 12 — Frontend tối thiểu

Có thể dùng `create-next-app`, nhưng nó chạy tương tác và tạo ra nhiều thứ thừa. Viết tay vài file lại nhanh và kiểm soát được hơn.

**`frontend/package.json`**

```json
{
  "name": "manage-frontend",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint ."
  },
  "dependencies": {
    "next": "16.3.5",
    "react": "19.3.0",
    "react-dom": "19.3.0"
  },
  "devDependencies": {
    "@eslint/eslintrc": "3.2.0",
    "@types/node": "22.10.7",
    "@types/react": "19.0.7",
    "@types/react-dom": "19.0.3",
    "autoprefixer": "10.4.20",
    "eslint": "9.39.5",
    "eslint-config-next": "16.3.5",
    "postcss": "8.5.1",
    "tailwindcss": "3.4.17",
    "typescript": "5.7.3"
  }
}
```

> **Ba lưu ý về phiên bản**
>
> - **Next 16, không phải 15.** Next 15.1.x có lỗ hổng bảo mật (CVE-2025-66478). Cứ cài là pnpm cảnh báo ngay.
> - **`next lint` đã bị bỏ ở Next 16.** Script `lint` phải gọi thẳng `eslint .`.
> - **ESLint giữ ở 9.x.** Bản 10 đã ra nhưng `eslint-plugin-react` mà `eslint-config-next` phụ thuộc chỉ chấp nhận tới `^9.7`. Nâng lên 10 sẽ báo unmet peer dependency. pnpm có cảnh báo "9.x no longer supported" — đó chỉ là thông báo vòng đời, không phải lỗ hổng, cứ bỏ qua cho tới khi hệ sinh thái bắt kịp.

**`frontend/eslint.config.mjs`** — ESLint 9 dùng flat config, `.eslintrc.json` không còn được đọc mặc định:

```js
import { dirname } from "path";
import { fileURLToPath } from "url";
import { FlatCompat } from "@eslint/eslintrc";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

const compat = new FlatCompat({ baseDirectory: __dirname });

const eslintConfig = [
  ...compat.extends("next/core-web-vitals", "next/typescript"),
  { ignores: [".next/**", "node_modules/**"] },
];

export default eslintConfig;
```

Ngoài ra cần: `tsconfig.json`, `tailwind.config.ts`, `postcss.config.mjs`, `src/app/globals.css`, `src/app/layout.tsx` (nội dung chuẩn của Next, xem file thật trong repo).

**Bắt buộc tạo thư mục `frontend/public/`** kể cả khi rỗng — Dockerfile production có `COPY /app/public ./public`, thiếu thư mục thì build hỏng với thông báo khó hiểu `"/app/public": not found`. Đặt một file `.gitkeep` vào đó.

**Sinh lockfile** (Dockerfile dùng `--frozen-lockfile` nên bắt buộc phải có):

```bash
docker run --rm -v "D:/Projects/manage/frontend:/app" -w /app node:22-alpine \
  sh -c "corepack enable && corepack prepare pnpm@9.15.0 --activate && pnpm install --lockfile-only"
```

Chỉnh `next.config.js` bật `output: "standalone"` (xem mục 11), rồi tạo trang kiểm chứng:

`frontend/src/app/page.tsx`

```tsx
async function fetchPing() {
  // Gọi từ phía server của Next.js nên dùng tên service trong mạng Docker,
  // không dùng localhost — localhost ở đây là chính container frontend.
  const res = await fetch("http://api:8080/api/v1/ping", { cache: "no-store" });
  if (!res.ok) throw new Error(`API trả về ${res.status}`);
  return res.json();
}

export default async function Home() {
  let result: unknown;
  let error: string | null = null;

  try {
    result = await fetchPing();
  } catch (e) {
    error = e instanceof Error ? e.message : "Lỗi không xác định";
  }

  return (
    <main className="mx-auto max-w-2xl p-8">
      <h1 className="text-2xl font-semibold">Manage — Phase 0</h1>
      <p className="mt-2 text-sm text-neutral-500">
        Trang này gọi api, api đọc PostgreSQL và đẩy một job sang worker.
        Thấy dữ liệu bên dưới nghĩa là bộ khung đã thông.
      </p>

      {error ? (
        <pre className="mt-6 rounded bg-red-50 p-4 text-sm text-red-700">{error}</pre>
      ) : (
        <pre className="mt-6 overflow-x-auto rounded bg-neutral-900 p-4 text-sm text-neutral-100">
          {JSON.stringify(result, null, 2)}
        </pre>
      )}
    </main>
  );
}
```

---

## 16. Bước 13 — CI

### `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main, develop]
  pull_request:
    branches: [main, develop]

env:
  REGISTRY: ghcr.io
  GO_VERSION: "1.25"

jobs:
  backend:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_USER: test
          POSTGRES_PASSWORD: test
          POSTGRES_DB: test
        options: >-
          --health-cmd "pg_isready -U test"
          --health-interval 10s --health-timeout 5s --health-retries 5
        ports: ["5432:5432"]
      redis:
        image: redis:7-alpine
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s --health-timeout 5s --health-retries 5
        ports: ["6379:6379"]
      rabbitmq:
        image: rabbitmq:3.13-alpine
        options: >-
          --health-cmd "rabbitmq-diagnostics -q ping"
          --health-interval 15s --health-timeout 10s --health-retries 5
        ports: ["5672:5672"]

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache-dependency-path: backend/go.sum

      - name: Kiểm tra go.mod đã tidy
        working-directory: backend
        run: |
          go mod tidy
          git diff --exit-code go.mod go.sum

      - name: Lint
        uses: golangci/golangci-lint-action@v7
        with:
          version: v2.13.2
          working-directory: backend

      - name: Chạy migration
        run: |
          curl -sSL https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz \
            | tar xvz migrate
          ./migrate -path backend/migrations \
            -database "postgres://test:test@localhost:5432/test?sslmode=disable" up

      - name: Test
        working-directory: backend
        env:
          POSTGRES_DSN: postgres://test:test@localhost:5432/test?sslmode=disable
          REDIS_ADDR: localhost:6379
          RABBITMQ_URL: amqp://guest:guest@localhost:5672/
        run: go test ./... -race -count=1 -coverprofile=coverage.out

      - name: Báo cáo coverage
        working-directory: backend
        run: go tool cover -func=coverage.out | tail -1

  frontend:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: pnpm/action-setup@v4
        with: { version: 9 }
      - uses: actions/setup-node@v4
        with:
          node-version: 22
          cache: pnpm
          cache-dependency-path: frontend/pnpm-lock.yaml

      - working-directory: frontend
        run: pnpm install --frozen-lockfile
      - working-directory: frontend
        run: pnpm lint
      - working-directory: frontend
        run: pnpm build

  docker:
    runs-on: ubuntu-latest
    needs: [backend, frontend]
    permissions:
      contents: read
      packages: write
    strategy:
      fail-fast: false
      matrix:
        include:
          - { name: api,      context: ./backend,  dockerfile: docker/backend/Dockerfile,  binary: api }
          - { name: worker,   context: ./backend,  dockerfile: docker/backend/Dockerfile,  binary: worker }
          - { name: frontend, context: ./frontend, dockerfile: docker/frontend/Dockerfile, binary: "" }

    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3

      - name: Đăng nhập GHCR
        if: github.ref == 'refs/heads/main'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build và đẩy image
        uses: docker/build-push-action@v6
        with:
          context: ${{ matrix.context }}
          file: ${{ matrix.dockerfile }}
          target: prod
          build-args: |
            BINARY=${{ matrix.binary }}
            VERSION=${{ github.ref_name }}
            GIT_SHA=${{ github.sha }}
          # Chỉ đẩy lên registry khi vào main. PR chỉ build để kiểm tra.
          push: ${{ github.ref == 'refs/heads/main' }}
          tags: |
            ${{ env.REGISTRY }}/${{ github.repository_owner }}/manage-${{ matrix.name }}:${{ github.sha }}
            ${{ env.REGISTRY }}/${{ github.repository_owner }}/manage-${{ matrix.name }}:latest
          cache-from: type=gha,scope=${{ matrix.name }}
          cache-to: type=gha,mode=max,scope=${{ matrix.name }}
          load: ${{ github.ref != 'refs/heads/main' }}

      - name: Quét lỗ hổng bảo mật
        uses: aquasecurity/trivy-action@master
        with:
          image-ref: ${{ env.REGISTRY }}/${{ github.repository_owner }}/manage-${{ matrix.name }}:${{ github.sha }}
          format: table
          exit-code: "1"
          ignore-unfixed: true
          severity: HIGH,CRITICAL
```

---

## 17. Kiểm chứng Phase 0

```bash
make init
make ps      # mọi container phải healthy, chờ khoảng 60 giây lần đầu
make smoke
```

Kết quả `make smoke` mong đợi:

```json
{
  "data": {
    "database_time": "2026-09-15T10:23:45.123456+07:00",
    "redis_ok": true,
    "job_queued": true,
    "request_id": "8f3a1c2e-...",
    "version": "dev"
  }
}
```

kèm dòng log của worker:

```
INF worker đã xử lý job app=worker job=system.ping request_id=8f3a1c2e-...
```

Đọc kết quả này cho biết:

| Bằng chứng | Chứng minh điều gì |
|---|---|
| `database_time` | api kết nối và truy vấn được PostgreSQL |
| `redis_ok: true` | Redis hoạt động |
| `job_queued: true` | api đẩy được message lên RabbitMQ |
| Log worker | worker nhận và xử lý được job |
| `request_id` khớp nhau ở cả hai nơi | Lần vết xuyên tiến trình hoạt động |

**Điểm cuối cùng là quan trọng nhất.** Khi Phase 4 có job tính lương chạy sai, bạn sẽ lấy `request_id` từ log HTTP và tìm ra chính xác job nào đã chạy. Không có nó thì việc đó gần như bất khả thi.

### Kiểm tra hot reload

```bash
make logs s=api
```

Sửa chuỗi `"worker đã xử lý job"` trong `consumer/system.go`, lưu lại. Log của **cả `api` và `worker`** phải hiện Air build lại — vì cả hai cùng theo dõi `internal/`.

### Kiểm tra graceful shutdown

```bash
docker compose stop api
make logs s=api
```

Phải thấy `nhận tín hiệu dừng, đang tắt êm` rồi `api đã dừng`. Không thấy nghĩa là `ENTRYPOINT` đang ở shell form, hoặc code chưa bắt `SIGTERM`.

### Kiểm tra dead-letter queue

Mở http://localhost:15672 (tài khoản trong `.env`), vào tab Queues. Phải thấy `manage.jobs.queue` và `manage.jobs.dead`. Đẩy thử một message rác vào exchange `manage.jobs` với routing key `system.ping` — nó phải rơi sang `manage.jobs.dead` chứ không lặp vô hạn.

---

## 18. Lỗi thường gặp

| Hiện tượng | Nguyên nhân | Cách xử lý |
|---|---|---|
| Air không build lại khi sửa file | Thiếu `poll = true` trong `.air.*.toml` | Bind mount Docker Desktop không phát inotify — bắt buộc bật polling |
| `api` khởi động rồi chết ngay | `migrate` chưa chạy xong, hoặc thiếu biến môi trường | Xem `docker compose logs migrate`; config validate sẽ in rõ thiếu biến nào |
| `job_queued: false` | RabbitMQ chưa sẵn sàng khi api khởi động | Kiểm tra `depends_on` có `condition: service_healthy` không |
| Worker nhận job nhưng không thấy log | Log level đang là `info`, handler log ở mức `debug` | Đổi `LOG_LEVEL=debug` trong `.env` |
| Message lặp vô hạn, worker ăn hết CPU | Nack với `requeue = true` | Phải nack với `requeue = false` để message rơi sang dead-letter |
| Job chạy 2 lần | Worker bị kill giữa chừng, message chưa ack nên RabbitMQ trả lại queue | Bình thường với at-least-once. Job phải viết idempotent — quan trọng từ Phase 4 |
| WebSocket đứt sau đúng 60 giây | Thiếu `proxy_read_timeout` trong nginx | Đặt `3600s` cho `location /ws` |
| `go build` báo thiếu package sau `go get` | `go.sum` trong container khác trên host | `make tidy` rồi restart container |
| Container `frontend` lỗi module native | `node_modules` của Windows bị bind mount đè lên | Dùng named volume như trong `docker-compose.override.yml` |
| `docker compose up` rất chậm trên Windows | Bind mount NTFS chậm cố hữu | Đặt repo trong WSL2 filesystem (`\\wsl$\...`) thay vì ổ `D:` |
| Sửa `01-extensions.sql` không có tác dụng | Script init chỉ chạy khi volume rỗng | `make destroy` rồi `make up` — sẽ mất hết dữ liệu |
| Port 80 hoặc 9001 bị chiếm | IIS, Laravel Herd, XAMPP | Đổi `NGINX_PORT` / `MINIO_UI_PORT` trong `.env`, nhớ đổi cả `PUBLIC_BASE_URL` cho khớp |

### Những lỗi gặp thật khi dựng Phase 0

Phần dưới là lỗi đã thực sự xảy ra trong lần dựng đầu tiên, không phải phỏng đoán.

| Hiện tượng | Nguyên nhân | Cách xử lý |
|---|---|---|
| `cannot call pointer method Info on zerolog.Logger` | `zerolog.Logger` có method pointer receiver; giá trị trả về từ hàm không lấy địa chỉ được | Gán ra biến trước: `log := logger.FromContext(ctx)` rồi `log.Info()` |
| `pull access denied for minio/mc` | MinIO đã rút image khỏi Docker Hub | Dùng `quay.io/minio/minio` và `quay.io/minio/mc` |
| nginx luôn `unhealthy` dù chạy bình thường | `listen 80` chỉ bind IPv4, còn `localhost` trong container phân giải ra `::1` trước | Healthcheck dùng `http://127.0.0.1/health` |
| `/health` trả về mảng byte thay vì chuỗi JSON | Chỉ thị `return` bỏ qua Content-Type đặt bằng `add_header` | Dùng `default_type application/json;` |
| `golangci-lint: the Go language version (go1.23) ... lower than the targeted Go version (1.25)` | Bản v1.62 build bằng Go 1.23 | Nâng lên golangci-lint v2.x (schema config khác hẳn v1, phải viết lại `.golangci.yml` với `version: "2"`) |
| `gosec G118: Goroutine uses context.Background` | Vòng lặp kết nối lại RabbitMQ không dừng khi ứng dụng tắt | Lưu context vào struct `Client`, huỷ nó trong `Close()` |
| `"/pnpm-lock.yaml": not found` khi build | Dockerfile dùng `--frozen-lockfile` nhưng chưa sinh lockfile | Chạy `pnpm install --lockfile-only` trong container node |
| `"/app/public": not found` khi build frontend prod | Chưa có thư mục `frontend/public/` | Tạo thư mục kèm `.gitkeep` |
| `make: command not found` | Windows không có sẵn `make` | Dùng `.\dev.ps1` thay thế |
| `dev.ps1` lỗi parser, chữ tiếng Việt thành `Lá»‡nh` | PowerShell 5.1 đọc file `.ps1` không BOM theo ANSI | Lưu lại file với UTF-8 **có BOM** |
| `docker run -v` báo `working directory 'C:/Program Files/Git/app' is invalid` | Git Bash tự chuyển đổi đường dẫn kiểu Unix | Đặt `MSYS_NO_PATHCONV=1` trước lệnh docker |

---

## 19. Việc chưa làm ở Phase 0 (cố ý để lại)

Những thứ này có thể làm ngay nhưng làm sớm sẽ không học được gì vì chưa có ngữ cảnh:

- **Thử lại job lỗi có giãn cách.** Hiện job lỗi đi thẳng dead-letter. Cần khi Phase 4 gửi email — lỗi mạng tạm thời rất hay xảy ra. Cách thông dụng: queue trung gian có TTL rồi quay lại queue chính.
- **Outbox pattern.** Bảo đảm sự kiện không mất khi api ghi database thành công nhưng chết trước lúc publish. Cần từ Phase 2 khi sự kiện bắt đầu mang ý nghĩa nghiệp vụ.
- **Job idempotent.** Với at-least-once, mọi job phải chạy nhiều lần mà kết quả không đổi. Bắt buộc từ Phase 4 — tính lương hai lần là chuyện lớn.
- **`go-arch-lint` trong CI.** Chặn import sai tầng tự động. Thêm khi đã có vài module thật để kiểm tra.
- **OpenTelemetry + Jaeger.** `request_id` đủ dùng cho hai tiến trình. Cần khi luồng xử lý trở nên phức tạp.
- **Rate limit ở tầng ứng dụng.** Nginx đã giới hạn theo IP. Giới hạn theo tài khoản cần Redis và cần biết ai đang gọi — làm ở Phase 1 cùng với auth.

---

## 20. Sau khi xong Phase 0

Sang [Phase 1](./TASKS.md#phase-1--xác-thực-phân-quyền-nhân-sự): xác thực JWT, phân quyền RBAC, CRUD nhân viên và phòng ban.

Module `system` ở Phase 0 là bản mẫu. Module `employee` của Phase 1 sao chép đúng cấu trúc đó, chỉ thay nội dung:

```
domain/employee/     entity.go (Employee, Department)  ·  port.go (EmployeeRepository)
usecase/employee/    create.go · update.go · list.go · deactivate.go
repository/postgres/  employee.go (hiện thực EmployeeRepository)
delivery/http/handler/ employee.go
```

Khi đó bạn sẽ gặp câu hỏi thiết kế thật đầu tiên: **`usecase/project` cần kiểm tra nhân viên có tồn tại không — nó gọi ai?** Câu trả lời đúng là khai báo một interface nhỏ ngay trong package `project` (chỉ có đúng method nó cần), rồi cho `usecase/employee` đáp ứng interface đó. Không import ngược, không gọi thẳng repository của module khác.

Giữ được kỷ luật đó thì monolith này sẽ sạch sẽ rất lâu.
