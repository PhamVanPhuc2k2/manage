// Package scheduler chạy các công việc định kỳ trong worker.
//
// Đây là tầng delivery thứ ba của hệ thống, bên cạnh http và consumer:
// nguồn kích hoạt là ĐỒNG HỒ thay vì request hay message. Nó cũng chỉ làm
// một việc — gọi xuống usecase — và không chứa logic nghiệp vụ.
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
)

// Job là một việc chạy định kỳ.
type Job struct {
	Name string
	// Every là chu kỳ lặp.
	Every time.Duration
	// RunAtStart cho biết có chạy ngay lúc khởi động hay đợi hết chu kỳ đầu.
	//
	// Công việc quét presence nên chạy ngay: worker vừa restart thì đã có
	// người đang online, và đợi thêm một phút là mất một phút dữ liệu.
	RunAtStart bool
	Run        func(ctx context.Context) error
}

// DailyJob là việc chạy một lần mỗi ngày vào một giờ cố định.
type DailyJob struct {
	Name string
	// AtHour, AtMinute theo MÚI GIỜ được truyền vào, không phải giờ máy chủ.
	AtHour   int
	AtMinute int
	Run      func(ctx context.Context) error
}

type Scheduler struct {
	log   zerolog.Logger
	loc   *time.Location
	jobs  []Job
	daily []DailyJob
	wg    sync.WaitGroup
}

func New(log zerolog.Logger, loc *time.Location) *Scheduler {
	if loc == nil {
		loc = time.UTC
	}
	return &Scheduler{log: log.With().Str("component", "scheduler").Logger(), loc: loc}
}

func (s *Scheduler) Add(j Job)           { s.jobs = append(s.jobs, j) }
func (s *Scheduler) AddDaily(j DailyJob) { s.daily = append(s.daily, j) }

// Start chạy mọi job tới khi ctx bị huỷ.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		s.wg.Add(1)
		go s.runInterval(ctx, j)
	}
	for _, j := range s.daily {
		s.wg.Add(1)
		go s.runDaily(ctx, j)
	}
	s.log.Info().
		Int("interval_jobs", len(s.jobs)).
		Int("daily_jobs", len(s.daily)).
		Str("timezone", s.loc.String()).
		Msg("đã khởi động bộ lập lịch")
}

// Wait chờ mọi job đang chạy kết thúc. Gọi lúc tắt êm.
func (s *Scheduler) Wait() { s.wg.Wait() }

func (s *Scheduler) runInterval(ctx context.Context, j Job) {
	defer s.wg.Done()

	ticker := time.NewTicker(j.Every)
	defer ticker.Stop()

	if j.RunAtStart {
		s.exec(ctx, j.Name, j.Run)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.exec(ctx, j.Name, j.Run)
		}
	}
}

func (s *Scheduler) runDaily(ctx context.Context, j DailyJob) {
	defer s.wg.Done()

	for {
		wait := s.untilNext(j.AtHour, j.AtMinute)

		// Dùng Timer chứ không Ticker 24 giờ.
		//
		// Ticker sẽ trôi dần khỏi giờ đã hẹn: nó đếm từ lúc tick trước, nên
		// mỗi lần worker restart là mốc giờ lệch đi. Tính lại khoảng chờ sau
		// mỗi lần chạy giữ cho job luôn nổ đúng giờ đã hẹn — và xử lý đúng
		// cả ngày đổi giờ mùa hè, nơi một "ngày" không phải 24 giờ.
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.exec(ctx, j.Name, j.Run)
		}
	}
}

// untilNext tính khoảng chờ tới lần kế tiếp của một mốc giờ trong ngày.
func (s *Scheduler) untilNext(hour, minute int) time.Duration {
	now := time.Now().In(s.loc)
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, s.loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(now)
}

// exec chạy một job, bắt panic và ghi log.
//
// Bắt panic là bắt buộc: job chạy trong goroutine riêng, và một panic không
// được bắt sẽ giết CẢ TIẾN TRÌNH worker chứ không riêng job đó. Với tầng
// http đã có middleware Recoverer làm việc này; ở đây phải tự lo.
func (s *Scheduler) exec(ctx context.Context, name string, run func(context.Context) error) {
	defer func() {
		if rec := recover(); rec != nil {
			s.log.Error().
				Interface("panic", rec).
				Str("job", name).
				Msg("job định kỳ panic, đã chặn lại")
			metrics.ScheduledJobRuns.WithLabelValues(name, "panic").Inc()
		}
	}()

	start := time.Now()

	jobLog := s.log.With().Str("job", name).Logger()
	jobCtx, cancel := context.WithTimeout(logger.WithContext(ctx, jobLog), 10*time.Minute)
	defer cancel()

	if err := run(jobCtx); err != nil {
		s.log.Error().Err(err).Str("job", name).Msg("job định kỳ thất bại")
		metrics.ScheduledJobRuns.WithLabelValues(name, "error").Inc()
		return
	}

	// Ghi mốc thành công, KHÔNG phải mốc "đã chạy".
	//
	// Một job chạy đúng giờ nhưng lỗi mỗi lần thì mốc "đã chạy" vẫn mới tinh
	// và không cảnh báo gì. Cảnh báo dựa trên mốc thành công bắt được cả hai:
	// job chết hẳn và job chạy mà luôn lỗi.
	metrics.ScheduledJobRuns.WithLabelValues(name, "ok").Inc()
	metrics.ScheduledJobLastSuccess.WithLabelValues(name).Set(float64(time.Now().Unix()))

	s.log.Debug().
		Str("job", name).
		Dur("duration", time.Since(start)).
		Msg("job định kỳ xong")
}
