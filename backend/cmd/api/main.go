// Binary api: HTTP REST (và WebSocket từ Phase 5).
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

	// Nhúng dữ liệu múi giờ vào binary.
	//
	// Không có nó, image production (không cài gói tzdata) sẽ không hiểu
	// "Asia/Ho_Chi_Minh" và time.LoadLocation trả lỗi — mọi tính toán giờ
	// giấc sẽ âm thầm chạy theo UTC.
	_ "time/tzdata"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/handler"
	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/router"
	repopg "github.com/PhamVanPhuc2k2/manage/internal/repository/postgres"
	repomq "github.com/PhamVanPhuc2k2/manage/internal/repository/rabbitmq"
	reporedis "github.com/PhamVanPhuc2k2/manage/internal/repository/redis"
	ucauth "github.com/PhamVanPhuc2k2/manage/internal/usecase/auth"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	ucsystem "github.com/PhamVanPhuc2k2/manage/internal/usecase/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
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

	jwtMgr, err := jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, "manage")
	if err != nil {
		return err
	}

	// =====================================================================
	// COMPOSITION ROOT — nơi DUY NHẤT biết cả interface lẫn bản hiện thực.
	//
	// Mọi tầng khác chỉ biết interface. Muốn đổi PostgreSQL sang thứ khác
	// thì sửa đúng ở đây, không đụng vào usecase.
	// =====================================================================

	// --- Repository ---
	clockRepo := repopg.NewClockRepository(db)
	cacheRepo := reporedis.NewCacheRepository(rdb)
	publisher := repomq.NewJobPublisher(mqClient)

	userRepo := repopg.NewUserRepository(db)
	authReader := repopg.NewAuthorizationReader(db)
	companyRepo := repopg.NewCompanyRepository(db)
	deptRepo := repopg.NewDepartmentRepository(db)
	posRepo := repopg.NewPositionRepository(db)
	empRepo := repopg.NewEmployeeRepository(db)
	roleRepo := repopg.NewRoleRepository(db)

	sessionStore := reporedis.NewSessionStore(rdb)
	refreshStore := reporedis.NewRefreshStore(rdb)
	throttle := reporedis.NewLoginThrottle(rdb)
	resetStore := reporedis.NewPasswordResetStore(rdb)
	mailer := repomq.NewMailer(mqClient)

	// --- Usecase ---
	pingUC := ucsystem.NewPingUsecase(clockRepo, cacheRepo, publisher)

	authUC := ucauth.NewUsecase(
		userRepo, authReader, sessionStore, refreshStore, throttle, resetStore,
		jwtMgr, mailer,
		ucauth.Config{
			AccessTTL:     cfg.JWTAccessTTL,
			RefreshTTL:    cfg.JWTRefreshTTL,
			PublicBaseURL: cfg.PublicBaseURL,
		},
	)

	hrUC := uchr.NewUsecase(companyRepo, deptRepo, posRepo, empRepo, userRepo, roleRepo)

	// Nối hr với auth: vô hiệu hoá nhân viên phải cắt luôn phiên đăng nhập
	// của họ, nếu không họ vẫn dùng được hệ thống tới khi token hết hạn.
	//
	// Dùng callback thay vì import trực tiếp để tránh phụ thuộc vòng giữa
	// hai module.
	hrUC.SetOnEmployeeDeactivated(func(ctx context.Context, userID uuid.UUID) {
		if err := authUC.LogoutAll(ctx, userID); err != nil {
			log.Error().Err(err).Str("user_id", userID.String()).
				Msg("không cắt được phiên của nhân viên vừa bị vô hiệu hoá")
		}
	})

	// --- Handler ---
	// Cookie Secure chỉ bật khi chạy HTTPS. Bật ở môi trường dev HTTP sẽ
	// khiến trình duyệt vứt cookie đi và refresh không bao giờ hoạt động.
	secureCookie := cfg.IsProduction()

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router.New(router.Deps{
			Config:     cfg,
			Logger:     log,
			Version:    version,
			Postgres:   db,
			Redis:      rdb,
			RabbitMQ:   mqClient,
			JWT:        jwtMgr,
			Sessions:   sessionStore,
			AuthReader: authReader,
			PingUC:     pingUC,
			Auth:       handler.NewAuthHandler(authUC, secureCookie),
			Employee:   handler.NewEmployeeHandler(hrUC),
			Department: handler.NewDepartmentHandler(hrUC),
			Position:   handler.NewPositionHandler(hrUC),
		}),
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

		// Dùng context MỚI, không dùng ctx đã bị huỷ — nếu không thì
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
