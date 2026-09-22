// Binary wsload: kiểm tra tải cho WebSocket.
//
// Mở N kết nối đồng thời, gửi nhịp tim như client thật, rồi báo cáo tỷ lệ kết
// nối thành công, độ trễ bắt tay và số bản tin nhận được.
//
// Chạy:
//
//	go run ./cmd/wsload -url ws://localhost:8080/ws -token <access_token> -n 500
//
// Vì sao là một binary riêng chứ không phải một hàm test Go:
//
//   - Nó cần chạy được với hệ thống ĐANG CHẠY THẬT, không phải một bản dựng
//     trong bộ nhớ. Thứ cần đo là hub, Redis, RabbitMQ và cả nginx cùng lúc.
//   - Nó phải chạy được từ MỘT MÁY KHÁC. 500 kết nối từ chính máy chủ sẽ đo
//     luôn cả tải của công cụ đo.
//   - `go test` có timeout mặc định 10 phút và không có tham số dòng lệnh
//     thuận tiện cho việc điều chỉnh tải.
//
// Lưu ý về giới hạn của HỆ ĐIỀU HÀNH: mỗi kết nối là một file descriptor, và
// ulimit mặc định trên nhiều bản Linux là 1024. Kiểm tra bằng `ulimit -n`
// trước khi kết luận rằng máy chủ không chịu nổi tải — rất dễ đo nhầm giới hạn
// của chính máy chạy công cụ này.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type stats struct {
	connected   atomic.Int64
	failed      atomic.Int64
	disconnects atomic.Int64
	received    atomic.Int64
	sent        atomic.Int64

	mu        sync.Mutex
	handshake []time.Duration
	errors    map[string]int
}

func newStats() *stats {
	return &stats{errors: map[string]int{}}
}

func (s *stats) recordHandshake(d time.Duration) {
	s.mu.Lock()
	s.handshake = append(s.handshake, d)
	s.mu.Unlock()
}

func (s *stats) recordError(err error) {
	s.mu.Lock()
	// Gom theo NỘI DUNG lỗi, không in từng cái: 500 kết nối hỏng vì cùng một
	// lý do sẽ thành 500 dòng giống hệt nhau và che mất lỗi thật sự khác.
	s.errors[err.Error()]++
	s.mu.Unlock()
}

// percentile trả về phân vị của danh sách độ trễ bắt tay.
func (s *stats) percentile(p float64) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.handshake) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(s.handshake))
	copy(sorted, s.handshake)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func main() {
	var (
		url       = flag.String("url", "ws://localhost:8080/ws", "địa chỉ WebSocket")
		token     = flag.String("token", "", "access token (bắt buộc)")
		n         = flag.Int("n", 500, "số kết nối đồng thời")
		rampUp    = flag.Duration("ramp", 10*time.Second, "thời gian mở dần hết số kết nối")
		duration  = flag.Duration("duration", 60*time.Second, "thời gian giữ kết nối")
		heartbeat = flag.Duration("heartbeat", 30*time.Second, "chu kỳ nhịp tim")
	)
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "thiếu -token. Lấy bằng cách đăng nhập rồi copy access_token.")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx, cancel := context.WithTimeout(ctx, *rampUp+*duration+30*time.Second)
	defer cancel()

	st := newStats()
	target := *url + "?token=" + *token

	fmt.Printf("Mở %d kết nối tới %s trong %v, giữ %v\n\n",
		*n, *url, *rampUp, *duration)

	// Mở DẦN chứ không mở một lúc.
	//
	// Mở 500 kết nối trong cùng một mili giây không giống bất kỳ tải thật nào,
	// và nó đo chủ yếu khả năng chịu đột biến của accept queue chứ không phải
	// khả năng phục vụ. Mở dần trong 10 giây gần với việc người dùng mở máy
	// buổi sáng hơn.
	gap := time.Duration(0)
	if *n > 0 {
		gap = *rampUp / time.Duration(*n)
	}

	var wg sync.WaitGroup
	stopHolding := time.Now().Add(*rampUp + *duration)

	for i := 0; i < *n; i++ {
		select {
		case <-ctx.Done():
			goto done
		case <-time.After(gap):
		}

		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			runClient(ctx, target, st, stopHolding, *heartbeat)
		}(i)

		if c := st.connected.Load(); c > 0 && c%100 == 0 {
			fmt.Printf("  ... %d kết nối đang mở\n", c)
		}
	}

done:
	wg.Wait()
	report(st, *n)
}

// runClient mở một kết nối, gửi nhịp tim và đọc tới khi hết giờ.
func runClient(
	ctx context.Context,
	target string,
	st *stats,
	until time.Time,
	heartbeat time.Duration,
) {
	start := time.Now()

	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, target, nil)
	if err != nil {
		st.failed.Add(1)
		st.recordError(err)
		return
	}
	defer func() { _ = conn.Close() }()

	st.recordHandshake(time.Since(start))
	st.connected.Add(1)
	defer st.connected.Add(-1)

	// Đọc trong goroutine riêng: không đọc thì hàng đợi gửi phía server đầy
	// lên và nó đóng kết nối — ta sẽ đo nhầm thành "server không chịu nổi".
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
			st.received.Add(1)
		}
	}()

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			// Server đóng kết nối trước khi hết giờ.
			if time.Now().Before(until) {
				st.disconnects.Add(1)
			}
			return
		case <-ticker.C:
			if time.Now().After(until) {
				return
			}
			msg, _ := json.Marshal(map[string]any{
				"type":    "heartbeat",
				"payload": map[string]bool{"is_active": true},
				"ts":      time.Now().Format(time.RFC3339),
			})
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				st.recordError(err)
				return
			}
			st.sent.Add(1)
		}
	}
}

func report(st *stats, target int) {
	failed := st.failed.Load()
	ok := int64(target) - failed

	fmt.Println()
	fmt.Println("══════════════════════════════════════")
	fmt.Printf("  Kết nối thành công : %d / %d\n", ok, target)
	fmt.Printf("  Thất bại           : %d\n", failed)
	fmt.Printf("  Bị đóng giữa chừng : %d\n", st.disconnects.Load())
	fmt.Printf("  Nhịp tim đã gửi    : %d\n", st.sent.Load())
	fmt.Printf("  Bản tin nhận được  : %d\n", st.received.Load())
	fmt.Println("  ────────────────────────────────────")
	fmt.Printf("  Bắt tay p50        : %v\n", st.percentile(0.50))
	fmt.Printf("  Bắt tay p95        : %v\n", st.percentile(0.95))
	fmt.Printf("  Bắt tay p99        : %v\n", st.percentile(0.99))

	st.mu.Lock()
	if len(st.errors) > 0 {
		fmt.Println("  ────────────────────────────────────")
		fmt.Println("  Lỗi:")
		for msg, count := range st.errors {
			fmt.Printf("    %4d × %s\n", count, msg)
		}
	}
	st.mu.Unlock()
	fmt.Println("══════════════════════════════════════")

	// Mã thoát khác 0 để chạy được trong CI hoặc script nghiệm thu.
	//
	// Ngưỡng 99%: một vài kết nối hỏng vì mạng là bình thường, nhưng dưới mức
	// đó thì có gì đó thật sự sai.
	if failed*100 > int64(target) {
		fmt.Fprintf(os.Stderr, "\nTHẤT BẠI: %d/%d kết nối hỏng (trên 1%%)\n", failed, target)
		os.Exit(1)
	}
	if st.disconnects.Load()*100 > int64(target) {
		fmt.Fprintf(os.Stderr, "\nTHẤT BẠI: %d kết nối bị đóng giữa chừng\n",
			st.disconnects.Load())
		os.Exit(1)
	}
}
