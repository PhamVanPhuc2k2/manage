package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SessionStore lưu phiên đăng nhập. Hiện thực bằng Redis.
//
// Phiên nằm ở Redis chứ không phải PostgreSQL vì nó được đọc ở MỌI request —
// đây là thứ duy nhất biến JWT từ "không thu hồi được" thành "thu hồi tức thì".
type SessionStore interface {
	Create(ctx context.Context, s Session, ttl time.Duration) error
	Get(ctx context.Context, sessionID uuid.UUID) (*Session, error)
	Touch(ctx context.Context, sessionID uuid.UUID) error
	Delete(ctx context.Context, sessionID uuid.UUID) error
	DeleteAllOfUser(ctx context.Context, userID uuid.UUID) error
	ListOfUser(ctx context.Context, userID uuid.UUID) ([]Session, error)
}

// RefreshConsumed là kết quả của một lần tiêu refresh token.
type RefreshConsumed struct {
	// SessionID là phiên gắn với token lúc phát hành.
	SessionID uuid.UUID

	// UserID là chủ nhân của token.
	UserID uuid.UUID

	// NextSessionID là phiên mà lần refresh THÀNH CÔNG trước đó đã đặt chỗ.
	// Chỉ có giá trị khi trả về ErrTokenReusedInGrace.
	NextSessionID uuid.UUID
}

type RefreshStore interface {
	Save(ctx context.Context, tokenHash string, sessionID, userID uuid.UUID, ttl time.Duration) error

	// Consume đánh dấu token đã dùng và trả về session và user gắn với nó.
	//
	// Nếu token đã được tiêu trước đó, trả về ErrTokenReused KÈM userID.
	//
	// Vì sao phải lưu cả userID chứ không chỉ sessionID? Vì mỗi lần refresh
	// thành công, phiên cũ bị xoá và phiên mới được tạo. Khi phát hiện tái
	// sử dụng, phiên gắn với token cũ đã không còn — nếu chỉ có sessionID
	// thì không tra ra được chủ nhân, và bước huỷ toàn bộ phiên sẽ bị bỏ
	// qua âm thầm. Cơ chế chống đánh cắp khi đó trở nên vô dụng.
	//
	// reserveSessionID là id mà người gọi SẼ dùng cho phiên mới. Nó được ghi
	// vào bản ghi token trong CÙNG lệnh đánh dấu đã dùng, nên lần dùng lại
	// trong thời gian ân hạn đọc ra ngay và bám được vào đúng phiên đó thay
	// vì đẻ thêm phiên mới.
	//
	// Phải đặt chỗ trong cùng một lệnh, không ghi bổ sung sau khi tạo phiên:
	// hai tab F5 gần như cùng một thời điểm, lần ghi bổ sung luôn đến sau
	// lần đọc của tab kia.
	Consume(ctx context.Context, tokenHash string, reserveSessionID uuid.UUID) (RefreshConsumed, error)
}

// OTPChallenge là thứ được giữ lại giữa hai bước đăng nhập.
//
// Nó lưu bối cảnh của lần đăng nhập đang dở: ai đang đăng nhập, từ đâu. Có
// nó thì phiên cấp ra ở bước hai mang đúng IP và tên thiết bị của bước một,
// chứ không phải của request xác minh.
type OTPChallenge struct {
	UserID     uuid.UUID
	Email      string
	IP         string
	UserAgent  string
	DeviceName string
}

// OTPVerified là kết quả xác minh đúng mã.
type OTPVerified struct {
	Challenge OTPChallenge
}

// OTPStore lưu thử thách OTP giữa bước nhập mật khẩu và bước nhập mã.
//
// Nằm ở Redis vì nó đúng nghĩa là dữ liệu tạm: sống 5 phút rồi tự biến mất,
// không cần sao lưu, không ai truy vấn lịch sử. Đưa vào PostgreSQL thì phải
// tự viết job dọn rác cho một thứ mà Redis dọn miễn phí.
type OTPStore interface {
	// Create lưu thử thách kèm mã ĐÃ BĂM.
	//
	// challengeHash là băm của id thử thách — id thật chỉ nằm ở client, hệt
	// như refresh token. Ai đọc được Redis cũng không mạo danh được phiên
	// đăng nhập đang dở.
	Create(ctx context.Context, challengeHash string, c OTPChallenge, codeHash string, ttl time.Duration) error

	// Verify so mã và đếm số lần sai trong MỘT thao tác nguyên khối.
	//
	// Phải nguyên khối: đọc rồi so ở Go thì nhiều request song song cùng đọc
	// được một giá trị attempts, và bộ đếm 5 lần trở thành vô nghĩa — đúng
	// cái mà OTP 6 chữ số dựa vào để an toàn.
	//
	// Trả về attemptsLeft khi mã sai, để giao diện nói rõ còn mấy lần.
	Verify(ctx context.Context, challengeHash, codeHash string, maxAttempts int) (OTPVerified, int, error)

	// Resend thay mã mới vào thử thách đang có, kèm chặn gửi lại quá dày.
	//
	// Thay mã chứ không tạo thử thách mới: giữ nguyên bộ đếm số lần thử,
	// nếu không kẻ tấn công cứ bấm gửi lại là bộ đếm về 0.
	//
	// Khi trả ErrOTPResendTooSoon, tham số thứ hai là thời gian còn phải chờ.
	Resend(ctx context.Context, challengeHash, newCodeHash string, cooldown time.Duration, maxResends int, now time.Time) (OTPChallenge, time.Duration, error)

	// Delete xoá thử thách (dùng khi huỷ giữa chừng).
	Delete(ctx context.Context, challengeHash string) error
}

// LoginThrottle chặn dò mật khẩu. Đếm theo cả IP lẫn tài khoản:
// chỉ đếm theo IP thì kẻ tấn công đổi IP là thoát; chỉ đếm theo tài khoản
// thì ai cũng khoá được tài khoản người khác bằng cách cố tình nhập sai.
type LoginThrottle interface {
	// Check trả về thời gian còn phải chờ. Bằng 0 nghĩa là được phép thử.
	Check(ctx context.Context, email, ip string) (time.Duration, error)
	RecordFailure(ctx context.Context, email, ip string) error
	Reset(ctx context.Context, email, ip string) error
}

// PasswordResetStore lưu token đặt lại mật khẩu, dùng một lần.
type PasswordResetStore interface {
	Save(ctx context.Context, tokenHash string, userID uuid.UUID, ttl time.Duration) error
	// Consume trả về userID và XOÁ token ngay. Gọi hai lần thì lần sau lỗi.
	Consume(ctx context.Context, tokenHash string) (uuid.UUID, error)
}

// AuthorizationReader tính quyền của một người dùng từ database.
type AuthorizationReader interface {
	Load(ctx context.Context, userID, employeeID uuid.UUID) (*Authorization, error)
}
