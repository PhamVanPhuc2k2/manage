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

// HealthCheck dùng cho endpoint /ready.
func (db *DB) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return db.Ping(ctx)
}
