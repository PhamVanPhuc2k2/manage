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
	// SessionID chỉ dùng trong nội bộ usecase (để ghi phiên thay thế vào
	// bản ghi refresh token). Tầng delivery không trả nó ra ngoài.
	SessionID uuid.UUID

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

	return u.issueSession(ctx, user, uuid.New(), in)
}

// issueSession tạo phiên MỚI với id cho trước rồi phát hành cặp token.
//
// id do người gọi truyền vào chứ không sinh ở đây: nhánh xoay vòng token phải
// đặt chỗ id đó trong Redis TRƯỚC khi tạo phiên (xem RefreshStore.Consume).
func (u *Usecase) issueSession(
	ctx context.Context,
	user *domainhr.User,
	sessionID uuid.UUID,
	in LoginInput,
) (*TokenPair, error) {
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

	pair, err := u.issueTokensFor(ctx, user, sessionID)
	if err != nil {
		// Mọi lỗi từ đây phải dọn phiên vừa tạo, nếu không Redis đọng lại
		// một phiên hợp lệ mà không ai cầm token của nó.
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, err
	}
	return pair, nil
}

// issueTokensFor phát hành access + refresh token cho một phiên ĐÃ tồn tại.
//
// Tách khỏi issueSession để nhánh ân hạn dùng lại được: nó cần cấp token mới
// cho đúng phiên cũ, không được tạo thêm phiên.
func (u *Usecase) issueTokensFor(
	ctx context.Context,
	user *domainhr.User,
	sessionID uuid.UUID,
) (*TokenPair, error) {
	authz, err := u.auth.Load(ctx, user.ID, user.EmployeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	accessToken, accessExp, err := u.jwt.Issue(jwt.Claims{
		UserID:      user.ID,
		EmployeeID:  user.EmployeeID,
		SessionID:   sessionID,
		Roles:       authz.Roles,
		Permissions: authz.Permissions,
		Scope:       string(authz.Scope),
	})
	if err != nil {
		return nil, apperror.Internal(err)
	}

	refreshToken, err := token.New()
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if err := u.refresh.Save(ctx, token.Hash(refreshToken), sessionID, user.ID, u.cfg.RefreshTTL); err != nil {
		return nil, apperror.Internal(err)
	}

	_ = u.users.UpdateLastLogin(ctx, user.ID)

	return &TokenPair{
		SessionID:          sessionID,
		AccessToken:        accessToken,
		AccessExpiresAt:    accessExp,
		RefreshToken:       refreshToken,
		RefreshExpiresAt:   time.Now().Add(u.cfg.RefreshTTL),
		MustChangePassword: user.MustChangePassword,
		User:               user,
	}, nil
}

// Refresh xoay vòng cặp token và phát hiện token bị đánh cắp.
func (u *Usecase) Refresh(ctx context.Context, refreshToken, ip, ua string) (*TokenPair, error) {
	log := logger.FromContext(ctx)

	// Sinh id phiên mới TRƯỚC khi tiêu token: Consume ghi id này vào bản ghi
	// token trong cùng một lệnh, nên tab thứ hai đọc ra được ngay.
	newSessionID := uuid.New()

	consumed, err := u.refresh.Consume(ctx, token.Hash(refreshToken), newSessionID)
	sessionID, userID := consumed.SessionID, consumed.UserID

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

	// --- DÙNG LẠI TRONG THỜI GIAN ÂN HẠN ---
	//
	// Gần như chắc chắn là hai tab cùng gọi refresh, hoặc client gửi lại vì
	// phản hồi lần trước rơi mất.
	//
	// Bám vào PHIÊN THAY THẾ mà lần refresh đầu đã tạo, không tạo phiên mới.
	// Cấp phiên mới ở đây gây ba chuyện, cả ba đều từng có thật:
	//
	//   1. Mỗi chu kỳ refresh với N tab đẻ ra N phiên mà chỉ xoá 1. Ba tab
	//      mở cả ngày là vài trăm phiên rác trong Redis, sống tới 7 ngày.
	//   2. Trang "thiết bị đang đăng nhập" đầy dòng trùng nhau, tên thiết bị
	//      rỗng. Bấm đăng xuất một dòng chỉ cắt được một tab.
	//   3. Nghiêm trọng nhất: đăng xuất bị vô hiệu. Đăng xuất xong, tab khác
	//      gửi lại token cũ trong vòng 10 giây là có phiên mới hợp lệ —
	//      phiên vừa xoá được hồi sinh dưới id khác.
	inGrace := errors.Is(err, domainauth.ErrTokenReusedInGrace)
	if inGrace {
		log.Info().
			Str("user_id", userID.String()).
			Msg("refresh token dùng lại trong thời gian ân hạn — nhiều tab hoặc gửi lại do mạng")

		if consumed.NextSessionID != uuid.Nil {
			return u.refreshInGrace(ctx, consumed.NextSessionID, userID)
		}
		// next_session rỗng chỉ xảy ra với token phát hành trước bản vá này
		// (bản ghi cũ trong Redis chưa có trường đó). Cấp phiên mới như cách
		// cũ — thà thừa một phiên còn hơn đá người dùng ra ngoài.
		log.Warn().
			Str("user_id", userID.String()).
			Msg("dùng lại trong ân hạn nhưng bản ghi token không có phiên thay thế")
	} else if err != nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã hết hạn.")
	}

	deviceName := ""
	if !inGrace {
		session, err := u.sessions.Get(ctx, sessionID)
		if err != nil || session == nil {
			return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã kết thúc.")
		}
		deviceName = session.DeviceName
		userID = session.UserID
	}

	user, err := u.users.FindByID(ctx, userID)
	if err != nil || user == nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}
	if err := user.CanLogin(); err != nil {
		_ = u.sessions.Delete(ctx, sessionID)
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}

	// Huỷ phiên cũ rồi tạo phiên mới — không tái dùng session_id.
	// Trong trường hợp ân hạn thì phiên cũ đã không còn, gọi Delete vô hại.
	_ = u.sessions.Delete(ctx, sessionID)

	// Dùng đúng id đã đặt chỗ ở Consume. Nhánh ân hạn đang chờ chính id này.
	return u.issueSession(ctx, user, newSessionID, LoginInput{
		IP:         ip,
		UserAgent:  ua,
		DeviceName: deviceName,
	})
}

// graceSessionWait là thời gian tối đa chờ phiên thay thế hiện ra trong Redis.
//
// Id phiên được đặt chỗ ngay lúc tiêu token, nhưng bản ghi phiên do luồng kia
// ghi vài mili giây sau đó. Hai tab F5 gần như cùng lúc nên khe này rất hay bị
// chạm phải. Chờ một nhịp ngắn rồi mới kết luận "phiên không còn".
const (
	graceSessionWait = 300 * time.Millisecond
	graceSessionStep = 20 * time.Millisecond
)

// refreshInGrace cấp cặp token mới cho phiên thay thế.
//
// Không tạo phiên, không xoá phiên. Nếu phiên đó không còn sau khi đã chờ —
// người dùng vừa đăng xuất, hoặc quản trị viên vừa cắt — thì đây là câu trả
// lời đúng: từ chối. Đăng xuất phải thắng thời gian ân hạn.
func (u *Usecase) refreshInGrace(
	ctx context.Context,
	sessionID, userID uuid.UUID,
) (*TokenPair, error) {
	session, err := u.waitForSession(ctx, sessionID)
	if err != nil || session == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Phiên đăng nhập đã kết thúc.")
	}

	user, err := u.users.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}
	if err := user.CanLogin(); err != nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Tài khoản không còn hiệu lực.")
	}

	return u.issueTokensFor(ctx, user, sessionID)
}

// waitForSession đọc phiên, thử lại trong graceSessionWait nếu chưa có.
//
// Chỉ dùng cho nhánh ân hạn, nơi ta BIẾT phiên sắp xuất hiện vì id của nó đã
// được đặt chỗ. Mọi chỗ khác đọc thẳng, không chờ.
func (u *Usecase) waitForSession(
	ctx context.Context,
	sessionID uuid.UUID,
) (*domainauth.Session, error) {
	deadline := time.Now().Add(graceSessionWait)
	for {
		session, err := u.sessions.Get(ctx, sessionID)
		if err == nil && session != nil {
			return session, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(graceSessionStep):
		}
	}
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
