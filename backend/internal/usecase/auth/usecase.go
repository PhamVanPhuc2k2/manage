// Package auth là tầng nghiệp vụ của xác thực và phân quyền.
package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
)

// Mailer là cổng gửi mail. Khai báo ở đây — phía người dùng — nên usecase
// không cần biết mail đi qua RabbitMQ hay SMTP trực tiếp.
type Mailer interface {
	SendPasswordReset(ctx context.Context, email, name, resetURL string) error
	SendSuspiciousActivity(ctx context.Context, email, name, ip, userAgent string) error
	SendLoginOTP(ctx context.Context, email, name, code string, ttlMinutes int, ip string) error
}

type Config struct {
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	ResetTokenTTL time.Duration
	PublicBaseURL string

	// OTPEnabled bật bước xác minh mã ở lần đăng nhập.
	//
	// Có công tắc vì hai lý do: môi trường kiểm thử tự động cần tắt để chạy
	// nhanh, và nếu SMTP hỏng thì tắt tạm còn hơn cả công ty không vào được
	// hệ thống. Mặc định BẬT — an toàn phải là mặc định, không phải tuỳ chọn.
	OTPEnabled bool
}

type Usecase struct {
	users    domainhr.UserRepository
	auth     domainauth.AuthorizationReader
	sessions domainauth.SessionStore
	refresh  domainauth.RefreshStore
	throttle domainauth.LoginThrottle
	reset    domainauth.PasswordResetStore
	otp      domainauth.OTPStore
	jwt      *jwt.Manager
	mailer   Mailer
	cfg      Config
}

func NewUsecase(
	users domainhr.UserRepository,
	authReader domainauth.AuthorizationReader,
	sessions domainauth.SessionStore,
	refresh domainauth.RefreshStore,
	throttle domainauth.LoginThrottle,
	reset domainauth.PasswordResetStore,
	otp domainauth.OTPStore,
	jwtMgr *jwt.Manager,
	mailer Mailer,
	cfg Config,
) *Usecase {
	if cfg.ResetTokenTTL == 0 {
		cfg.ResetTokenTTL = 30 * time.Minute
	}
	return &Usecase{
		users:    users,
		auth:     authReader,
		sessions: sessions,
		refresh:  refresh,
		throttle: throttle,
		reset:    reset,
		otp:      otp,
		jwt:      jwtMgr,
		mailer:   mailer,
		cfg:      cfg,
	}
}

// ListSessions trả về các thiết bị đang đăng nhập của một người.
func (u *Usecase) ListSessions(ctx context.Context, userID uuid.UUID) ([]domainauth.Session, error) {
	return u.sessions.ListOfUser(ctx, userID)
}

// Me lấy thông tin người dùng hiện tại.
//
// Đọc từ database chứ không lấy từ token: token cố ý chỉ mang id, vai trò
// và quyền — nhét thêm email, họ tên vào sẽ làm token phình ra và thông tin
// trong đó cũ đi ngay khi người dùng sửa hồ sơ.
func (u *Usecase) Me(ctx context.Context, userID uuid.UUID) (*domainhr.User, error) {
	user, err := u.users.FindByID(ctx, userID)
	if err != nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không tồn tại")
	}
	return user, nil
}
