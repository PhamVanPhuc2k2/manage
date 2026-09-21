package notification

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Repository interface {
	// CreateMany ghi thông báo cho nhiều người trong MỘT lượt.
	//
	// Một sự kiện thường có nhiều người nhận (cả nhóm chat, cả danh sách
	// được @mention). Ghi từng người một là N lượt round-trip cho một việc
	// mà một câu INSERT nhiều dòng làm xong.
	CreateMany(ctx context.Context, items []*Notification) error

	List(ctx context.Context, f Filter) ([]*Notification, error)
	CountUnread(ctx context.Context, employeeID uuid.UUID) (int, error)

	MarkRead(ctx context.Context, employeeID uuid.UUID, ids []uuid.UUID) (int64, error)
	MarkAllRead(ctx context.Context, employeeID uuid.UUID) (int64, error)

	// ListMuted trả về những loại người này đã tắt.
	ListMuted(ctx context.Context, employeeID uuid.UUID) ([]Type, error)
	SetMuted(ctx context.Context, employeeID uuid.UUID, t Type, muted bool) error

	// MutedByMany tra cứu cấu hình tắt của nhiều người trong một lượt, dùng
	// khi lọc danh sách người nhận trước lúc ghi.
	MutedByMany(ctx context.Context, employeeIDs []uuid.UUID, t Type) (
		map[uuid.UUID]bool, error)

	// PendingEmail liệt kê thông báo quan trọng chưa đọc, chưa gửi mail và
	// đã quá hạn chờ. Dùng cho job nhắc qua email.
	PendingEmail(ctx context.Context, olderThan time.Time, limit int) ([]*Notification, error)
	MarkEmailed(ctx context.Context, ids []uuid.UUID) error
}

// Pusher đẩy thông báo tới người đang online qua WebSocket.
//
// Khai báo ở ĐÂY, phía người dùng: module thông báo không được biết về hub
// hay RabbitMQ. Composition root nối hub vào.
type Pusher interface {
	// PushNotification gửi một thông báo tới danh sách người nhận.
	//
	// KHÔNG trả lỗi: đẩy realtime thất bại không được làm hỏng việc ghi
	// thông báo vào database. Người dùng vẫn thấy nó ở lần tải trang sau.
	PushNotification(ctx context.Context, recipients []uuid.UUID, payload any)

	// PushBadge gửi số chưa đọc mới để chuông cập nhật ngay.
	PushBadge(ctx context.Context, employeeID uuid.UUID, unread int)
}

// PresenceReader cho biết ai đang online.
//
// Job nhắc qua email cần nó để KHÔNG gửi mail cho người đang ngồi trước màn
// hình — họ đã thấy thông báo rồi, và một email thừa là một lý do để người
// ta lập bộ lọc bỏ qua mọi email từ hệ thống.
type PresenceReader interface {
	StatusOf(ctx context.Context, employeeIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// EmailLookup tra địa chỉ email của nhân viên.
type EmailLookup interface {
	EmailsOf(ctx context.Context, employeeIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// Mailer gửi email nhắc, qua hàng đợi worker.
type Mailer interface {
	SendNotification(ctx context.Context, email, name, title, body, link string) error
}
