package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

// startOTPChallenge sinh mã, lưu thử thách và gửi mail.
//
// Trả về id thử thách — một chuỗi ngẫu nhiên 32 byte. Nó KHÔNG phải là thứ
// chứng minh danh tính: người cầm id vẫn phải có mã trong hộp thư mới đi
// tiếp được. Nhưng nó đủ ngẫu nhiên để không ai đoán ra thử thách của người
// khác, và trong Redis nó được lưu dưới dạng băm hệt như refresh token.
func (u *Usecase) startOTPChallenge(
	ctx context.Context,
	user *domainhr.User,
	in LoginInput,
) (*OTPChallengeInfo, error) {
	log := logger.FromContext(ctx)

	challengeID, err := token.New()
	if err != nil {
		return nil, apperror.Internal(err)
	}
	code, err := token.NumericCode(domainauth.OTPDigits)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	challenge := domainauth.OTPChallenge{
		UserID: user.ID,
		Email:  user.Email,
		// Giữ lại bối cảnh của BƯỚC MỘT. Phiên cấp ra ở bước hai phải mang
		// IP và tên thiết bị của lúc nhập mật khẩu, nếu không danh sách
		// thiết bị sẽ ghi sai và cảnh báo đăng nhập lạ thành vô dụng.
		IP:         in.IP,
		UserAgent:  in.UserAgent,
		DeviceName: in.DeviceName,
	}

	if err := u.otp.Create(
		ctx, token.Hash(challengeID), challenge, token.Hash(code), domainauth.OTPTTL,
	); err != nil {
		return nil, apperror.Internal(err)
	}

	if err := u.sendOTP(ctx, user.Email, user.EmployeeName, code, in.IP); err != nil {
		// Không gửi được mail thì thử thách vô dụng — dọn đi ngay thay vì
		// để người dùng ngồi chờ một mã không bao giờ tới.
		_ = u.otp.Delete(ctx, token.Hash(challengeID))
		log.Error().Err(err).Msg("không gửi được mã đăng nhập")
		return nil, apperror.New(apperror.KindInternal,
			"Không gửi được mã xác minh. Vui lòng thử lại hoặc liên hệ bộ phận kỹ thuật.")
	}

	log.Info().
		Str("user_id", user.ID.String()).
		Str("ip", in.IP).
		Msg("đã gửi mã đăng nhập, chờ xác minh")

	return &OTPChallengeInfo{
		ChallengeID: challengeID,
		ExpiresAt:   time.Now().Add(domainauth.OTPTTL),
		ResendAfter: domainauth.OTPResendCooldown,
		MaskedEmail: maskEmail(user.Email),
	}, nil
}

// VerifyOTP hoàn tất đăng nhập bằng mã nhận qua email.
func (u *Usecase) VerifyOTP(
	ctx context.Context,
	challengeID, code, ip, ua string,
) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	code = strings.TrimSpace(code)
	if code == "" {
		return nil, apperror.Invalid("Vui lòng nhập mã xác minh", nil)
	}

	verified, attemptsLeft, err := u.otp.Verify(
		ctx, token.Hash(challengeID), token.Hash(code), domainauth.OTPMaxAttempts)
	switch {
	case errors.Is(err, domainauth.ErrOTPWrongCode):
		return nil, apperror.New(apperror.KindUnauthorized,
			fmt.Sprintf("Mã xác minh không đúng. Bạn còn %d lần thử.", attemptsLeft))

	case errors.Is(err, domainauth.ErrOTPTooManyAttempts):
		log.Warn().Str("ip", ip).Msg("nhập sai mã đăng nhập quá số lần cho phép")
		return nil, apperror.New(apperror.KindUnauthorized,
			"Bạn đã nhập sai mã quá nhiều lần. Vui lòng đăng nhập lại từ đầu.")

	case errors.Is(err, domainauth.ErrOTPNotFound):
		return nil, apperror.New(apperror.KindUnauthorized,
			"Mã xác minh đã hết hạn. Vui lòng đăng nhập lại từ đầu.")

	case err != nil:
		return nil, apperror.Internal(err)
	}

	c := verified.Challenge

	// Đọc lại tài khoản từ database, KHÔNG tin bản chụp lúc bước một.
	//
	// Giữa hai bước có thể tới 5 phút. Trong 5 phút đó nhân viên có thể đã
	// bị cho nghỉ hoặc bị khoá tài khoản — cấp phiên dựa trên dữ liệu cũ là
	// mở cửa cho người vừa bị đuổi.
	user, err := u.users.FindByID(ctx, c.UserID)
	if err != nil || user == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}
	if err := user.CanLogin(); err != nil {
		return nil, apperror.New(apperror.KindForbidden,
			err.Error()+". Vui lòng liên hệ bộ phận nhân sự.")
	}

	return u.issueSession(ctx, user, uuid.New(), LoginInput{
		IP:         c.IP,
		UserAgent:  c.UserAgent,
		DeviceName: c.DeviceName,
	})
}

// ResendOTP gửi lại mã mới cho thử thách đang có.
func (u *Usecase) ResendOTP(ctx context.Context, challengeID string) (*OTPChallengeInfo, error) {
	log := logger.FromContext(ctx)

	newCode, err := token.NumericCode(domainauth.OTPDigits)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	challenge, wait, err := u.otp.Resend(
		ctx, token.Hash(challengeID), token.Hash(newCode),
		domainauth.OTPResendCooldown, domainauth.OTPMaxResends, time.Now())
	switch {
	case errors.Is(err, domainauth.ErrOTPResendTooSoon):
		return nil, apperror.New(apperror.KindRateLimited,
			"Vui lòng chờ "+formatWait(wait)+" trước khi gửi lại mã.")

	case errors.Is(err, domainauth.ErrOTPTooManyResends):
		return nil, apperror.New(apperror.KindRateLimited,
			"Đã gửi lại mã quá nhiều lần. Vui lòng đăng nhập lại từ đầu.")

	case errors.Is(err, domainauth.ErrOTPNotFound):
		return nil, apperror.New(apperror.KindUnauthorized,
			"Phiên xác minh đã hết hạn. Vui lòng đăng nhập lại từ đầu.")

	case err != nil:
		return nil, apperror.Internal(err)
	}

	// Tên người nhận không được lưu trong thử thách — tra lại từ database.
	// Lỗi ở đây không chặn việc gửi mã: thiếu tên chỉ làm mail kém thân
	// thiện, còn không gửi mã là người dùng kẹt luôn.
	name := ""
	if user, uErr := u.users.FindByID(ctx, challenge.UserID); uErr == nil && user != nil {
		name = user.EmployeeName
	}

	if err := u.sendOTP(ctx, challenge.Email, name, newCode, challenge.IP); err != nil {
		log.Error().Err(err).Msg("không gửi lại được mã đăng nhập")
		return nil, apperror.New(apperror.KindInternal, "Không gửi được mã xác minh.")
	}

	return &OTPChallengeInfo{
		// Id thử thách KHÔNG đổi: client đang giữ id cũ, đổi id nghĩa là
		// nó phải nhớ cập nhật, và quên một chỗ là hỏng cả luồng.
		ChallengeID: challengeID,
		ExpiresAt:   time.Now().Add(domainauth.OTPTTL),
		ResendAfter: domainauth.OTPResendCooldown,
		MaskedEmail: maskEmail(challenge.Email),
	}, nil
}

func (u *Usecase) sendOTP(ctx context.Context, email, name, code, ip string) error {
	if u.mailer == nil {
		return errors.New("chưa cấu hình mailer")
	}
	return u.mailer.SendLoginOTP(
		ctx, email, name, code, int(domainauth.OTPTTL.Minutes()), ip)
}

// maskEmail che bớt email để màn hình nhập mã nhắc được người dùng mã đi
// đâu mà không phơi địa chỉ đầy đủ ra màn hình.
//
// "nguyenvana@abc.vn" → "ng********@abc.vn"
func maskEmail(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 {
		return email
	}
	local, domain := email[:at], email[at:]
	if len(local) <= 2 {
		return strings.Repeat("*", len(local)) + domain
	}
	return local[:2] + strings.Repeat("*", len(local)-2) + domain
}
