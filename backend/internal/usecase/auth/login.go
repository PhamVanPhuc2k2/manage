package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

type LoginInput struct {
	Email      string
	Password   string
	IP         string
	UserAgent  string
	DeviceName string
}

type TokenPair struct {
	AccessToken        string
	AccessExpiresAt    time.Time
	RefreshToken       string
	RefreshExpiresAt   time.Time
	MustChangePassword bool
	User               *domainhr.User
}

// errInvalidCredentials trả về MỘT thông báo duy nhất cho mọi trường hợp sai.
//
// Phân biệt "email không tồn tại" với "mật khẩu sai" sẽ biến endpoint đăng
// nhập thành công cụ dò danh sách email nhân viên của công ty.
func errInvalidCredentials() error {
	return apperror.New(apperror.KindUnauthorized, "Email hoặc mật khẩu không đúng")
}

func (u *Usecase) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	// --- 1. Chặn dò mật khẩu TRƯỚC KHI chạm database ---
	wait, err := u.throttle.Check(ctx, in.Email, in.IP)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if wait > 0 {
		return nil, apperror.New(apperror.KindRateLimited,
			"Đăng nhập sai quá nhiều lần. Vui lòng thử lại sau "+formatWait(wait))
	}

	// --- 2. Tra tài khoản ---
	user, err := u.users.FindByEmail(ctx, in.Email)
	if err != nil || user == nil {
		// Băm một mật khẩu giả để thời gian phản hồi không khác biệt.
		// Thiếu bước này, thời gian trả lời nhanh bất thường sẽ tiết lộ
		// rằng email không tồn tại trong hệ thống.
		hash.DummyVerify(in.Password)
		_ = u.throttle.RecordFailure(ctx, in.Email, in.IP)
		return nil, errInvalidCredentials()
	}

	// --- 3. Kiểm tra mật khẩu ---
	if err := hash.Verify(user.PasswordHash, in.Password); err != nil {
		_ = u.throttle.RecordFailure(ctx, in.Email, in.IP)
		_ = u.users.IncrementFailedAttempts(ctx, user.ID)

		log.Warn().Str("email", in.Email).Str("ip", in.IP).Msg("đăng nhập thất bại")
		return nil, errInvalidCredentials()
	}

	// --- 4. Tài khoản có còn dùng được không ---
	//
	// Kiểm tra SAU khi xác minh mật khẩu, không phải trước. Kiểm tra trước
	// sẽ để lộ trạng thái tài khoản cho người không biết mật khẩu.
	if err := user.CanLogin(); err != nil {
		return nil, apperror.New(apperror.KindForbidden,
			err.Error()+". Vui lòng liên hệ bộ phận nhân sự.")
	}

	// --- 5. Thành công ---
	_ = u.throttle.Reset(ctx, in.Email, in.IP)
	_ = u.users.ResetFailedAttempts(ctx, user.ID)

	// Nâng cost bcrypt âm thầm nếu hash cũ yếu hơn hiện tại.
	// Nhờ vậy nâng cost sau này không cần bắt ai đổi mật khẩu.
	if hash.NeedsRehash(user.PasswordHash) {
		if newHash, hErr := hash.Password(in.Password); hErr == nil {
			_ = u.users.UpdatePasswordHash(ctx, user.ID, newHash)
			// UpdatePasswordHash đặt luôn must_change_password = FALSE,
			// nên phải khôi phục lại cờ cũ.
			_ = u.users.SetMustChangePassword(ctx, user.ID, user.MustChangePassword)
		}
	}

	return u.issueSession(ctx, user, in)
}

// issueSession tạo phiên mới và phát hành cặp token.
// Dùng chung cho Login và Refresh.
func (u *Usecase) issueSession(
	ctx context.Context,
	user *domainhr.User,
	in LoginInput,
) (*TokenPair, error) {
	authz, err := u.auth.Load(ctx, user.ID, user.EmployeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	sessionID := uuid.New()
	now := time.Now()

	// --- Lưu phiên vào Redis TRƯỚC khi phát hành token ---
	//
	// Thứ tự này quan trọng. Phát hành token trước rồi mới lưu phiên, mà bước
	// lưu lỗi, thì đã có một token hợp lệ ngoài kia mà server không biết gì
	// về nó — và không thu hồi được.
	session := domainauth.Session{
		ID:         sessionID,
		UserID:     user.ID,
		DeviceName: in.DeviceName,
		IP:         in.IP,
		UserAgent:  in.UserAgent,
		CreatedAt:  now,
		LastSeenAt: now,
	}
	if err := u.sessions.Create(ctx, session, u.cfg.RefreshTTL); err != nil {
		return nil, apperror.Internal(err)
	}

	// Từ đây trở đi, mọi lỗi phải dọn phiên vừa tạo.
	cleanup := func() { _ = u.sessions.Delete(ctx, sessionID) }

	accessToken, accessExp, err := u.jwt.Issue(jwt.Claims{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		SessionID:   sessionID,
		Roles:       authz.Roles,
		Permissions: authz.Permissions,
		Scope:       string(authz.Scope),
	})
	if err != nil {
		cleanup()
		return nil, apperror.Internal(err)
	}

	refreshToken, err := token.New()
	if err != nil {
		cleanup()
		return nil, apperror.Internal(err)
	}

	if err := u.refresh.Save(ctx, token.Hash(refreshToken), sessionID, user.ID, u.cfg.RefreshTTL); err != nil {
		cleanup()
		return nil, apperror.Internal(err)
	}

	_ = u.users.UpdateLastLogin(ctx, user.ID)

	return &TokenPair{
		AccessToken:        accessToken,
		AccessExpiresAt:    accessExp,
		RefreshToken:       refreshToken,
		RefreshExpiresAt:   now.Add(u.cfg.RefreshTTL),
		MustChangePassword: user.MustChangePassword,
		User:               user,
	}, nil
}

// Refresh xoay vòng cặp token và phát hiện token bị đánh cắp.
func (u *Usecase) Refresh(ctx context.Context, refreshToken, ip, ua string) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	sessionID, userID, err := u.refresh.Consume(ctx, token.Hash(refreshToken))

	// --- PHÁT HIỆN ĐÁNH CẮP ---
	//
	// Refresh token dùng một lần. Nếu một token đã tiêu lại xuất hiện lần
	// nữa, chỉ có thể là ai đó đã sao chép nó. Chưa biết ai là người thật,
	// nên cắt hết và bắt mọi thiết bị đăng nhập lại. Bất tiện một lần,
	// nhưng chặn được kẻ trộm.
	//
	// Dùng userID lấy thẳng từ bản ghi token, KHÔNG tra ngược từ phiên:
	// phiên gắn với token cũ đã bị xoá ở lần refresh trước, tra sẽ không ra
	// và bước huỷ sẽ bị bỏ qua âm thầm.
	if errors.Is(err, domainauth.ErrTokenReused) {
		log.Error().
			Str("user_id", userID.String()).
			Str("ip", ip).
			Msg("PHÁT HIỆN REFRESH TOKEN BỊ TÁI SỬ DỤNG — huỷ toàn bộ phiên")

		_ = u.sessions.DeleteAllOfUser(ctx, userID)
		u.notifySuspicious(ctx, userID, ip, ua)

		return nil, apperror.New(apperror.KindUnauthorized,
			"Phiên đăng nhập không hợp lệ. Vui lòng đăng nhập lại.")
	}

	if err != nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã hết hạn.")
	}

	session, err := u.sessions.Get(ctx, sessionID)
	if err != nil || session == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã kết thúc.")
	}

	user, err := u.users.FindByID(ctx, session.UserID)
	if err != nil || user == nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}
	if err := user.CanLogin(); err != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}

	// Huỷ phiên cũ rồi tạo phiên mới — không tái dùng session_id.
	_ = u.sessions.Delete(ctx, sessionID)

	return u.issueSession(ctx, user, LoginInput{
		IP:         ip,
		UserAgent:  ua,
		DeviceName: session.DeviceName,
	})
}

func (u *Usecase) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return u.sessions.Delete(ctx, sessionID)
}

func (u *Usecase) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	return u.sessions.DeleteAllOfUser(ctx, userID)
}

func (u *Usecase) notifySuspicious(ctx context.Context, userID uuid.UUID, ip, ua string) {
	if u.mailer == nil {
		return
	}
	user, err := u.users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return
	}
	_ = u.mailer.SendSuspiciousActivity(ctx, user.Email, user.EmployeeName, ip, ua)
}

func formatWait(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d giây", int(d.Seconds())+1)
	}
	return fmt.Sprintf("%d phút", int(d.Minutes())+1)
}
