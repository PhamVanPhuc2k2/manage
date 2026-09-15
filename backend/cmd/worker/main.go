// Binary worker: xử lý job nền nhận từ RabbitMQ.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yourorg/manage/internal/delivery/consumer"
	domainsystem "github.com/yourorg/manage/internal/domain/system"
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
	if err := dispatcher.Register(domainsystem.JobSystemPing, consumer.HandleSystemPing); err != nil {
		return fmt.Errorf("đăng ký job %s: %w", domainsystem.JobSystemPing, err)
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
