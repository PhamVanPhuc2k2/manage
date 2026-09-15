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
	Consume(ctx context.Context, tokenHash string) (sessionID, userID uuid.UUID, err error)
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
