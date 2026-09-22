// Package realtime chứa entity và port của hạ tầng WebSocket: định dạng bản
// tin, trạng thái hiện diện (presence), và cổng phát tin giữa các instance.
//
// Package này CỐ Ý không biết gì về chat hay thông báo. Nó chỉ lo việc đưa
// một bản tin từ chỗ này tới đúng người nhận, dù họ đang nối vào instance
// nào. Nghiệp vụ nằm ở các module khác và dùng lại đường ống này.
package realtime

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// =========================================================================
// ĐỊNH DẠNG BẢN TIN
// =========================================================================

// Envelope là hình dạng DUY NHẤT của mọi bản tin đi qua WebSocket, cả hai
// chiều.
//
// Một hình dạng chung cho tất cả là có chủ ý: client chỉ cần viết một bộ
// giải mã và một bộ định tuyến theo `type`. Mỗi loại bản tin một hình dạng
// riêng sẽ khiến phần xử lý ở frontend phình ra theo số loại.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	TS      time.Time       `json:"ts"`

	// TraceID nối bản tin này với request HTTP đã sinh ra nó. Không có nó,
	// một thông báo đến sai người là chuyện không thể lần ngược được.
	TraceID string `json:"trace_id,omitempty"`
}

// Bản tin client GỬI LÊN server.
const (
	// TypeHeartbeat: client còn sống và đang (hoặc không đang) hoạt động.
	// Đây là nguồn dữ liệu chấm công của Phase 3.
	TypeHeartbeat = "heartbeat"
	// TypePing: client chủ động thăm dò. Server trả TypePong.
	TypePing = "ping"
)

// Bản tin GỌI ĐIỆN, chiều client → server.
//
// Hub chỉ CHUYỂN TIẾP nhóm này, không hiểu nội dung SDP hay ICE bên trong.
// Đó là cố ý: signaling của WebRTC là một cuộc hội thoại giữa hai trình
// duyệt, và máy chủ càng biết ít về nó thì càng ít thứ phải sửa khi trình
// duyệt đổi hành vi.
const (
	// TypeCallSDP chuyển tiếp SDP offer/answer giữa hai đầu.
	TypeCallSDP = "call.sdp"
	// TypeCallICE chuyển tiếp một ICE candidate.
	TypeCallICE = "call.ice"
)

// Bản tin server GỬI XUỐNG client.
const (
	TypeWelcome  = "welcome"
	TypePong     = "pong"
	TypeError    = "error"
	TypePresence = "presence.changed"

	// Các loại nghiệp vụ. Khai báo ở đây để client và server không gõ lệch
	// chuỗi — gõ lệch thì bản tin đến nơi nhưng không ai xử lý, và không có
	// lỗi nào được báo.
	TypeNotification = "notification"
	TypeTaskUpdated  = "task.updated"

	// TypeNotificationBadge chỉ mang con số chưa đọc.
	//
	// Tách khỏi TypeNotification vì hai việc khác nhau: một thông báo mới
	// làm số tăng, nhưng đọc ở tab khác cũng làm số đổi mà KHÔNG có thông
	// báo nào mới. Gộp lại thì tab kia sẽ hiện một thẻ thông báo ma.
	TypeNotificationBadge = "notification.badge"

	// TypeChatBadge là tổng số tin nhắn chưa đọc trên mọi hội thoại.
	TypeChatBadge = "chat.badge"
)

// Các loại bản tin GỌI ĐIỆN chiều server → client KHÔNG khai ở đây.
//
// Chúng nằm trong internal/domain/call, theo đúng tiền lệ của chat: package
// realtime giữ các loại DÙNG CHUNG (welcome, pong, error, presence) còn tên
// sự kiện nghiệp vụ thuộc về module sinh ra chúng. Nhờ vậy package này
// không phải đổi mỗi lần một module thêm một loại thông báo mới.
//
// Riêng call.sdp và call.ice ở trên là chiều ngược lại (client → server) nên
// hub phải nhận diện được chúng — giống heartbeat và ping.


// HeartbeatPayload là nội dung bản tin nhịp tim.
//
// IsActive phân biệt "mở tab" với "đang làm việc": client tự đặt false khi
// không có chuột/bàn phím quá 5 phút hoặc khi tab bị ẩn. Phân biệt này là
// điều kiện để dữ liệu chấm công không vô nghĩa — máy để đó qua đêm vẫn
// online, nhưng không ai làm việc.
type HeartbeatPayload struct {
	IsActive bool `json:"is_active"`
}

// ErrorPayload báo lỗi cho client mà không đóng kết nối.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CallSignalPayload là nội dung của call.sdp và call.ice.
//
// `Data` để nguyên dạng thô: đó là SDP hoặc ICE candidate do trình duyệt
// sinh ra, và máy chủ không có lý do gì để đọc hay sửa chúng. Khai báo một
// struct đầy đủ cho SDP chỉ tạo ra một thứ phải cập nhật mỗi lần chuẩn đổi.
type CallSignalPayload struct {
	CallID uuid.UUID `json:"call_id"`
	// To là người nhận. Bắt buộc với gọi nhóm: trong phòng ba người, một
	// SDP offer chỉ dành cho ĐÚNG MỘT đầu bên kia.
	To   uuid.UUID       `json:"to"`
	From uuid.UUID       `json:"from,omitempty"`
	Data json.RawMessage `json:"data"`
}

// NewEnvelope đóng gói một payload bất kỳ thành Envelope.
func NewEnvelope(typ string, payload any, traceID string) (Envelope, error) {
	e := Envelope{Type: typ, TS: time.Now(), TraceID: traceID}
	if payload == nil {
		return e, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return e, err
	}
	e.Payload = raw
	return e, nil
}

// =========================================================================
// ĐỊNH TUYẾN
// =========================================================================

// Message là một bản tin kèm danh sách người nhận, dùng để phát giữa các
// instance qua RabbitMQ.
//
// Người nhận là EMPLOYEE ID chứ không phải user id: mọi module nghiệp vụ
// (task, chat, chấm công) đều làm việc với nhân viên, và bắt chúng phải tra
// ngược ra user id chỉ để gửi một thông báo là thêm một truy vấn vô ích ở
// mọi chỗ gọi.
type Message struct {
	Recipients []uuid.UUID `json:"recipients"`
	Envelope   Envelope    `json:"envelope"`

	// Broadcast = true nghĩa là gửi cho MỌI người đang kết nối, bỏ qua
	// Recipients. Dùng cho thông báo toàn hệ thống (bảo trì, tắt máy chủ).
	Broadcast bool `json:"broadcast,omitempty"`
}

// =========================================================================
// PRESENCE
// =========================================================================

// PresenceTTL là thời gian sống của một bản ghi presence trong Redis.
//
// Bằng ba lần chu kỳ heartbeat (30 giây): mất hai nhịp liên tiếp do mạng
// chớp vẫn chưa bị coi là offline. Ngắn hơn thì trạng thái nhấp nháy liên
// tục; dài hơn thì người đã đóng máy vẫn hiện online hàng phút.
const (
	HeartbeatInterval = 30 * time.Second
	PresenceTTL       = 90 * time.Second
)

// Connection là một kết nối WebSocket đang sống của một người.
//
// Một người có thể có NHIỀU kết nối cùng lúc (máy tính ở công ty, điện
// thoại, thêm một tab nữa). Mọi phép tính về "người này có online không"
// phải gộp các kết nối lại, không được nhìn từng cái một.
type Connection struct {
	ConnID uuid.UUID `json:"conn_id"`
	// InstanceID là instance api đang giữ kết nối này. Đây chính là bản đồ
	// user → instance mà thiết kế Phase 5 yêu cầu, dùng để biết ai đang ở
	// đâu khi chạy nhiều replica.
	InstanceID  string    `json:"instance_id"`
	IsActive    bool      `json:"is_active"`
	ConnectedAt time.Time `json:"connected_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	DeviceInfo  string    `json:"device_info,omitempty"`
}

// Presence là trạng thái hiện diện tổng hợp của một nhân viên.
type Presence struct {
	EmployeeID  uuid.UUID     `json:"employee_id"`
	Connections []*Connection `json:"connections"`
}

// Online: còn ít nhất một kết nối sống.
func (p *Presence) Online() bool { return len(p.Connections) > 0 }

// Active: có ít nhất một kết nối đang hoạt động thật.
//
// Lấy theo "bất kỳ" chứ không phải "tất cả": người đang gõ trên máy tính
// nhưng để điện thoại trong túi vẫn là đang làm việc.
func (p *Presence) Active() bool {
	for _, c := range p.Connections {
		if c.IsActive {
			return true
		}
	}
	return false
}

// LastSeen trả về thời điểm hoạt động gần nhất trong mọi kết nối.
func (p *Presence) LastSeen() time.Time {
	var latest time.Time
	for _, c := range p.Connections {
		if c.LastSeenAt.After(latest) {
			latest = c.LastSeenAt
		}
	}
	return latest
}

// Status là trạng thái rút gọn để hiển thị.
type Status string

const (
	StatusOnline  Status = "online"  // đang kết nối và có hoạt động
	StatusIdle    Status = "idle"    // đang kết nối nhưng không hoạt động
	StatusOffline Status = "offline" // không có kết nối nào
)

func (p *Presence) Status() Status {
	switch {
	case !p.Online():
		return StatusOffline
	case p.Active():
		return StatusOnline
	default:
		return StatusIdle
	}
}

// PresenceChangedPayload báo cho client biết ai vừa đổi trạng thái.
type PresenceChangedPayload struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	Status     Status    `json:"status"`
	LastSeenAt time.Time `json:"last_seen_at"`
}
