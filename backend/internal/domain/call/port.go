package call

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// =========================================================================
// LƯU TRỮ
// =========================================================================

type Repository interface {
	Create(ctx context.Context, c *Call) error
	GetByID(ctx context.Context, id uuid.UUID) (*Call, error)

	// LiveInConversation trả về cuộc gọi đang diễn ra của một hội thoại.
	//
	// Trả ErrNotFound khi không có. Dùng để chặn hai cuộc gọi song song và
	// để người vào muộn tìm được phòng đang mở thay vì tạo phòng mới.
	LiveInConversation(ctx context.Context, conversationID uuid.UUID) (*Call, error)

	// LiveForEmployee trả về cuộc gọi mà người này đang tham gia.
	//
	// Đây là phép kiểm "đang bận": có kết quả nghĩa là từ chối lời mời mới
	// ngay lập tức thay vì đổ chuông.
	LiveForEmployee(ctx context.Context, employeeID uuid.UUID) (*Call, error)

	// UpdateStatus đổi trạng thái và ghi lý do kết thúc.
	//
	// Nhận cả trạng thái MONG ĐỢI hiện tại để tránh ghi đè lẫn nhau: hai
	// người cùng bấm cúp máy sẽ có một lời gọi thắng, và lời gọi thua phải
	// biết mình đã thua chứ không âm thầm ghi đè lý do kết thúc.
	UpdateStatus(ctx context.Context, id uuid.UUID, from, to Status, reason string) error

	// SetRelayRatio ghi tỉ lệ relay sau khi cuộc gọi kết thúc.
	SetRelayRatio(ctx context.Context, id uuid.UUID, ratio float64) error

	// ListForConversation trả về lịch sử cuộc gọi, mới nhất trước.
	ListForConversation(ctx context.Context, conversationID uuid.UUID, limit int) ([]*Call, error)

	// ExpireRinging đánh dấu 'missed' cho mọi cuộc gọi đổ chuông quá hạn.
	//
	// Chạy định kỳ ở worker. Cần thiết dù hub đã có hẹn giờ riêng: hẹn giờ
	// sống trong bộ nhớ một instance, và instance đó khởi động lại giữa
	// chừng thì cuộc gọi treo ở 'ringing' vĩnh viễn.
	ExpireRinging(ctx context.Context, olderThan time.Time) ([]*Call, error)
}

type ParticipantRepository interface {
	// Invite ghi những người được mời, chưa vào phòng (joined_at = NULL).
	//
	// Ghi TRƯỚC khi đổ chuông để lời mời còn dấu vết kể cả khi không ai bắt
	// máy — một cuộc gọi nhỡ vẫn phải hiện ra với đúng những người đã bị
	// gọi.
	Invite(ctx context.Context, callID uuid.UUID, employeeIDs []uuid.UUID) error

	// Join đánh dấu một người đã vào phòng. Chạy lại được: vào lại sau khi
	// rớt mạng chỉ xoá left_at chứ không tạo dòng mới.
	Join(ctx context.Context, callID, employeeID uuid.UUID, at time.Time) error

	// Leave đánh dấu một người đã rời phòng.
	Leave(ctx context.Context, callID, employeeID uuid.UUID, at time.Time) error

	// SetTracks ghi nhận người này đã từng bật mic, camera hay màn hình.
	//
	// Chỉ BẬT cờ, không bao giờ tắt: đây là dấu vết phục vụ audit, và câu
	// hỏi cần trả lời là "trong cuộc họp đó người này có chiếu màn hình
	// không", chứ không phải "lúc 10h03 có đang chiếu không".
	SetTracks(ctx context.Context, callID, employeeID uuid.UUID, audio, video, screen bool) error

	SetRelay(ctx context.Context, callID, employeeID uuid.UUID, relay bool) error

	ListForCall(ctx context.Context, callID uuid.UUID) ([]*Participant, error)

	// CountInRoom đếm người đang trong phòng, để biết khi nào phòng rỗng.
	CountInRoom(ctx context.Context, callID uuid.UUID) (int, error)
}

// =========================================================================
// CỔNG SANG MODULE KHÁC
// =========================================================================

// ConversationLookup khai báo ĐÚNG những gì module gọi cần từ module chat.
//
// Khai báo ở ĐÂY, phía người dùng. Module call không được import
// usecase/chat: hai tầng nghiệp vụ import chéo nhau là đường nhanh nhất tới
// phụ thuộc vòng.
type ConversationLookup interface {
	// IsMember kiểm tra một người có trong hội thoại không.
	//
	// Đây là hàng rào phân quyền DUY NHẤT của module gọi: middleware chỉ
	// biết "người này có quyền call:start", nó không biết họ định gọi vào
	// hội thoại nào.
	IsMember(ctx context.Context, conversationID, employeeID uuid.UUID) (bool, error)

	// MemberIDs trả về mọi thành viên, để biết đổ chuông cho ai.
	MemberIDs(ctx context.Context, conversationID uuid.UUID) ([]uuid.UUID, error)
}

// SystemMessenger ghi một tin nhắn hệ thống vào hội thoại.
//
// Dùng lại bảng messages thay vì dựng một dòng thời gian riêng cho cuộc
// gọi: người dùng đọc lịch sử trò chuyện theo thứ tự thời gian, và tách ra
// hai nơi buộc họ phải ghép lại trong đầu.
type SystemMessenger interface {
	PostSystem(ctx context.Context, conversationID uuid.UUID, content string) error
}

// EmployeeLookup tra tên người, để dựng câu thông báo đổ chuông.
type EmployeeLookup interface {
	NamesOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// =========================================================================
// HẠ TẦNG MEDIA
// =========================================================================

// ICEServer là một mục trong danh sách máy chủ ICE gửi cho trình duyệt.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// MediaServer là cổng tới SFU và TURN.
//
// Một interface cho cả hai vì trong triển khai hiện tại chúng là cùng một
// tiến trình (LiveKit kèm TURN tích hợp). Tách ra khi nào thật sự chạy hai
// máy chủ riêng — tách sớm chỉ tạo ra hai interface luôn được truyền cùng
// nhau.
type MediaServer interface {
	// IssueToken cấp access token vào một phòng.
	//
	// Token nhúng sẵn tên phòng và danh tính, nên client KHÔNG tự chọn được
	// phòng. Đây là chỗ chặn việc một người nghe lén cuộc họp mà họ biết id.
	IssueToken(ctx context.Context, roomName, identity, displayName string,
		canPublish bool, ttl time.Duration) (string, error)

	// ICEServers trả về danh sách STUN/TURN kèm credential ngắn hạn.
	//
	// Credential sinh theo HMAC thời gian, không dùng user/pass tĩnh: một
	// cặp tĩnh lộ ra là bị dùng chùa băng thông, và không có cách nào thu
	// hồi ngoài đổi khoá cho tất cả mọi người.
	ICEServers(ctx context.Context, identity string, ttl time.Duration) ([]ICEServer, error)

	// CloseRoom đóng phòng và đá mọi người còn lại ra.
	//
	// Gọi khi cuộc gọi kết thúc. Không gọi thì phòng rỗng vẫn chiếm tài
	// nguyên trên SFU cho tới lúc nó tự dọn.
	CloseRoom(ctx context.Context, roomName string) error
}

// Signaler đẩy bản tin gọi tới người dùng qua WebSocket.
//
// Khai báo ở đây với đúng một method, giống mẫu chat.Pusher của Phase 5.
// Không trả lỗi: gửi được hay không thì cuộc gọi vẫn phải tiếp tục theo
// đúng trạng thái của nó, và một lỗi ở đây chỉ đáng ghi log.
type Signaler interface {
	PushCall(ctx context.Context, recipients []uuid.UUID, eventType string, payload any)
}
