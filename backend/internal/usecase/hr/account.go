package hr

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

// WelcomeMailer gửi mail chào mừng kèm mật khẩu tạm.
//
// Khai báo ở đây — phía người dùng — nên module hr không cần biết mail đi
// qua RabbitMQ hay SMTP trực tiếp.
type WelcomeMailer interface {
	SendWelcome(ctx context.Context, email, name, tempPassword string) error
}

// SetWelcomeMailer nối module hr với hạ tầng gửi mail. Gọi ở cmd/api.
func (u *Usecase) SetWelcomeMailer(m WelcomeMailer) { u.welcomeMailer = m }

type CreateAccountResult struct {
	UserID uuid.UUID `json:"user_id"`
	Email  string    `json:"email"`
	// TempPassword trả về ĐÚNG MỘT LẦN trong response này.
	//
	// Vì sao trả ra chứ không chỉ gửi mail? Vì gửi mail có thể lỗi hoặc rơi
	// vào hộp thư rác, và lúc đó HR không còn cách nào đưa mật khẩu cho nhân
	// viên ngoài việc đặt lại. Trả ra đây để HR chép tay được nếu cần.
	//
	// Mật khẩu này KHÔNG được ghi vào log và không lưu ở đâu khác.
	TempPassword string `json:"temp_password"`
}

// CreateAccount tạo tài khoản đăng nhập cho một nhân viên đã có hồ sơ.
func (u *Usecase) CreateAccount(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) (*CreateAccountResult, error) {
	log := logger.FromContext(ctx)

	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}
	if emp.Status == domainhr.StatusResigned {
		return nil, apperror.Invalid("Không tạo tài khoản cho nhân viên đã nghỉ việc", nil)
	}

	// Mỗi nhân viên chỉ một tài khoản (ràng buộc UNIQUE ở database cũng
	// chặn, nhưng báo lỗi ở đây thì thông báo dễ hiểu hơn nhiều).
	if existing, err := u.users.FindByEmployeeID(ctx, employeeID); err == nil && existing != nil {
		return nil, apperror.Conflict("Nhân viên này đã có tài khoản đăng nhập")
	}

	password, err := token.RandomPassword(16)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	pwHash, err := hash.Password(password)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	user := &domainhr.User{
		EmployeeID:   employeeID,
		Email:        strings.ToLower(strings.TrimSpace(emp.Email)),
		PasswordHash: pwHash,
		IsActive:     true,
		// Bắt đổi ngay lần đăng nhập đầu: mật khẩu này đã đi qua tay HR và
		// qua hộp thư, không còn là bí mật của riêng nhân viên.
		MustChangePassword: true,
	}
	if err := u.users.Create(ctx, user); err != nil {
		return nil, apperror.Internal(err)
	}

	// Vai trò mặc định là "employee" — quyền thấp nhất.
	//
	// Nguyên tắc đặc quyền tối thiểu: tài khoản mới không được có quyền gì
	// hơn mức cơ bản. Muốn cao hơn thì admin phải gán tay, và thao tác đó
	// đi qua endpoint riêng có kiểm soát.
	if role, err := u.roles.GetByCode(ctx, "employee"); err == nil {
		if err := u.users.AssignRole(ctx, user.ID, role.ID, actor.UserID); err != nil {
			log.Error().Err(err).Msg("không gán được vai trò mặc định")
		}
	}

	if u.welcomeMailer != nil {
		if err := u.welcomeMailer.SendWelcome(ctx, user.Email, emp.FullName, password); err != nil {
			// Không chặn: mật khẩu vẫn trả về trong response để HR đưa tay.
			log.Error().Err(err).Msg("không gửi được mail chào mừng")
		}
	}

	log.Info().
		Str("employee_id", employeeID.String()).
		Str("created_by", actor.UserID.String()).
		Msg("đã tạo tài khoản đăng nhập")

	return &CreateAccountResult{
		UserID:       user.ID,
		Email:        user.Email,
		TempPassword: password,
	}, nil
}

// SetAccountActive bật/tắt tài khoản mà không xoá hồ sơ nhân viên.
func (u *Usecase) SetAccountActive(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	active bool,
) error {
	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return apperror.NotFound("nhân viên")
	}

	user, err := u.users.FindByEmployeeID(ctx, employeeID)
	if err != nil || user == nil {
		return apperror.NotFound("tài khoản")
	}
	if user.ID == actor.UserID && !active {
		return apperror.Invalid("Không thể tự vô hiệu hoá tài khoản của mình", nil)
	}

	if err := u.users.SetActive(ctx, user.ID, active); err != nil {
		return apperror.Internal(err)
	}

	// Tắt tài khoản phải cắt phiên ngay, nếu không người đó vẫn dùng được
	// hệ thống cho tới khi access token hết hạn.
	if !active && u.onEmployeeDeactivated != nil {
		u.onEmployeeDeactivated(ctx, user.ID)
	}
	return nil
}
