// Binary worker: xử lý job nền nhận từ RabbitMQ.
package main

import (
	"context"
	"fmt"
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

	goredis "github.com/redis/go-redis/v9"

	"github.com/PhamVanPhuc2k2/manage/internal/delivery/consumer"
	"github.com/PhamVanPhuc2k2/manage/internal/delivery/scheduler"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	repopg "github.com/PhamVanPhuc2k2/manage/internal/repository/postgres"
	repomq "github.com/PhamVanPhuc2k2/manage/internal/repository/rabbitmq"
	reporedis "github.com/PhamVanPhuc2k2/manage/internal/repository/redis"
	ucatt "github.com/PhamVanPhuc2k2/manage/internal/usecase/attendance"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	ucpay "github.com/PhamVanPhuc2k2/manage/internal/usecase/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
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

	// Worker cần Redis từ Phase 3: nguồn dữ liệu chấm công là bảng presence
	// mà api ghi vào Redis mỗi khi nhận heartbeat.
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer func() { _ = rdb.Close() }()

	dispatcher := consumer.NewDispatcher(mqClient, log)

	mailSender := consumer.NewMailSender(
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser, cfg.SMTPPass)

	// Máy tính lương chạy Ở ĐÂY, không ở api: kỳ lương của công ty vài trăm
	// người mất vài chục giây, quá lâu cho một request HTTP.
	payrollConsumer := consumer.NewPayrollConsumer(buildPayrollUsecase(db, mqClient))

	// Đăng ký handler cho từng loại job.
	// Phase sau chỉ cần thêm dòng vào đây.
	jobs := map[string]consumer.HandlerFunc{
		domainsystem.JobSystemPing:       consumer.HandleSystemPing,
		repomq.JobSendPasswordReset:      mailSender.HandlePasswordReset,
		repomq.JobSendSuspiciousActivity: mailSender.HandleSuspiciousActivity,
		repomq.JobSendWelcome:            mailSender.HandleWelcome,
		repomq.JobSendLoginOTP:           mailSender.HandleLoginOTP,
		repomq.JobSendPayslip:            mailSender.HandlePayslip,

		// Tính lương. Job này chịu được chạy lại: ReplaceForPeriod xoá sạch
		// phiếu cũ rồi ghi bộ mới trong một giao dịch.
		repomq.JobCalculatePayroll: payrollConsumer.HandleCalculate,

		// Sự kiện module dự án. Phase 2 mới ghi log; Phase 5 sẽ sinh thông
		// báo thật từ chính các message này. Đăng ký ngay để sự kiện không
		// rơi vào dead-letter queue vì thiếu handler.
		domainproject.JobTaskAssigned:      consumer.HandleProjectEvent,
		domainproject.JobTaskStatusChanged: consumer.HandleProjectEvent,
		domainproject.JobTaskMentioned:     consumer.HandleProjectEvent,
		domainproject.JobTaskDueSoon:       consumer.HandleProjectEvent,
	}
	for name, fn := range jobs {
		if err := dispatcher.Register(name, fn); err != nil {
			return fmt.Errorf("đăng ký job %s: %w", name, err)
		}
	}
	log.Info().Int("jobs", len(jobs)).Msg("đã đăng ký handler")

	// =====================================================================
	// CÔNG VIỆC ĐỊNH KỲ — chấm công
	//
	// Chạy ở worker chứ không ở api, có chủ ý: api có thể chạy nhiều replica
	// và khi đó mỗi replica sẽ chạy job một lần, tạo ra dữ liệu trùng. Worker
	// cũng scale được, nhưng cả hai job đều viết sao cho chạy lại vô hại —
	// xem ghi chú ở CollectPresence.
	// =====================================================================
	attendanceUC := buildAttendanceUsecase(db, rdb)

	loc, err := attendanceUC.Timezone(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("không đọc được múi giờ công ty, dùng UTC")
		loc = time.UTC
	}

	sched := scheduler.New(log, loc)

	sched.Add(scheduler.Job{
		Name:       "attendance.collect_presence",
		Every:      ucatt.CollectInterval,
		RunAtStart: true,
		Run: func(ctx context.Context) error {
			_, err := attendanceUC.CollectPresence(ctx)
			return err
		},
	})

	// Tổng hợp lúc 00:30 cho NGÀY HÔM TRƯỚC.
	//
	// Nửa đêm đúng thì phiên của những người còn đang làm chưa đóng; chờ
	// thêm nửa tiếng để mọi presence của ngày cũ đã hết hạn và dữ liệu đứng
	// yên. Tổng hợp lúc 00:00 sẽ cắt mất phần cuối ca của người làm khuya.
	sched.AddDaily(scheduler.DailyJob{
		Name:     "attendance.rollup_yesterday",
		AtHour:   0,
		AtMinute: 30,
		Run: func(ctx context.Context) error {
			yesterday := time.Now().In(loc).AddDate(0, 0, -1)
			_, err := attendanceUC.RollupDay(ctx, yesterday)
			return err
		},
	})

	// Tổng hợp lại NGÀY HÔM NAY mỗi 15 phút.
	//
	// Nhờ nó, bảng công của hôm nay luôn gần đúng thay vì trống rỗng tới tận
	// nửa đêm — đó là màn hình người dùng mở nhiều nhất.
	sched.AddDaily(scheduler.DailyJob{
		Name:     "attendance.rollup_today_noon",
		AtHour:   12,
		AtMinute: 5,
		Run: func(ctx context.Context) error {
			_, err := attendanceUC.RollupDay(ctx, time.Now().In(loc))
			return err
		},
	})

	sched.Start(ctx)

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

		// Chờ các job định kỳ đang chạy dở kết thúc. Cắt ngang job tổng hợp
		// bảng công giữa chừng sẽ để lại một ngày tổng hợp một nửa.
		sched.Wait()

		log.Info().Msg("worker đã dừng")
		return nil
	}
}

// buildAttendanceUsecase lắp ráp module chấm công cho worker.
//
// Tách ra hàm riêng vì worker chỉ cần ĐÚNG module này, không cần toàn bộ
// composition root của api. Danh sách phụ thuộc dài là dấu hiệu module có
// nhiều việc — và nó đúng là vậy: chấm công đọc presence, phiên, bảng công,
// đơn nghỉ, quỹ phép, khung giờ và ngày lễ.
func buildAttendanceUsecase(db *postgres.DB, rdb *goredis.Client) *ucatt.Usecase {
	companyRepo := repopg.NewCompanyRepository(db)
	deptRepo := repopg.NewDepartmentRepository(db)
	posRepo := repopg.NewPositionRepository(db)
	empRepo := repopg.NewEmployeeRepository(db)
	userRepo := repopg.NewUserRepository(db)
	roleRepo := repopg.NewRoleRepository(db)

	// hrUC ở đây chỉ dùng làm CompanyLookup (id công ty + múi giờ). Truyền
	// nil cho storage vì worker không đụng tới tệp.
	hrUC := uchr.NewUsecase(companyRepo, deptRepo, posRepo, empRepo, userRepo, roleRepo, nil)

	return ucatt.NewUsecase(
		repopg.NewScheduleRepository(db),
		repopg.NewHolidayRepository(db),
		repopg.NewAttendanceSessionRepository(db),
		repopg.NewAttendanceDayRepository(db),
		repopg.NewAdjustmentRepository(db),
		repopg.NewLeaveRepository(db),
		repopg.NewBalanceRepository(db),
		repopg.NewEmployeeLookup(db),
		reporedis.NewPresenceStore(rdb),
		hrUC,
	)
}

// buildPayrollUsecase lắp ráp module lương cho worker.
//
// Truyền nil cho JobPublisher: worker KHÔNG đẩy việc cho chính mình. Nếu
// truyền vào, một job tính lương lỗi có thể tự đẩy lại và tạo vòng lặp.
func buildPayrollUsecase(db *postgres.DB, mqClient *rabbitmq.Client) *ucpay.Usecase {
	companyRepo := repopg.NewCompanyRepository(db)
	deptRepo := repopg.NewDepartmentRepository(db)
	posRepo := repopg.NewPositionRepository(db)
	empRepo := repopg.NewEmployeeRepository(db)
	userRepo := repopg.NewUserRepository(db)
	roleRepo := repopg.NewRoleRepository(db)

	hrUC := uchr.NewUsecase(companyRepo, deptRepo, posRepo, empRepo, userRepo, roleRepo, nil)

	return ucpay.NewUsecase(
		repopg.NewPayrollSettingsRepository(db),
		repopg.NewSalaryStructureRepository(db),
		repopg.NewPayrollPeriodRepository(db),
		repopg.NewPayslipRepository(db),
		repopg.NewAuditRepository(db),
		repopg.NewAttendanceLookup(db),
		repopg.NewEmployeeLookup(db),
		hrUC,
		nil,
		repomq.NewMailer(mqClient),
	)
}
