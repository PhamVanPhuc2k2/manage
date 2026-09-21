// Binary api: HTTP REST (và WebSocket từ Phase 5).
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

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/handler"
	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/router"
	deliveryws "github.com/PhamVanPhuc2k2/manage/internal/delivery/ws"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	repopg "github.com/PhamVanPhuc2k2/manage/internal/repository/postgres"
	repomq "github.com/PhamVanPhuc2k2/manage/internal/repository/rabbitmq"
	reporedis "github.com/PhamVanPhuc2k2/manage/internal/repository/redis"
	reposto "github.com/PhamVanPhuc2k2/manage/internal/repository/storage"
	ucatt "github.com/PhamVanPhuc2k2/manage/internal/usecase/attendance"
	ucauth "github.com/PhamVanPhuc2k2/manage/internal/usecase/auth"
	ucchat "github.com/PhamVanPhuc2k2/manage/internal/usecase/chat"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	ucnotif "github.com/PhamVanPhuc2k2/manage/internal/usecase/notification"
	ucpay "github.com/PhamVanPhuc2k2/manage/internal/usecase/payroll"
	ucproject "github.com/PhamVanPhuc2k2/manage/internal/usecase/project"
	ucsystem "github.com/PhamVanPhuc2k2/manage/internal/usecase/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
	"github.com/PhamVanPhuc2k2/manage/pkg/storage"
)

// Ba biến này được nhúng lúc build qua -ldflags -X.
var (
	version   = "dev"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "api dừng do lỗi: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("api")
	if err != nil {
		return err
	}

	log := logger.New("api", cfg.LogLevel, cfg.Env)
	log.Info().
		Str("version", version).
		Str("git_sha", gitSHA).
		Str("build_time", buildTime).
		Str("env", cfg.Env).
		Msg("đang khởi động api")

	// Context này bị huỷ khi nhận SIGTERM (docker stop) hoặc SIGINT (Ctrl+C).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// --- Hạ tầng ---
	db, err := postgres.New(ctx, cfg.PostgresDSN, cfg.PostgresMaxConn, log)
	if err != nil {
		return err
	}
	defer db.Close()

	rdb := goredis.NewClient(&goredis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer func() { _ = rdb.Close() }()

	mqClient, err := rabbitmq.New(ctx, cfg.RabbitMQURL, log)
	if err != nil {
		return err
	}
	defer func() { _ = mqClient.Close() }()

	jwtMgr, err := jwt.NewManager(cfg.JWTSecret, cfg.JWTAccessTTL, "manage")
	if err != nil {
		return err
	}

	// Cloudflare R2. Trả về nil khi chưa cấu hình — hệ thống vẫn chạy bình
	// thường, chỉ chức năng tải tệp báo lỗi rõ ràng. Nhờ vậy phát triển phần
	// không liên quan tới tệp không cần tài khoản Cloudflare.
	r2, err := storage.New(ctx, storage.Config{
		AccountID:       cfg.R2AccountID,
		AccessKeyID:     cfg.R2AccessKeyID,
		SecretAccessKey: cfg.R2SecretAccessKey,
		Bucket:          cfg.R2Bucket,
		PublicURL:       cfg.R2PublicURL,
	})
	if err != nil {
		return fmt.Errorf("khởi tạo Cloudflare R2: %w", err)
	}
	if r2 == nil {
		log.Warn().Msg("chưa cấu hình Cloudflare R2 — chức năng tải tệp sẽ không dùng được")
	} else if err := r2.HealthCheck(ctx); err != nil {
		// Cảnh báo chứ không dừng: R2 trục trặc không nên chặn cả hệ thống
		// khởi động, phần lớn chức năng không đụng tới tệp.
		log.Error().Err(err).Msg("không truy cập được bucket R2")
	} else {
		log.Info().Str("bucket", cfg.R2Bucket).Msg("đã kết nối Cloudflare R2")
	}

	// =====================================================================
	// COMPOSITION ROOT — nơi DUY NHẤT biết cả interface lẫn bản hiện thực.
	//
	// Mọi tầng khác chỉ biết interface. Muốn đổi PostgreSQL sang thứ khác
	// thì sửa đúng ở đây, không đụng vào usecase.
	// =====================================================================

	// --- Repository ---
	clockRepo := repopg.NewClockRepository(db)
	cacheRepo := reporedis.NewCacheRepository(rdb)
	publisher := repomq.NewJobPublisher(mqClient)

	userRepo := repopg.NewUserRepository(db)
	authReader := repopg.NewAuthorizationReader(db)
	companyRepo := repopg.NewCompanyRepository(db)
	deptRepo := repopg.NewDepartmentRepository(db)
	posRepo := repopg.NewPositionRepository(db)
	empRepo := repopg.NewEmployeeRepository(db)
	roleRepo := repopg.NewRoleRepository(db)

	projectRepo := repopg.NewProjectRepository(db)
	projectMemberRepo := repopg.NewProjectMemberRepository(db)
	taskRepo := repopg.NewTaskRepository(db)
	taskCommentRepo := repopg.NewTaskCommentRepository(db)
	taskAttachmentRepo := repopg.NewTaskAttachmentRepository(db)
	taskActivityRepo := repopg.NewTaskActivityRepository(db)
	taskTimelogRepo := repopg.NewTaskTimelogRepository(db)
	projectReportRepo := repopg.NewProjectReportRepository(db)

	presenceStore := reporedis.NewPresenceStore(rdb)

	scheduleRepo := repopg.NewScheduleRepository(db)
	holidayRepo := repopg.NewHolidayRepository(db)
	attSessionRepo := repopg.NewAttendanceSessionRepository(db)
	attDayRepo := repopg.NewAttendanceDayRepository(db)
	adjustmentRepo := repopg.NewAdjustmentRepository(db)
	leaveRepo := repopg.NewLeaveRepository(db)
	balanceRepo := repopg.NewBalanceRepository(db)

	payrollSettingsRepo := repopg.NewPayrollSettingsRepository(db)
	salaryStructureRepo := repopg.NewSalaryStructureRepository(db)
	payrollPeriodRepo := repopg.NewPayrollPeriodRepository(db)
	payslipRepo := repopg.NewPayslipRepository(db)
	auditRepo := repopg.NewAuditRepository(db)

	// Cổng hẹp để module lương đọc ngày công. Đọc thẳng bảng
	// attendance_days chứ không gọi qua usecase/attendance — hai tầng
	// nghiệp vụ import chéo nhau là đường nhanh nhất tới phụ thuộc vòng.
	attendanceLookup := repopg.NewAttendanceLookup(db)
	realtimeBus := repomq.NewRealtimeBus(mqClient)

	// Cổng hẹp để module dự án tra cứu nhân viên. Module dự án chỉ biết
	// interface domainproject.EmployeeLookup — nó không được và không cần
	// biết dữ liệu nhân viên nằm ở đâu.
	employeeLookup := repopg.NewEmployeeLookup(db)

	// Cổng hẹp cho module chat và module thông báo. Kiểu riêng chứ không
	// dùng lại employeeLookup: hai bên hỏi những câu khác nhau.
	chatLookup := repopg.NewChatLookup(db)

	sessionStore := reporedis.NewSessionStore(rdb)
	refreshStore := reporedis.NewRefreshStore(rdb)
	throttle := reporedis.NewLoginThrottle(rdb)
	resetStore := reporedis.NewPasswordResetStore(rdb)
	otpStore := reporedis.NewOTPStore(rdb)
	mailer := repomq.NewMailer(mqClient)

	// --- Usecase ---
	pingUC := ucsystem.NewPingUsecase(clockRepo, cacheRepo, publisher)

	authUC := ucauth.NewUsecase(
		userRepo, authReader, sessionStore, refreshStore, throttle, resetStore, otpStore,
		jwtMgr, mailer,
		ucauth.Config{
			AccessTTL:     cfg.JWTAccessTTL,
			RefreshTTL:    cfg.JWTRefreshTTL,
			PublicBaseURL: cfg.PublicBaseURL,
			OTPEnabled:    cfg.AuthOTPEnabled,
		},
	)
	if !cfg.AuthOTPEnabled {
		log.Warn().Msg("AUTH_OTP_ENABLED=false — đăng nhập chỉ cần mật khẩu")
	}

	// storage có thể nil — usecase kiểm tra nil ở mọi chỗ dùng tới tệp.
	var fileStorage domainhr.FileStorage
	if r2 != nil {
		fileStorage = reposto.NewRepository(r2)
	}

	hrUC := uchr.NewUsecase(
		companyRepo, deptRepo, posRepo, empRepo, userRepo, roleRepo, fileStorage)

	// Module dự án dùng lại đúng lớp lưu trữ đó, nhưng qua interface của
	// riêng nó — hai module không chia sẻ interface, chỉ chia sẻ bản hiện thực.
	var projectStorage domainproject.FileStorage
	if r2 != nil {
		projectStorage = reposto.NewRepository(r2)
	}

	projectUC := ucproject.NewUsecase(
		projectRepo, projectMemberRepo, taskRepo, taskCommentRepo,
		taskAttachmentRepo, taskActivityRepo, taskTimelogRepo, projectReportRepo,
		employeeLookup,
		// hrUC đáp ứng CompanyLookup nhờ method CurrentCompanyID. Nối ở đây
		// thay vì cho usecase/project import usecase/hr: hai tầng nghiệp vụ
		// import chéo nhau là đường nhanh nhất tới phụ thuộc vòng.
		hrUC,
		projectStorage,
		repomq.NewProjectEventPublisher(mqClient),
	)

	// Module chấm công đọc presence từ Redis — chính bảng mà hub WebSocket
	// ghi vào mỗi khi nhận heartbeat. Đây là chỗ Phase 3 nối vào Phase 5.
	attendanceUC := ucatt.NewUsecase(
		scheduleRepo, holidayRepo, attSessionRepo, attDayRepo,
		adjustmentRepo, leaveRepo, balanceRepo,
		employeeLookup, presenceStore, hrUC,
	)

	payrollUC := ucpay.NewUsecase(
		payrollSettingsRepo, salaryStructureRepo, payrollPeriodRepo,
		payslipRepo, auditRepo,
		attendanceLookup, employeeLookup, hrUC,
		repomq.NewPayrollJobs(mqClient),
		mailer,
	)

	// Nối hr với auth: vô hiệu hoá nhân viên phải cắt luôn phiên đăng nhập
	// của họ, nếu không họ vẫn dùng được hệ thống tới khi token hết hạn.
	//
	// Dùng callback thay vì import trực tiếp để tránh phụ thuộc vòng giữa
	// hai module.
	hrUC.SetWelcomeMailer(mailer)

	hrUC.SetOnEmployeeDeactivated(func(ctx context.Context, userID uuid.UUID) {
		if err := authUC.LogoutAll(ctx, userID); err != nil {
			log.Error().Err(err).Str("user_id", userID.String()).
				Msg("không cắt được phiên của nhân viên vừa bị vô hiệu hoá")
		}
	})

	// --- Hạ tầng WebSocket ---
	//
	// instanceID phân biệt các replica api với nhau. Dùng hostname vì trong
	// Docker mỗi container có hostname riêng và ổn định suốt vòng đời của
	// nó; rơi về một uuid ngẫu nhiên khi không đọc được.
	instanceID, err := os.Hostname()
	if err != nil || instanceID == "" {
		instanceID = uuid.NewString()
	}

	hub := deliveryws.NewHub(log, instanceID, presenceStore, realtimeBus)

	// Mỗi instance tự nhận bản tin từ exchange fanout rồi giao cho những
	// client đang nối vào chính nó. Chạy nền, tự dừng khi ctx bị huỷ.
	go func() {
		if err := realtimeBus.Subscribe(ctx, hub.Deliver); err != nil {
			// Không làm chết tiến trình: mất fan-out nghĩa là realtime chỉ
			// còn hoạt động trong phạm vi instance này, còn toàn bộ REST
			// vẫn phục vụ bình thường.
			log.Error().Err(err).Msg("dừng nhận bản tin realtime")
		}
	}()

	log.Info().Str("instance_id", instanceID).Msg("hub WebSocket đã sẵn sàng")

	// --- Thông báo và chat ---
	//
	// Cả hai đẩy realtime qua wsPusher (bọc Hub), không qua RabbitMQ trực
	// tiếp: người nhận nối vào chính instance này được gửi thẳng, người nối
	// vào instance khác đi tiếp qua fanout — Hub.Publish lo cả hai.
	wsPusher := deliveryws.NewPusher(hub)

	notifUC := ucnotif.NewUsecase(
		repopg.NewNotificationRepository(db),
		wsPusher,
		presenceStore,
		chatLookup,
		mailer,
	)

	// Module chat dùng lại lớp lưu trữ R2 qua interface của riêng nó, đúng
	// như module hr và module dự án.
	var chatStorage domainchat.Storage
	if r2 != nil {
		chatStorage = reposto.NewRepository(r2)
	}

	chatUC := ucchat.NewUsecase(
		repopg.NewChatConversationRepository(db),
		repopg.NewChatMessageRepository(db),
		chatLookup,
		repopg.NewChatSourceRepository(db),
		presenceStore,
		wsPusher,
		// notifUC đáp ứng chat.Notifier nhờ method NotifyNewMessage. Nối ở
		// đây thay vì cho usecase/chat import usecase/notification — hai tầng
		// nghiệp vụ import chéo nhau là đường nhanh nhất tới phụ thuộc vòng.
		notifUC,
		chatStorage,
		hrUC,
	)

	// Cắm chat vào hub SAU khi dựng: usecase chat cần một Pusher, mà Pusher
	// lại bọc chính hub này. Truyền qua hàm dựng sẽ tạo vòng tròn không gỡ được.
	hub.SetChat(chatUC)

	// --- Handler ---
	// Cookie Secure chỉ bật khi chạy HTTPS. Bật ở môi trường dev HTTP sẽ
	// khiến trình duyệt vứt cookie đi và refresh không bao giờ hoạt động.
	secureCookie := cfg.IsProduction()

	srv := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: router.New(router.Deps{
			Config:     cfg,
			Logger:     log,
			Version:    version,
			Postgres:   db,
			Redis:      rdb,
			RabbitMQ:   mqClient,
			Storage:    r2,
			JWT:        jwtMgr,
			Sessions:   sessionStore,
			AuthReader: authReader,
			PingUC:     pingUC,
			Auth:       handler.NewAuthHandler(authUC, secureCookie),
			Employee:   handler.NewEmployeeHandler(hrUC),
			Department: handler.NewDepartmentHandler(hrUC),
			Position:   handler.NewPositionHandler(hrUC),
			Role:       handler.NewRoleHandler(hrUC),
			Project:    handler.NewProjectHandler(projectUC),
			Task:       handler.NewTaskHandler(projectUC),
			Attendance: handler.NewAttendanceHandler(attendanceUC),
			Payroll:    handler.NewPayrollHandler(payrollUC),
			Notif:      handler.NewNotificationHandler(notifUC),
			Chat:       handler.NewChatHandler(chatUC),
			WS: deliveryws.NewHandler(
				hub, jwtMgr, sessionStore, cfg.CORSAllowedOrigins),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Int("port", cfg.HTTPPort).Msg("HTTP server đang chạy")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err

	case <-ctx.Done():
		log.Info().Msg("nhận tín hiệu dừng, đang tắt êm")

		// Đóng WebSocket TRƯỚC khi đóng HTTP server.
		//
		// srv.Shutdown chờ mọi kết nối đang mở kết thúc, mà kết nối WebSocket
		// thì không tự kết thúc — nó sống tới khi một bên đóng. Không đóng
		// trước thì Shutdown treo đúng hết ShutdownTimeout rồi mới cắt ngang,
		// và client không nhận được frame Close nên không biết đường nối lại.
		hub.CloseAll()

		// Dùng context MỚI, không dùng ctx đã bị huỷ — nếu không thì
		// Shutdown trả về ngay lập tức và request đang dở bị cắt ngang.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("tắt HTTP server: %w", err)
		}
		log.Info().Msg("api đã dừng")
		return nil
	}
}
