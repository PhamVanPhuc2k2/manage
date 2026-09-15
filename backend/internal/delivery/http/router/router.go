// Package router lắp ráp toàn bộ route HTTP của api.
package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/yourorg/manage/internal/delivery/http/handler"
	appmw "github.com/yourorg/manage/internal/delivery/http/middleware"
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
	r.Use(chimw.RequestID)               // sinh request_id trước tiên
	r.Use(chimw.RealIP)                  // lấy IP thật phía sau nginx
	r.Use(appmw.RequestLogger(d.Logger)) // log + nhét logger vào context
	r.Use(chimw.Recoverer)               // bắt panic, không để sập cả tiến trình
	r.Use(chimw.Timeout(30 * time.Second))
	r.Use(chimw.Compress(5))

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
	//
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
