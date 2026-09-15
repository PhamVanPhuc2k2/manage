package auth

import (
	"context"
	"net/url"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/token"
)

// ValidatePassword kiểm tra độ mạnh tối thiểu.
//
// Cố ý KHÔNG bắt buộc ký tự đặc biệt. Nghiên cứu về mật khẩu (và hướng dẫn
// NIST 800-63B) cho thấy quy tắc phức tạp khiến người dùng chọn kiểu
// "Password1!" — dễ đoán hơn một cụm từ dài. Độ dài quan trọng hơn.
func ValidatePassword(p string) error {
	if len([]rune(p)) < 10 {
		return apperror.Invalid("Mật khẩu phải có ít nhất 10 ký tự", nil)
	}
	if len(p) > 200 {
		// bcrypt chỉ dùng 72 byte đầu; chặn ở đây để tránh hiểu nhầm
		// rằng phần đuôi có tác dụng.
		return apperror.Invalid("Mật khẩu quá dài (tối đa 200 ký tự)", nil)
	}

	var hasLetter, hasDigit bool
	for _, r := range p {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return apperror.Invalid("Mật khẩu phải có cả chữ và số", nil)
	}
	return nil
}

// ChangePassword đổi mật khẩu khi người dùng đang đăng nhập.
func (u *Usecase) ChangePassword(
	ctx context.Context,
	userID, currentSessionID uuid.UUID,
	oldPassword, newPassword string,
) error {
	user, err := u.users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return apperror.New(apperror.KindUnauthorized, "Tài khoản không tồn tại")
	}

	if err := hash.Verify(user.PasswordHash, oldPassword); err != nil {
		return apperror.New(apperror.KindInvalid, "Mật khẩu hiện tại không đúng")
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if oldPassword == newPassword {
		return apperror.Invalid("Mật khẩu mới phải khác mật khẩu cũ", nil)
	}

	newHash, err := hash.Password(newPassword)
	if err != nil {
		return apperror.Internal(err)
	}
	if err := u.users.UpdatePasswordHash(ctx, userID, newHash); err != nil {
		return apperror.Internal(err)
	}

	// Cắt mọi phiên KHÁC, giữ lại phiên hiện tại để người dùng không bị
	// đá ra ngay sau khi tự đổi mật khẩu.
	//
	// Người ta thường đổi mật khẩu vì nghi bị lộ — không cắt phiên cũ thì
	// kẻ tấn công vẫn đang đăng nhập.
	sessions, err := u.sessions.ListOfUser(ctx, userID)
	if err == nil {
		for _, s := range sessions {
			if s.ID != currentSessionID {
				_ = u.sessions.Delete(ctx, s.ID)
			}
		}
	}
	return nil
}

// ForgotPassword sinh token đặt lại mật khẩu và gửi mail.
//
// LUÔN trả về nil dù email có tồn tại hay không. Trả lỗi khi email không
// tồn tại là biến endpoint này thành công cụ dò danh sách email nhân viên.
func (u *Usecase) ForgotPassword(ctx context.Context, email string) error {
	log := logger.FromContext(ctx)

	user, err := u.users.FindByEmail(ctx, email)
	if err != nil || user == nil {
		log.Info().Str("email", email).Msg("yêu cầu đặt lại mật khẩu cho email không tồn tại")
		// Nuốt lỗi CÓ CHỦ Ý — xem ghi chú ở đầu hàm. Trả lỗi ra ngoài là
		// biến endpoint này thành công cụ dò danh sách email nhân viên.
		//nolint:nilerr // im lặng có chủ ý để chống dò email
		return nil
	}
	if err := user.CanLogin(); err != nil {
		log.Info().Str("email", email).Msg("yêu cầu đặt lại mật khẩu cho tài khoản không hoạt động")
		//nolint:nilerr // im lặng có chủ ý để chống dò email
		return nil
	}

	raw, err := token.New()
	if err != nil {
		return apperror.Internal(err)
	}
	if err := u.reset.Save(ctx, token.Hash(raw), user.ID, u.cfg.ResetTokenTTL); err != nil {
		return apperror.Internal(err)
	}

	resetURL := strings.TrimRight(u.cfg.PublicBaseURL, "/") +
		"/reset-password?token=" + url.QueryEscape(raw)

	if u.mailer != nil {
		if err := u.mailer.SendPasswordReset(ctx, user.Email, user.EmployeeName, resetURL); err != nil {
			log.Error().Err(err).Msg("không gửi được mail đặt lại mật khẩu")
			return apperror.Internal(err)
		}
	}
	return nil
}

// ResetPassword đặt lại mật khẩu bằng token nhận qua mail.
func (u *Usecase) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Consume xoá token ngay trong một thao tác nguyên khối (GETDEL), nên
	// hai request đến cùng lúc chỉ có một request dùng được.
	userID, err := u.reset.Consume(ctx, token.Hash(rawToken))
	if err != nil {
		return apperror.New(apperror.KindInvalid,
			"Liên kết đặt lại mật khẩu không hợp lệ hoặc đã hết hạn")
	}

	newHash, err := hash.Password(newPassword)
	if err != nil {
		return apperror.Internal(err)
	}
	if err := u.users.UpdatePasswordHash(ctx, userID, newHash); err != nil {
		return apperror.Internal(err)
	}

	// Cắt TOÀN BỘ phiên. Khác với ChangePassword, ở đây người dùng chưa
	// đăng nhập nên không có phiên nào cần giữ lại.
	if err := u.sessions.DeleteAllOfUser(ctx, userID); err != nil {
		// Gán ra biến trước: zerolog.Logger có method pointer receiver nên
		// không gọi trực tiếp trên giá trị trả về từ hàm được.
		log := logger.FromContext(ctx)
		log.Error().Err(err).Msg("không huỷ được phiên sau khi đặt lại mật khẩu")
	}
	return nil
}
