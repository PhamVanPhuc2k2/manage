// Binary worker: xử lý job nền nhận từ RabbitMQ.
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
	ucchat "github.com/PhamVanPhuc2k2/manage/internal/usecase/chat"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	ucnotif "github.com/PhamVanPhuc2k2/manage/internal/usecase/notification"
	ucpay "github.com/PhamVanPhuc2k2/manage/internal/usecase/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
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
	metrics.SetBuildInfo("worker", version, gitSHA, buildTime)
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

	// Máy chủ HTTP CHỈ để phục vụ /metrics và /health.
	//
	// Worker không nhận request nghiệp vụ nào, nhưng Prometheus cần scrape nó:
	// độ sâu hàng đợi, thời gian xử lý job và mốc thành công của job định kỳ
	// đều chỉ có ở đây. Không có cổng này thì đúng những chỉ số quan trọng
	// nhất của phần nền lại là phần không quan sát được.
	//
	// Cổng KHÔNG mở ra host (xem docker-compose.yml) — chỉ mạng nội bộ Docker
	// gọi tới được.
	metricsSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           workerMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info().Int("port", cfg.HTTPPort).Msg("máy chủ /metrics của worker đang chạy")
		if err := metricsSrv.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			// Không làm chết worker: mất /metrics thì mất khả năng quan sát,
			// còn job vẫn phải tiếp tục được xử lý.
			log.Error().Err(err).Msg("máy chủ /metrics dừng")
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsSrv.Shutdown(shutdownCtx)
	}()

	dispatcher := consumer.NewDispatcher(mqClient, log)

	mailSender := consumer.NewMailSender(
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser, cfg.SMTPPass,
		cfg.PublicBaseURL)

	// Module thông báo ở worker đẩy realtime qua RabbitMQ chứ không qua hub:
	// worker không giữ kết nối WebSocket nào. Fanout tới mọi instance api,
	// và instance nào đang giữ người nhận sẽ chuyển tiếp.
	notifUC := ucnotif.NewUsecase(
		repopg.NewNotificationRepository(db),
		repomq.NewPusher(mqClient),
		reporedis.NewPresenceStore(rdb),
		repopg.NewChatLookup(db),
		repomq.NewMailer(mqClient),
	)
	projectConsumer := consumer.NewProjectConsumer(notifUC)

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
		repomq.JobSendNotification:       mailSender.HandleNotification,

		// Tính lương. Job này chịu được chạy lại: ReplaceForPeriod xoá sạch
		// phiếu cũ rồi ghi bộ mới trong một giao dịch.
		repomq.JobCalculatePayroll: payrollConsumer.HandleCalculate,

		// Sự kiện module dự án, từ Phase 5 sinh thông báo thật. Cả bốn dùng
		// chung một handler vì payload của chúng giống hệt nhau.
		domainproject.JobTaskAssigned:      projectConsumer.Handle,
		domainproject.JobTaskStatusChanged: projectConsumer.Handle,
		domainproject.JobTaskMentioned:     projectConsumer.Handle,
		domainproject.JobTaskDueSoon:       projectConsumer.Handle,
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

	// =====================================================================
	// CÔNG VIỆC ĐỊNH KỲ — thông báo và chat
	// =====================================================================

	// Nhắc qua email những thông báo quan trọng chưa đọc.
	//
	// Chạy mỗi 5 phút chứ không mỗi phút: thông báo phải quá hạn chờ 15 phút
	// mới được nhắc, nên quét dày hơn chỉ tốn truy vấn mà không nhắc sớm hơn
	// được phút nào.
	sched.Add(scheduler.Job{
		Name:  "notification.email_reminders",
		Every: 5 * time.Minute,
		Run: func(ctx context.Context) error {
			n, err := notifUC.SendEmailReminders(ctx)
			if err == nil && n > 0 {
				log.Info().Int("sent", n).Msg("đã gửi email nhắc thông báo")
			}
			return err
		},
	})

	// Đồng bộ nhóm chat theo phòng ban và dự án.
	//
	// RunAtStart để nhóm có ngay sau lần triển khai đầu, không phải chờ tới
	// chu kỳ sau. Job tự sửa mọi sai lệch nên chạy lại luôn vô hại.
	chatUC := buildChatUsecase(db, rdb, mqClient)
	sched.Add(scheduler.Job{
		Name:       "chat.sync_auto_groups",
		Every:      15 * time.Minute,
		RunAtStart: true,
		Run: func(ctx context.Context) error {
			created, changed, err := chatUC.SyncAll(ctx)
			if err == nil && (created > 0 || changed > 0) {
				log.Info().Int("created", created).Int("changed", changed).
					Msg("đã đồng bộ nhóm chat tự động")
			}
			return err
		},
	})

	// Đo độ sâu hàng đợi.
	//
	// Chu kỳ 30 giây: đủ dày để thấy hàng đợi đang dồn lên trước khi nó thành
	// sự cố, đủ thưa để không thêm tải đáng kể lên RabbitMQ.
	sched.Add(scheduler.Job{
		Name:       "metrics.queue_depth",
		Every:      30 * time.Second,
		RunAtStart: true,
		Run: func(ctx context.Context) error {
			return consumer.SampleQueueDepth(ctx, mqClient)
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

// workerMux dựng router tối thiểu của worker: chỉ /metrics và /health.
//
// Dùng http.ServeMux thuần chứ không chi: worker không có route nghiệp vụ,
// không có middleware xác thực, không có gì để định tuyến. Kéo cả router của
// api vào đây chỉ để phục vụ hai đường dẫn tĩnh là thêm phụ thuộc không cần.
func workerMux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
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

// buildChatUsecase lắp ráp module chat cho worker.
//
// Worker chỉ dùng nó cho job đồng bộ nhóm tự động, nên notifier và storage là
// nil: job này không gửi tin nhắn và không đụng tới tệp.
func buildChatUsecase(
	db *postgres.DB,
	rdb *goredis.Client,
	mqClient *rabbitmq.Client,
) *ucchat.Usecase {
	companyRepo := repopg.NewCompanyRepository(db)
	deptRepo := repopg.NewDepartmentRepository(db)
	posRepo := repopg.NewPositionRepository(db)
	empRepo := repopg.NewEmployeeRepository(db)
	userRepo := repopg.NewUserRepository(db)
	roleRepo := repopg.NewRoleRepository(db)

	hrUC := uchr.NewUsecase(companyRepo, deptRepo, posRepo, empRepo, userRepo, roleRepo, nil)

	return ucchat.NewUsecase(
		repopg.NewChatConversationRepository(db),
		repopg.NewChatMessageRepository(db),
		repopg.NewChatLookup(db),
		repopg.NewChatSourceRepository(db),
		reporedis.NewPresenceStore(rdb),
		repomq.NewPusher(mqClient),
		nil,
		nil,
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
