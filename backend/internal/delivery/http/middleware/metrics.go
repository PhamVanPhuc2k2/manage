package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
)

// Metrics ghi số lượng và độ trễ request vào Prometheus.
//
// Đặt SAU chi router trong chuỗi middleware là không được: nhãn route lấy từ
// chi.RouteContext, và context đó chỉ có sau khi router đã khớp đường dẫn.
// Vì vậy middleware này đọc route ở thời điểm SAU khi next.ServeHTTP trả về —
// lúc đó chi đã điền xong RoutePattern.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := routeLabel(r)
		status := strconv.Itoa(ww.Status())

		metrics.HTTPRequests.WithLabelValues(r.Method, route, status).Inc()
		metrics.HTTPDuration.WithLabelValues(r.Method, route).
			Observe(time.Since(start).Seconds())
	})
}

// routeLabel trả về MẪU đường dẫn, không phải đường dẫn thật.
//
// Đây là chi tiết quan trọng nhất của tệp này. Dùng r.URL.Path sẽ sinh một
// chuỗi thời gian riêng cho mỗi uuid xuất hiện trong URL — với một hệ thống
// có /employees/{id} thì đó là một chuỗi mới cho mỗi nhân viên, mỗi ngày.
// Prometheus sẽ hết bộ nhớ, và cái hỏng không phải dashboard mà là cả tiến
// trình giám sát.
//
// Request không khớp route nào (404) gom về nhãn "unmatched", cũng vì lý do
// đó: kẻ quét tự động bắn hàng nghìn đường dẫn lạ và mỗi cái sẽ là một nhãn.
func routeLabel(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}
