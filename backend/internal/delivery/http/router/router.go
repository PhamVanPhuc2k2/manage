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

	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/handler"
	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

type Deps struct {
	Config   *config.Config
	Logger   zerolog.Logger
	Version  string
	Postgres *postgres.DB
	Redis    *goredis.Client
	RabbitMQ *rabbitmq.Client

	JWT        *jwt.Manager
	Sessions   domainauth.SessionStore
	AuthReader domainauth.AuthorizationReader

	PingUC     handler.PingUsecase
	Auth       *handler.AuthHandler
	Employee   *handler.EmployeeHandler
	Department *handler.DepartmentHandler
	Position   *handler.PositionHandler
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
		AllowCredentials: true, // bắt buộc để trình duyệt gửi cookie refresh
		MaxAge:           300,
	}))

	systemHandler := handler.NewSystemHandler(d.PingUC, d.Version)
	requireAuth := appmw.RequireAuth(d.JWT, d.Sessions, d.AuthReader)

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

		// =================================================================
		// CÔNG KHAI — không cần đăng nhập
		// =================================================================
		r.Group(func(r chi.Router) {
			r.Post("/auth/login", d.Auth.Login)
			r.Post("/auth/refresh", d.Auth.Refresh)
			r.Post("/auth/forgot-password", d.Auth.ForgotPassword)
			r.Post("/auth/reset-password", d.Auth.ResetPassword)
		})

		// =================================================================
		// CẦN ĐĂNG NHẬP
		// =================================================================
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)

			r.Get("/auth/me", d.Auth.Me)
			r.Post("/auth/logout", d.Auth.Logout)
			r.Post("/auth/logout-all", d.Auth.LogoutAll)
			r.Get("/auth/sessions", d.Auth.ListSessions)
			r.Post("/auth/change-password", d.Auth.ChangePassword)

			// --- Nhân viên ---
			r.Route("/employees", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermEmployeeRead)).
					Get("/", d.Employee.List)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeRead)).
					Get("/{id}", d.Employee.Get)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeCreate)).
					Post("/", d.Employee.Create)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeUpdate)).
					Put("/{id}", d.Employee.Update)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeDelete)).
					Delete("/{id}", d.Employee.Deactivate)
			})

			// --- Phòng ban ---
			r.Route("/departments", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/", d.Department.List)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/tree", d.Department.Tree)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/{id}", d.Department.Get)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentCreate)).
					Post("/", d.Department.Create)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentUpdate)).
					Put("/{id}", d.Department.Update)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentDelete)).
					Delete("/{id}", d.Department.Delete)
			})

			// --- Chức vụ ---
			r.Route("/positions", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermPositionRead)).
					Get("/", d.Position.List)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Post("/", d.Position.Create)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Put("/{id}", d.Position.Update)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Delete("/{id}", d.Position.Delete)
			})
		})
	})

	return r
}
