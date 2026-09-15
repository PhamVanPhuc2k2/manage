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

// WithContext nhét logger vào context để tầng dưới lấy ra dùng.
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
