package realtime

import (
	"context"

	"github.com/google/uuid"
)

// PresenceStore giữ trạng thái hiện diện, dùng chung cho mọi instance api.
//
// Phải là bộ nhớ NGOÀI tiến trình (Redis): chạy ba replica api thì mỗi
// replica chỉ thấy những kết nối của riêng nó, và câu hỏi "ai đang online"
// sẽ có ba câu trả lời khác nhau.
type PresenceStore interface {
	// Touch ghi nhận một kết nối còn sống, làm mới TTL.
	// Gọi lúc bắt tay và mỗi lần nhận heartbeat.
	Touch(ctx context.Context, employeeID uuid.UUID, conn *Connection) error

	// Remove gỡ một kết nối khi nó đóng.
	Remove(ctx context.Context, employeeID, connID uuid.UUID) error

	// Get trả về presence của một người. Không bao giờ trả nil — người
	// không online cho ra Presence rỗng, Online() = false.
	Get(ctx context.Context, employeeID uuid.UUID) (*Presence, error)

	// GetMany tra nhiều người trong một lượt, phục vụ danh sách.
	GetMany(ctx context.Context, employeeIDs []uuid.UUID) (map[uuid.UUID]*Presence, error)

	// ListOnline liệt kê mọi người đang có kết nối.
	// Dùng cho job chấm công quét định kỳ và cho màn hình quản lý.
	ListOnline(ctx context.Context) ([]*Presence, error)
}

// Broadcaster phát một bản tin tới MỌI instance api.
//
// Vì sao phải đi qua hàng đợi thay vì gửi thẳng: người nhận có thể đang nối
// vào một instance khác với instance đang xử lý request. Giữ trạng thái chỉ
// trong bộ nhớ một tiến trình là kiểu thiết kế chạy được với một replica rồi
// hỏng lặng lẽ ngay khi scale lên hai.
type Broadcaster interface {
	Broadcast(ctx context.Context, msg Message) error
}

// Dispatcher là phía NHẬN của Broadcaster: mỗi instance đăng ký một hàm để
// giao bản tin cho những client đang nối vào chính nó.
type Dispatcher interface {
	// Subscribe chạy tới khi ctx bị huỷ, gọi handle cho mỗi bản tin nhận được.
	Subscribe(ctx context.Context, handle func(Message)) error
}
