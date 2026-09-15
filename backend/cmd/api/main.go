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

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router.New(router.Deps{
			Config:   cfg,
			Logger:   log,
			Version:  version,
			PingUC:   pingUC,
			Postgres: db,
			Redis:    rdb,
			RabbitMQ: mqClient,
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
