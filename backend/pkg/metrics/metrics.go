// Package metrics khai báo các chỉ số Prometheus của hệ thống.
//
// Tập trung ở MỘT chỗ chứ không rải theo từng module, có chủ ý: tên chỉ số và
// nhãn là một giao diện công khai — dashboard và cảnh báo phụ thuộc vào chúng.
// Đổi tên một chỉ số nằm rải rác trong code thì dashboard hỏng lặng lẽ; tập
// trung lại thì mọi tên nằm trong một tệp đọc được hết trong một lần.
//
// Nguyên tắc chọn chỉ số: chỉ đo thứ sẽ dùng để TRẢ LỜI một câu hỏi vận hành
// cụ thể. Mỗi chỉ số thêm vào là bộ nhớ thường trú và một cột trong mọi lần
// scrape, nên "đo cho có" là một cái giá trả mãi.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

// Registry riêng thay vì prometheus.DefaultRegisterer.
//
// Registry mặc định là biến toàn cục dùng chung với mọi thư viện được import;
// một thư viện nào đó đăng ký trùng tên sẽ làm panic lúc khởi động. Registry
// riêng cũng khiến test dựng được một bộ chỉ số sạch.
var Registry = prometheus.NewRegistry()

// =========================================================================
// HTTP
// =========================================================================

var (
	// HTTPRequests đếm request theo route, method và mã trạng thái.
	//
	// Nhãn là ROUTE (mẫu đường dẫn của chi), không phải đường dẫn thật. Dùng
	// đường dẫn thật sẽ sinh một chuỗi thời gian riêng cho mỗi uuid trong URL
	// — đó là lỗ hổng bùng nổ nhãn kinh điển, và nó làm sập Prometheus chứ
	// không chỉ làm dashboard rối.
	HTTPRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_http_requests_total",
			Help: "Tổng số HTTP request đã xử lý.",
		},
		[]string{"method", "route", "status"},
	)

	// HTTPDuration là độ trễ request.
	//
	// Histogram chứ không Summary: histogram cho phép tính phân vị GỘP từ
	// nhiều instance api, còn summary thì không — phân vị của summary chỉ
	// đúng cho một tiến trình, và cộng chúng lại là vô nghĩa.
	HTTPDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "manage_http_request_duration_seconds",
			Help: "Độ trễ HTTP request, tính bằng giây.",
			// Mốc chia dày ở khoảng 10–500ms vì đó là vùng request bình
			// thường của hệ thống này; mốc 5s và 10s để thấy đuôi chậm.
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "route"},
	)
)

// =========================================================================
// WEBSOCKET
// =========================================================================

var (
	// WSConnections là số kết nối đang mở trên instance NÀY.
	//
	// Gauge, không Counter: đây là một con số lên xuống. Cảnh báo "số kết nối
	// tụt đột ngột" đọc chính chỉ số này.
	WSConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "manage_ws_connections",
		Help: "Số kết nối WebSocket đang mở trên instance này.",
	})

	// WSOnlineEmployees là số NGƯỜI đang online trên instance này.
	//
	// Khác WSConnections vì một người có thể mở nhiều thiết bị. Hai con số
	// lệch nhau nhiều là dấu hiệu client mở kết nối trùng mà không đóng —
	// một lỗi chỉ thấy được khi đo cả hai.
	WSOnlineEmployees = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "manage_ws_online_employees",
		Help: "Số nhân viên đang có ít nhất một kết nối trên instance này.",
	})

	// WSMessages đếm bản tin theo chiều và loại.
	WSMessages = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_ws_messages_total",
			Help: "Tổng số bản tin WebSocket.",
		},
		[]string{"direction", "type"},
	)

	// WSDropped đếm lần phải đóng kết nối vì hàng đợi gửi đầy.
	//
	// Con số này lớn dần nghĩa là có client quá chậm hoặc bản tin quá nhiều —
	// và nó là thứ duy nhất cho biết điều đó, vì việc đóng kết nối chậm diễn
	// ra im lặng với mọi người khác.
	WSDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "manage_ws_dropped_total",
		Help: "Số lần đóng kết nối vì hàng đợi gửi đầy.",
	})
)

// =========================================================================
// HÀNG ĐỢI VÀ JOB
// =========================================================================

var (
	// JobsProcessed đếm job theo tên và kết quả.
	JobsProcessed = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_jobs_processed_total",
			Help: "Tổng số job nền đã xử lý.",
		},
		[]string{"job", "result"},
	)

	// JobDuration là thời gian xử lý một job.
	//
	// Mốc chia THƯA VÀ RỘNG hơn HTTP: job nền chạy hàng giây tới hàng phút
	// (tính lương cả công ty), và dùng chung mốc với HTTP thì mọi job đều rơi
	// vào ô cuối cùng, không đọc được gì.
	JobDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "manage_job_duration_seconds",
			Help:    "Thời gian xử lý job nền, tính bằng giây.",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300},
		},
		[]string{"job"},
	)

	// QueueDepth là số message đang chờ trong một hàng đợi RabbitMQ.
	//
	// Đây là chỉ số cảnh báo quan trọng nhất của worker: nó tăng đều nghĩa là
	// worker xử lý không kịp, và không có nó thì hàng đợi ứ đọng hàng giờ mà
	// không ai biết — job vẫn "đang chạy", chỉ là chậm hơn tốc độ nạp vào.
	QueueDepth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "manage_queue_depth",
			Help: "Số message đang chờ trong hàng đợi.",
		},
		[]string{"queue"},
	)
)

// =========================================================================
// JOB ĐỊNH KỲ
// =========================================================================

var (
	// ScheduledJobRuns đếm lần chạy của job định kỳ.
	ScheduledJobRuns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_scheduled_job_runs_total",
			Help: "Tổng số lần chạy job định kỳ.",
		},
		[]string{"job", "result"},
	)

	// ScheduledJobLastSuccess là thời điểm job định kỳ thành công gần nhất,
	// dạng Unix timestamp.
	//
	// Đo "lần cuối THÀNH CÔNG" chứ không phải "lần cuối chạy": một job chạy
	// đúng giờ nhưng lỗi mỗi lần thì chỉ số "lần cuối chạy" vẫn mới tinh và
	// không cảnh báo gì. Cảnh báo dựa trên chỉ số này bắt được cả hai trường
	// hợp — job chết hẳn và job chạy mà luôn lỗi.
	ScheduledJobLastSuccess = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "manage_scheduled_job_last_success_timestamp_seconds",
			Help: "Thời điểm job định kỳ thành công gần nhất (Unix timestamp).",
		},
		[]string{"job"},
	)
)

// =========================================================================
// THÔNG TIN BUILD
// =========================================================================

// BuildInfo là một gauge luôn bằng 1, mang thông tin build trong NHÃN.
//
// Mẫu quen thuộc của Prometheus: nó cho phép dashboard hiện phiên bản đang
// chạy và cho phép so sánh chỉ số giữa hai lần triển khai, mà không cần một
// cơ chế riêng nào.
// =========================================================================
// GỌI THOẠI / VIDEO
// =========================================================================

var (
	// CallsStarted đếm cuộc gọi mở ra, tách theo audio và video.
	//
	// Cặp với CallsEnded để trả lời câu "bao nhiêu cuộc gọi mỗi ngày" mà
	// không phải quét bảng calls — một câu hỏi vận hành hỏi hằng ngày thì
	// không nên là một câu SELECT trên database nghiệp vụ.
	CallsStarted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_calls_started_total",
			Help: "Số cuộc gọi đã mở, theo kiểu.",
		},
		[]string{"kind"},
	)

	// CallsEnded tách theo LÝ DO kết thúc, không chỉ theo trạng thái.
	//
	// Lý do mới là thứ đáng nhìn: tỉ lệ 'timeout' tăng nghĩa là người ta
	// gọi nhau mà không ai nghe; tỉ lệ 'network' tăng nghĩa là hạ tầng có
	// vấn đề. Gộp cả hai thành "cuộc gọi hỏng" sẽ giấu mất hai câu chuyện
	// hoàn toàn khác nhau.
	CallsEnded = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "manage_calls_ended_total",
			Help: "Số cuộc gọi đã kết thúc, theo trạng thái cuối và lý do.",
		},
		[]string{"status", "reason"},
	)

	// CallDuration chỉ ghi cuộc gọi ĐÃ CÓ NGƯỜI BẮT MÁY.
	//
	// Đưa cuộc gọi nhỡ (0 giây) vào cùng một histogram sẽ kéo trung vị
	// xuống gần 0 và làm con số mất hết ý nghĩa.
	CallDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "manage_call_duration_seconds",
			Help: "Thời lượng cuộc gọi đã có người bắt máy.",
			// Mốc chọn theo cách người ta thật sự gọi nhau: vài chục giây
			// để hỏi một câu, vài phút để bàn một việc, nửa tiếng trở lên
			// là một cuộc họp.
			Buckets: []float64{10, 30, 60, 300, 900, 1800, 3600},
		},
	)
)

var BuildInfo = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "manage_build_info",
		Help: "Thông tin build, luôn bằng 1; dữ liệu nằm ở nhãn.",
	},
	[]string{"version", "git_sha", "build_time", "app"},
)

func init() {
	Registry.MustRegister(
		HTTPRequests, HTTPDuration,
		WSConnections, WSOnlineEmployees, WSMessages, WSDropped,
		JobsProcessed, JobDuration, QueueDepth,
		ScheduledJobRuns, ScheduledJobLastSuccess,
		CallsStarted, CallsEnded, CallDuration,
		BuildInfo,

		// Chỉ số của chính tiến trình Go: bộ nhớ, goroutine, GC, số file
		// descriptor. Rẻ và là thứ đầu tiên phải xem khi tiến trình có vấn đề
		// — rò goroutine trong hub WebSocket chỉ nhìn thấy ở đây.
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// SetBuildInfo ghi nhận thông tin build. Gọi một lần lúc khởi động.
func SetBuildInfo(app, version, gitSHA, buildTime string) {
	BuildInfo.WithLabelValues(version, gitSHA, buildTime, app).Set(1)
}

// Handler trả về http.Handler phục vụ /metrics.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		// Lỗi lúc thu thập thì ghi log và trả về phần thu được, không trả 500:
		// mất một chỉ số không nên làm mất toàn bộ khả năng quan sát.
		ErrorHandling: promhttp.ContinueOnError,
	})
}
