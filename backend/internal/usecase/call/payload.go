package call

import (
	"time"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
)

// Payload của các bản tin realtime mà module gọi phát ra.
//
// Khai báo ở tầng usecase và dùng LẠI cho cả REST lẫn WebSocket, giống
// MessageView của module chat. Lý do là một lỗi đã xảy ra thật ở Phase 5:
// đẩy thẳng entity domain (không có json tag) xuống WebSocket khiến client
// nhận `{"ID":...}` thay vì `{"id":...}` và im lặng bỏ qua — không một phép
// thử REST nào bắt được, vì REST đi qua một DTO khác.

// incomingPayload là nội dung bản tin đổ chuông.
type incomingPayload struct {
	CallID         uuid.UUID       `json:"call_id"`
	ConversationID uuid.UUID       `json:"conversation_id"`
	Kind           domaincall.Kind `json:"kind"`
	InitiatorID    uuid.UUID       `json:"initiator_id"`
	InitiatorName  string          `json:"initiator_name"`

	// ExpiresAt để client tự tắt chuông đúng lúc thay vì chờ máy chủ báo.
	//
	// Máy chủ vẫn gửi call.cancelled khi hết giờ, nhưng bản tin đó có thể
	// tới muộn hoặc không tới nếu mạng chớp — và một cái chuông không tắt
	// là thứ người dùng nhớ rất lâu.
	ExpiresAt time.Time `json:"expires_at"`
}

// ringingPayload báo cho người gọi biết hệ thống đã làm gì với lời mời.
type ringingPayload struct {
	CallID  uuid.UUID   `json:"call_id"`
	Ringing []uuid.UUID `json:"ringing"`
	// Busy là những người không được đổ chuông vì đang trong cuộc gọi khác.
	// Gửi xuống để giao diện nói rõ "Đang bận" thay vì để người gọi ngồi
	// đoán vì sao không ai bắt máy.
	Busy []uuid.UUID `json:"busy,omitempty"`
}

// statusPayload dùng chung cho accepted, rejected, cancelled và ended.
//
// Một hình dạng cho bốn sự kiện vì client xử lý chúng gần như giống nhau:
// đóng màn hình đổ chuông và cập nhật trạng thái. Bốn struct riêng chỉ tạo
// ra bốn nhánh giống hệt nhau ở frontend.
type statusPayload struct {
	CallID uuid.UUID         `json:"call_id"`
	Status domaincall.Status `json:"status"`
	// By là người gây ra thay đổi. uuid.Nil khi do hệ thống (hết giờ).
	By uuid.UUID `json:"by,omitempty"`
	// Reason dùng hằng trong domain: hangup, timeout, busy, rejected...
	Reason string `json:"reason,omitempty"`
	// DurationSeconds chỉ có nghĩa với ended.
	DurationSeconds int `json:"duration_seconds,omitempty"`
}

// participantPayload báo có người vào hoặc rời phòng giữa cuộc gọi.
type participantPayload struct {
	CallID     uuid.UUID `json:"call_id"`
	EmployeeID uuid.UUID `json:"employee_id"`
	Name       string    `json:"name,omitempty"`
	Joined     bool      `json:"joined"`
}
