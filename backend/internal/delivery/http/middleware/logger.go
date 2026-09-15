// Package middleware chứa middleware HTTP riêng của ứng dụng.
package middleware

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/yourorg/manage/pkg/logger"
)

// RequestLogger ghi log mỗi request và nhét logger đã gắn request_id
// vào context.
//
// Nhờ middleware này, mọi tầng bên dưới chỉ cần gọi logger.FromContext(ctx)
// là có sẵn request_id — không phải truyền tay qua từng hàm.
func RequestLogger(base zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := chimw.GetReqID(r.Context())

			l := base.With().
				Str("request_id", requestID).
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Logger()

			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			// Trả request_id về client để người dùng báo lỗi kèm mã này.
			w.Header().Set("X-Request-Id", requestID)

			next.ServeHTTP(ww, r.WithContext(logger.WithContext(r.Context(), l)))

			event := l.Info()
			if ww.Status() >= 500 {
				event = l.Error()
			} else if ww.Status() >= 400 {
				event = l.Warn()
			}

			event.
				Int("status", ww.Status()).
				Int("bytes", ww.BytesWritten()).
				Dur("duration", time.Since(start)).
				Str("remote_ip", r.RemoteAddr).
				Msg("HTTP request")
		})
	}
}
