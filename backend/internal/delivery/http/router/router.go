// Package router lắp ráp toàn bộ route HTTP của api.
package router

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	goredis "github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/PhamVanPhuc2k2/manage/internal/delivery/http/handler"
	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/config"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
	"github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
	"github.com/PhamVanPhuc2k2/manage/pkg/storage"
)

type Deps struct {
	Config   *config.Config
	Logger   zerolog.Logger
	Version  string
	Postgres *postgres.DB
	Redis    *goredis.Client
	RabbitMQ *rabbitmq.Client
	Storage  *storage.Storage

	JWT        *jwt.Manager
	Sessions   domainauth.SessionStore
	AuthReader domainauth.AuthorizationReader

	PingUC     handler.PingUsecase
	Auth       *handler.AuthHandler
	Employee   *handler.EmployeeHandler
	Department *handler.DepartmentHandler
	Position   *handler.PositionHandler
	Role       *handler.RoleHandler
	Project    *handler.ProjectHandler
	Task       *handler.TaskHandler
	Attendance *handler.AttendanceHandler
	Payroll    *handler.PayrollHandler
	Notif      *handler.NotificationHandler
	Chat       *handler.ChatHandler

	// WS có thể nil trong test. Khi nil, route /ws đơn giản không tồn tại.
	WS http.Handler
}

func New(d Deps) http.Handler {
	r := chi.NewRouter()

	// Thứ tự middleware chính là thứ tự chạy, đọc từ trên xuống.
	r.Use(chimw.RequestID)               // sinh request_id trước tiên
	r.Use(chimw.RealIP)                  // lấy IP thật phía sau nginx
	r.Use(appmw.RequestLogger(d.Logger)) // log + nhét logger vào context
	r.Use(chimw.Recoverer)               // bắt panic, không để sập cả tiến trình
	r.Use(chimw.Timeout(30 * time.Second))
	r.Use(chimw.Compress(5))

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   d.Config.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true, // bắt buộc để trình duyệt gửi cookie refresh
		MaxAge:           300,
	}))

	systemHandler := handler.NewSystemHandler(d.PingUC, d.Version)
	requireAuth := appmw.RequireAuth(d.JWT, d.Sessions, d.AuthReader)

	// /health trả lời ngay, không chạm vào dependency nào.
	// Docker dùng nó để biết container còn SỐNG.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		httpx.OK(w, map[string]string{"status": "ok", "version": d.Version})
	})

	// --- WebSocket ---
	//
	// Nằm NGOÀI /api/v1 và ngoài nhóm requireAuth, có chủ ý:
	//
	//   - Đường dẫn riêng để nginx nhận diện và cấu hình nâng cấp giao thức
	//     cùng timeout dài; trộn vào /api sẽ kéo theo cả giới hạn tốc độ và
	//     timeout 60 giây của REST.
	//   - Không qua RequireAuth vì trình duyệt KHÔNG cho đặt header
	//     Authorization khi mở WebSocket. Handler tự xác minh token trên
	//     query string và tự kiểm tra phiên còn sống — xem ws/handler.go.
	if d.WS != nil {
		r.Handle("/ws", d.WS)
	}

	// /ready kiểm tra dependency. Dùng để biết có nên GỬI TRAFFIC vào không.
	//
	// Phân biệt hai cái này quan trọng: database sập thì container vẫn sống
	// (không cần restart) nhưng chưa sẵn sàng phục vụ.
	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		ready := true

		if err := d.Postgres.HealthCheck(ctx); err != nil {
			checks["postgres"] = err.Error()
			ready = false
		} else {
			checks["postgres"] = "ok"
		}
		if err := d.Redis.Ping(ctx).Err(); err != nil {
			checks["redis"] = err.Error()
			ready = false
		} else {
			checks["redis"] = "ok"
		}
		if err := d.RabbitMQ.HealthCheck(ctx); err != nil {
			checks["rabbitmq"] = err.Error()
			ready = false
		} else {
			checks["rabbitmq"] = "ok"
		}

		// R2 KHÔNG ảnh hưởng tới `ready`: mất chỗ lưu tệp thì không tải ảnh
		// lên được, nhưng toàn bộ phần còn lại của hệ thống vẫn phục vụ bình
		// thường. Cắt traffic vì lý do đó là phản ứng thái quá.
		if d.Storage == nil {
			checks["r2"] = "chưa cấu hình"
		} else if err := d.Storage.HealthCheck(ctx); err != nil {
			checks["r2"] = err.Error()
		} else {
			checks["r2"] = "ok"
		}

		status := http.StatusOK
		if !ready {
			status = http.StatusServiceUnavailable
		}
		httpx.JSON(w, status, httpx.Envelope{Data: checks})
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/ping", systemHandler.Ping)

		// =================================================================
		// CÔNG KHAI — không cần đăng nhập
		// =================================================================
		r.Group(func(r chi.Router) {
			r.Post("/auth/login", d.Auth.Login)
			// Hai endpoint của bước hai. Vẫn công khai: người gọi chưa có
			// token — họ mới qua được mật khẩu. Thứ bảo vệ chúng là id thử
			// thách ngẫu nhiên, bộ đếm 5 lần sai và giới hạn gửi lại.
			r.Post("/auth/verify-otp", d.Auth.VerifyOTP)
			r.Post("/auth/resend-otp", d.Auth.ResendOTP)
			r.Post("/auth/refresh", d.Auth.Refresh)
			r.Post("/auth/forgot-password", d.Auth.ForgotPassword)
			r.Post("/auth/reset-password", d.Auth.ResetPassword)
		})

		// =================================================================
		// CẦN ĐĂNG NHẬP
		// =================================================================
		r.Group(func(r chi.Router) {
			r.Use(requireAuth)

			r.Get("/auth/me", d.Auth.Me)
			r.Post("/auth/logout", d.Auth.Logout)
			r.Post("/auth/logout-all", d.Auth.LogoutAll)
			r.Get("/auth/sessions", d.Auth.ListSessions)
			r.Post("/auth/change-password", d.Auth.ChangePassword)

			// --- Nhân viên ---
			r.Route("/employees", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermEmployeeRead)).
					Get("/", d.Employee.List)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeRead)).
					Get("/{id}", d.Employee.Get)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeCreate)).
					Post("/", d.Employee.Create)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeUpdate)).
					Put("/{id}", d.Employee.Update)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeDelete)).
					Delete("/{id}", d.Employee.Deactivate)

				// Ảnh đại diện. Dùng quyền employee:update vì đây là sửa hồ
				// sơ — nhân viên tự đổi ảnh của mình được nhờ phạm vi "self".
				r.With(appmw.RequirePermission(domainauth.PermEmployeeUpdate)).
					Post("/{id}/avatar/upload-url", d.Employee.RequestAvatarUpload)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeUpdate)).
					Post("/{id}/avatar/confirm", d.Employee.ConfirmAvatar)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeUpdate)).
					Delete("/{id}/avatar", d.Employee.RemoveAvatar)

				// --- Tài khoản đăng nhập của nhân viên ---
				//
				// Dùng quyền employee:create (admin và HR) chứ không phải
				// employee:update. Tạo tài khoản là cấp quyền truy cập hệ
				// thống — nặng hơn hẳn việc sửa số điện thoại. Trưởng phòng
				// có employee:update nhưng không nên tạo được tài khoản.
				r.With(appmw.RequirePermission(domainauth.PermEmployeeCreate)).
					Post("/{id}/account", d.Employee.CreateAccount)
				r.With(appmw.RequirePermission(domainauth.PermEmployeeCreate)).
					Put("/{id}/account/active", d.Employee.SetAccountActive)

				// --- Vai trò của nhân viên ---
				r.With(appmw.RequirePermission(domainauth.PermRoleRead)).
					Get("/{id}/roles", d.Employee.GetRoles)
				r.With(appmw.RequirePermission(domainauth.PermRoleAssign)).
					Put("/{id}/roles", d.Employee.SetRoles)
			})

			// --- Danh mục vai trò ---
			r.With(appmw.RequirePermission(domainauth.PermRoleRead)).
				Get("/roles", d.Role.List)

			// --- Phòng ban ---
			r.Route("/departments", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/", d.Department.List)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/tree", d.Department.Tree)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentRead)).
					Get("/{id}", d.Department.Get)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentCreate)).
					Post("/", d.Department.Create)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentUpdate)).
					Put("/{id}", d.Department.Update)
				r.With(appmw.RequirePermission(domainauth.PermDepartmentDelete)).
					Delete("/{id}", d.Department.Delete)
			})

			// --- Dự án ---
			//
			// Lưu ý về mô hình quyền: middleware dưới đây chỉ trả lời "người
			// này có được làm loại việc đó không". Việc "được đụng vào ĐÚNG
			// dự án nào" do tầng usecase kiểm tra qua bảng project_members —
			// middleware không nhìn thấy bản ghi cụ thể nên không làm được.
			r.Route("/projects", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
					Get("/", d.Project.List)
				r.With(appmw.RequirePermission(domainauth.PermProjectCreate)).
					Post("/", d.Project.Create)
				r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
					Get("/{id}", d.Project.Get)
				r.With(appmw.RequirePermission(domainauth.PermProjectUpdate)).
					Put("/{id}", d.Project.Update)
				r.With(appmw.RequirePermission(domainauth.PermProjectDelete)).
					Delete("/{id}", d.Project.Delete)

				// Bảng Kanban và tiến độ đọc bằng quyền xem CÔNG VIỆC, không
				// phải quyền xem dự án: người chỉ được xem danh sách dự án mà
				// không được xem việc thì bảng Kanban là rỗng nghĩa.
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{id}/board", d.Task.Board)
				r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
					Get("/{id}/progress", d.Project.Progress)

				// --- Thành viên dự án ---
				r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
					Get("/{id}/members", d.Project.ListMembers)
				r.With(appmw.RequirePermission(domainauth.PermProjectUpdate)).
					Post("/{id}/members", d.Project.AddMember)
				r.With(appmw.RequirePermission(domainauth.PermProjectUpdate)).
					Put("/{id}/members/{employeeID}", d.Project.UpdateMemberRole)
				// Gỡ thành viên dùng quyền project:read vì ai cũng TỰ RỜI dự
				// án được. Việc gỡ NGƯỜI KHÁC do usecase chặn — nó là chỗ duy
				// nhất biết người bị gỡ có phải chính người gọi hay không.
				r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
					Delete("/{id}/members/{employeeID}", d.Project.RemoveMember)
			})

			// --- Công việc ---
			r.Route("/tasks", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/", d.Task.List)
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/my", d.Task.MyTasks)
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/overdue", d.Task.Overdue)
				r.With(appmw.RequirePermission(domainauth.PermTaskCreate)).
					Post("/", d.Task.Create)

				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}", d.Task.Get)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Put("/{taskID}", d.Task.Update)
				r.With(appmw.RequirePermission(domainauth.PermTaskDelete)).
					Delete("/{taskID}", d.Task.Delete)

				// Kéo-thả trên Kanban. PATCH vì đây là sửa MỘT PHẦN trạng
				// thái, không phải thay cả bản ghi như PUT.
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Patch("/{taskID}/move", d.Task.Move)

				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}/subtasks", d.Task.ListSubtasks)
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}/activities", d.Task.ListActivities)

				// --- Bình luận ---
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}/comments", d.Task.ListComments)
				// Bình luận dùng quyền task:update chứ không phải task:create:
				// viết bình luận là góp vào một công việc đã có, không phải
				// tạo công việc mới.
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Post("/{taskID}/comments", d.Task.CreateComment)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Put("/comments/{commentID}", d.Task.UpdateComment)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Delete("/comments/{commentID}", d.Task.DeleteComment)

				// --- Tệp đính kèm ---
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}/attachments", d.Task.ListAttachments)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Post("/{taskID}/attachments/upload-url", d.Task.RequestAttachmentUpload)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Post("/{taskID}/attachments/confirm", d.Task.ConfirmAttachment)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Delete("/attachments/{attachmentID}", d.Task.DeleteAttachment)

				// --- Ghi nhận thời gian ---
				r.With(appmw.RequirePermission(domainauth.PermTaskRead)).
					Get("/{taskID}/timelogs", d.Task.ListTimelogs)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Post("/{taskID}/timelogs", d.Task.LogTime)
				r.With(appmw.RequirePermission(domainauth.PermTaskUpdate)).
					Delete("/timelogs/{timelogID}", d.Task.DeleteTimelog)
			})

			// --- Chấm công ---
			//
			// Lưu ý về mô hình quyền: attendance:read chỉ cho xem công CỦA
			// CHÍNH MÌNH. Xem của người khác cần thêm attendance:read_all, và
			// tầng usecase vẫn giới hạn tiếp trong phạm vi phòng ban — dữ
			// liệu chấm công là dữ liệu cá nhân nhạy cảm.
			r.Route("/attendance", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/today", d.Attendance.Today)
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/days", d.Attendance.ListDays)
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/day", d.Attendance.GetDay)
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/summary", d.Attendance.MonthSummary)

				// Ai đang online. Cần attendance:read_all vì đây là thông tin
				// về người khác.
				r.With(appmw.RequirePermission(domainauth.PermAttendanceReadAll)).
					Get("/team", d.Attendance.TeamPresence)

				// Check-in thủ công cho trường hợp ngoại lệ (mất mạng, họp
				// ngoài). Dùng quyền đọc vì người ta chỉ ghi cho chính mình.
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Post("/check-in", d.Attendance.CheckIn)

				// --- Yêu cầu điều chỉnh công ---
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/adjustments", d.Attendance.ListAdjustments)
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Post("/adjustments", d.Attendance.CreateAdjustment)
				// Duyệt thì cần quyền quản lý công thật sự.
				r.With(appmw.RequirePermission(domainauth.PermAttendanceManage)).
					Put("/adjustments/{id}/decision", d.Attendance.DecideAdjustment)

				// --- Khoá kỳ công ---
				r.With(appmw.RequirePermission(domainauth.PermAttendanceManage)).
					Post("/lock", d.Attendance.LockPeriod)
			})

			// --- Nghỉ phép ---
			r.Route("/leaves", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermLeaveRead)).
					Get("/", d.Attendance.ListLeaves)
				r.With(appmw.RequirePermission(domainauth.PermLeaveCreate)).
					Post("/", d.Attendance.CreateLeave)
				r.With(appmw.RequirePermission(domainauth.PermLeaveApprove)).
					Put("/{id}/decision", d.Attendance.DecideLeave)
				// Huỷ đơn của chính mình chỉ cần quyền tạo đơn — usecase chặn
				// việc huỷ đơn người khác.
				r.With(appmw.RequirePermission(domainauth.PermLeaveCreate)).
					Delete("/{id}", d.Attendance.CancelLeave)

				// --- Quỹ ngày phép ---
				r.With(appmw.RequirePermission(domainauth.PermLeaveRead)).
					Get("/balance", d.Attendance.MyBalance)
				r.With(appmw.RequirePermission(domainauth.PermLeaveApprove)).
					Get("/balances", d.Attendance.ListBalances)
				r.With(appmw.RequirePermission(domainauth.PermLeaveManage)).
					Put("/balances", d.Attendance.SetBalance)
			})

			// --- Khung giờ làm việc ---
			r.Route("/work-schedules", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermAttendanceRead)).
					Get("/", d.Attendance.ListSchedules)
				r.With(appmw.RequirePermission(domainauth.PermScheduleManage)).
					Post("/", d.Attendance.CreateSchedule)
				r.With(appmw.RequirePermission(domainauth.PermScheduleManage)).
					Put("/{id}", d.Attendance.UpdateSchedule)
				r.With(appmw.RequirePermission(domainauth.PermScheduleManage)).
					Delete("/{id}", d.Attendance.DeleteSchedule)
			})

			// --- Ngày lễ ---
			r.Route("/holidays", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermLeaveRead)).
					Get("/", d.Attendance.ListHolidays)
				r.With(appmw.RequirePermission(domainauth.PermLeaveManage)).
					Post("/", d.Attendance.CreateHoliday)
				r.With(appmw.RequirePermission(domainauth.PermLeaveManage)).
					Delete("/{id}", d.Attendance.DeleteHoliday)
			})

			// --- Lương ---
			//
			// Lương là dữ liệu nhạy cảm nhất hệ thống. Ba điểm khác biệt so
			// với các module trên:
			//
			//   - payroll:read_own chỉ cho xem phiếu CỦA CHÍNH MÌNH. Không
			//     có ngoại lệ theo phòng ban như chấm công — trưởng phòng
			//     KHÔNG xem được lương nhân viên phòng mình.
			//   - payroll:manage (chạy tính lương) tách khỏi payroll:approve
			//     (khoá kỳ, xác nhận đã trả): nguyên tắc bốn mắt.
			//   - Mọi lượt XEM đều được ghi vào audit_logs, không chỉ lượt
			//     sửa. Rò rỉ bảng lương thường là do đọc.
			r.Route("/payroll", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadAll)).
					Get("/periods", d.Payroll.ListPeriods)
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadAll)).
					Get("/periods/{id}", d.Payroll.GetPeriod)
				r.With(appmw.RequirePermission(domainauth.PermPayrollManage)).
					Post("/periods", d.Payroll.CreatePeriod)
				r.With(appmw.RequirePermission(domainauth.PermPayrollManage)).
					Post("/periods/{id}/calculate", d.Payroll.Calculate)
				// Đổi trạng thái kỳ cần quyền RIÊNG: người chạy tính lương
				// không được tự chốt kỳ mình vừa chạy.
				r.With(appmw.RequirePermission(domainauth.PermPayrollApprove)).
					Put("/periods/{id}/status", d.Payroll.ChangeStatus)

				r.With(appmw.RequirePermission(domainauth.PermPayrollReadAll)).
					Get("/periods/{id}/cost-by-department", d.Payroll.CostByDepartment)
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadAll)).
					Get("/cost-by-month", d.Payroll.CostByMonth)

				// --- Phiếu lương ---
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadAll)).
					Get("/payslips", d.Payroll.ListPayslips)
				// Phiếu của chính mình: ai cũng xem được.
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadOwn)).
					Get("/payslips/my", d.Payroll.MyPayslips)
				// Usecase tự kiểm tra là phiếu của mình hay của người khác,
				// nên route chỉ cần quyền tối thiểu.
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadOwn)).
					Get("/payslips/{id}", d.Payroll.GetPayslip)
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadOwn)).
					Get("/payslips/{id}/document", d.Payroll.Document)
				r.With(appmw.RequirePermission(domainauth.PermPayrollManage)).
					Put("/payslips/{id}", d.Payroll.UpdatePayslip)

				// --- Tham số tính lương ---
				r.With(appmw.RequirePermission(domainauth.PermSalaryRead)).
					Get("/settings", d.Payroll.GetSettings)
				r.With(appmw.RequirePermission(domainauth.PermSalaryManage)).
					Put("/settings", d.Payroll.UpdateSettings)

				// --- Nhật ký truy cập dữ liệu nhạy cảm ---
				r.With(appmw.RequirePermission(domainauth.PermAuditRead)).
					Get("/audit", d.Payroll.ListAudit)
			})

			// --- Cấu hình lương theo nhân viên ---
			//
			// Nằm dưới /employees vì đó là thứ nó mô tả. Xem cấu hình của
			// CHÍNH MÌNH luôn được (usecase kiểm tra), nên route chỉ cần
			// quyền đọc phiếu lương của mình.
			r.Route("/employees/{employeeID}/salary", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadOwn)).
					Get("/", d.Payroll.GetStructure)
				r.With(appmw.RequirePermission(domainauth.PermPayrollReadOwn)).
					Get("/history", d.Payroll.StructureHistory)
				r.With(appmw.RequirePermission(domainauth.PermSalaryManage)).
					Put("/", d.Payroll.SetStructure)
			})

			// --- Báo cáo khối lượng việc theo nhân viên ---
			r.With(appmw.RequirePermission(domainauth.PermProjectRead)).
				Get("/reports/workload", d.Project.Workload)

			// --- Thông báo ---
			//
			// KHÔNG có middleware quyền nào: thông báo là hộp thư cá nhân,
			// mỗi người chỉ đọc được của chính mình và usecase khoá cứng theo
			// actor. Thêm một quyền ở đây chỉ tạo ảo giác rằng có thể cấp nó
			// cho người khác.
			r.Route("/notifications", func(r chi.Router) {
				r.Get("/", d.Notif.List)
				r.Get("/summary", d.Notif.Summary)
				r.Post("/read", d.Notif.MarkRead)
				r.Post("/read-all", d.Notif.MarkAllRead)

				r.Get("/preferences", d.Notif.Preferences)
				r.Put("/preferences", d.Notif.SetPreference)
			})

			// --- Chat ---
			//
			// Mô hình quyền giống dự án: middleware chỉ trả lời "người này có
			// được dùng chat không". Việc "được đọc hội thoại NÀO" do bảng
			// conversation_members quyết định và tầng usecase kiểm tra.
			r.Route("/chat", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Get("/conversations", d.Chat.ListConversations)
				r.With(appmw.RequirePermission(domainauth.PermChatCreate)).
					Post("/conversations", d.Chat.CreateConversation)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Get("/unread", d.Chat.Unread)

				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Get("/conversations/{id}", d.Chat.GetConversation)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Put("/conversations/{id}", d.Chat.Rename)
				// Ghim và tắt thông báo là tuỳ chọn RIÊNG của người xem, nên
				// chỉ cần quyền đọc — không đụng gì tới người khác.
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Patch("/conversations/{id}/flags", d.Chat.SetFlags)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Post("/conversations/{id}/leave", d.Chat.Leave)

				r.With(appmw.RequirePermission(domainauth.PermChatCreate)).
					Post("/conversations/{id}/members", d.Chat.AddMembers)
				r.With(appmw.RequirePermission(domainauth.PermChatCreate)).
					Delete("/conversations/{id}/members/{employeeID}", d.Chat.RemoveMember)
				r.With(appmw.RequirePermission(domainauth.PermChatCreate)).
					Put("/conversations/{id}/members/{employeeID}/admin", d.Chat.SetAdmin)

				// --- Tin nhắn ---
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Get("/conversations/{id}/messages", d.Chat.History)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Post("/conversations/{id}/messages", d.Chat.Send)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Post("/conversations/{id}/read", d.Chat.MarkRead)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Post("/conversations/{id}/upload-url", d.Chat.PresignUpload)

				// Sửa và thu hồi dùng quyền đọc: usecase mới là chỗ biết tin
				// nhắn đó có phải của người gọi hay không.
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Put("/messages/{messageID}", d.Chat.EditMessage)
				r.With(appmw.RequirePermission(domainauth.PermChatRead)).
					Delete("/messages/{messageID}", d.Chat.DeleteMessage)
			})

			// --- Chức vụ ---
			r.Route("/positions", func(r chi.Router) {
				r.With(appmw.RequirePermission(domainauth.PermPositionRead)).
					Get("/", d.Position.List)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Post("/", d.Position.Create)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Put("/{id}", d.Position.Update)
				r.With(appmw.RequirePermission(domainauth.PermPositionManage)).
					Delete("/{id}", d.Position.Delete)
			})
		})
	})

	return r
}
