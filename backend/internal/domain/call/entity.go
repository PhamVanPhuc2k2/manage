// Package call là tầng domain của module gọi video và gọi thoại.
//
// Package này KHÔNG biết gì về WebRTC, LiveKit hay WebSocket. Nó chỉ mô tả
// một cuộc gọi là gì, đi qua những trạng thái nào, và ai được làm gì với
// nó. Phần kỹ thuật truyền media nằm ở tầng repository và delivery.
//
// Ranh giới đó quan trọng hơn bình thường ở đây: WebRTC và SFU là hai thứ
// thay đổi nhanh, còn "một cuộc gọi có người khởi tạo, có người tham gia,
// và kết thúc vì một lý do nào đó" thì không.
package call

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("không tìm thấy cuộc gọi")
	ErrNotMember = errors.New("không phải thành viên hội thoại")
)

// =========================================================================
// KIỂU VÀ TRẠNG THÁI
// =========================================================================

type Kind string

const (
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
)

func (k Kind) Valid() bool { return k == KindAudio || k == KindVideo }

type Status string

const (
	StatusRinging   Status = "ringing"
	StatusActive    Status = "active"
	StatusEnded     Status = "ended"
	StatusMissed    Status = "missed"
	StatusRejected  Status = "rejected"
	StatusCancelled Status = "cancelled"
	StatusFailed    Status = "failed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusRinging, StatusActive, StatusEnded,
		StatusMissed, StatusRejected, StatusCancelled, StatusFailed:
		return true
	}
	return false
}

// Live: cuộc gọi còn đang diễn ra, tức là còn chiếm chỗ trong hội thoại.
//
// Đây là điều kiện dùng ở chỉ mục một phần uq_calls_active_per_conversation.
// Giữ định nghĩa ở MỘT chỗ để Go và SQL không nói hai chuyện khác nhau —
// lệch nhau thì database từ chối một cuộc gọi mà tầng nghiệp vụ tưởng hợp lệ,
// và lỗi hiện ra là 500 không giải thích được.
func (s Status) Live() bool { return s == StatusRinging || s == StatusActive }

// Final: đã kết thúc, không đổi trạng thái được nữa.
func (s Status) Final() bool { return !s.Live() }

// Answered: cuộc gọi đã từng có người bắt máy.
//
// Phân biệt với Final: một cuộc gọi bị từ chối cũng là Final nhưng chưa bao
// giờ được trả lời, và giao diện hiển thị hai thứ đó khác nhau.
func (s Status) Answered() bool { return s == StatusActive || s == StatusEnded }

// =========================================================================
// LÝ DO KẾT THÚC
// =========================================================================

// Lý do kết thúc, lưu dạng chuỗi tự do trong database nhưng khai báo hằng ở
// đây để không gõ lệch. Gõ lệch thì thống kê im lặng đếm thiếu.
const (
	ReasonHangup   = "hangup"   // có người bấm kết thúc
	ReasonTimeout  = "timeout"  // hết 45 giây không ai bắt máy
	ReasonBusy     = "busy"     // người nhận đang trong cuộc gọi khác
	ReasonRejected = "rejected" // người nhận bấm từ chối
	ReasonNetwork  = "network"  // mất kết nối signaling
	ReasonEmpty    = "empty"    // phòng không còn ai
)

// =========================================================================
// THỜI GIAN
// =========================================================================

const (
	// RingTimeout là thời gian đổ chuông trước khi tự huỷ.
	//
	// 45 giây theo đặc tả. Ngắn hơn thì người đang ở phòng khác không kịp
	// quay lại bàn; dài hơn thì người gọi ngồi nghe chuông vô vọng, và một
	// cuộc gọi nhỡ rõ ràng còn hơn một phút chờ đợi.
	RingTimeout = 45 * time.Second

	// EmptyRoomGrace là thời gian giữ phòng sau khi người cuối rời đi.
	//
	// Không đóng ngay: rớt mạng chớp nhoáng rồi vào lại là chuyện thường,
	// và đóng phòng ngay biến một sự cố hai giây thành một cuộc gọi phải
	// bắt đầu lại từ đầu.
	EmptyRoomGrace = 30 * time.Second

	// TokenTTL là hạn của access token vào phòng SFU.
	//
	// Ngắn vì token này cho phép publish media vào phòng: lộ ra là người
	// ngoài nói chen vào cuộc họp. Đủ dài để người dùng kịp bấm "tham gia"
	// sau khi màn hình kiểm tra thiết bị hiện lên.
	TokenTTL = 10 * time.Minute

	// ICECredentialTTL là hạn của credential TURN.
	//
	// TURN relay tốn băng thông thật, nên credential phải hết hạn. Dài hơn
	// TokenTTL vì ICE có thể phải thương lượng lại giữa cuộc gọi (đổi WiFi
	// sang 4G) mà không ai cấp token mới.
	ICECredentialTTL = 30 * time.Minute
)

// =========================================================================
// TÊN SỰ KIỆN REALTIME
// =========================================================================

// Vòng đời một cuộc gọi, nhìn từ phía bản tin:
//
//	người gọi          hub              người nhận
//	   │  POST /calls   │                    │
//	   │─────────────►│   call.incoming    │
//	   │                │─────────────────►│  (MỌI thiết bị)
//	   │  call.ringing  │                    │
//	   │◄──────────────│                    │
//	   │                │   POST /accept     │
//	   │  call.accepted │◄─────────────────│
//	   │◄──────────────│   call.cancelled   │
//	   │                │─────────────────►│  (thiết bị CÒN LẠI)
//
// call.cancelled gửi tới các thiết bị khác của chính người nhận là phần hay
// bị quên nhất: không có nó thì điện thoại vẫn đổ chuông sau khi người ta
// đã bắt máy trên máy tính.
const (
	// EventIncoming: có cuộc gọi tới, đổ chuông.
	EventIncoming = "call.incoming"
	// EventRinging: báo cho người gọi rằng đầu kia đang đổ chuông.
	EventRinging = "call.ringing"
	// EventAccepted: có người bắt máy.
	EventAccepted = "call.accepted"
	// EventRejected: người nhận từ chối, hoặc hệ thống từ chối thay vì họ
	// đang bận cuộc khác.
	EventRejected = "call.rejected"
	// EventCancelled: lời mời không còn hiệu lực — người gọi cúp trước, hết
	// giờ, hoặc chính người nhận đã bắt máy ở thiết bị khác.
	EventCancelled = "call.cancelled"
	// EventEnded: cuộc gọi đã kết thúc.
	EventEnded = "call.ended"
	// EventParticipant: có người vào hoặc rời phòng giữa cuộc gọi.
	EventParticipant = "call.participant"
)

// =========================================================================
// ENTITY
// =========================================================================

type Call struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	InitiatorID    *uuid.UUID

	Kind   Kind
	Status Status

	RoomName string

	StartedAt time.Time
	EndedAt   *time.Time

	EndReason string

	// RelayRatio là tỉ lệ người phải đi qua TURN, từ 0 tới 1. nil khi chưa
	// tính được.
	RelayRatio *float64

	CreatedAt time.Time

	// Nạp kèm lúc đọc.
	InitiatorName string
	Participants  []*Participant
}

// Duration là thời lượng cuộc gọi.
//
// Trả 0 cho cuộc gọi chưa kết thúc thay vì "tới bây giờ": một con số tăng
// dần mỗi lần đọc sẽ khiến mọi phép tổng hợp cho ra kết quả khác nhau tuỳ
// thời điểm chạy.
func (c *Call) Duration() time.Duration {
	if c.EndedAt == nil {
		return 0
	}
	d := c.EndedAt.Sub(c.StartedAt)
	// Kẹp về 0 thay vì trả số âm. Database đã có CHECK, nhưng hàm này cũng
	// chạy trên đối tượng chưa lưu, và một giá trị âm sẽ lặng lẽ làm hỏng
	// trung bình cộng trong báo cáo.
	if d < 0 {
		return 0
	}
	return d
}

// SystemMessage là câu mô tả cuộc gọi, ghi vào hội thoại khi nó kết thúc.
//
// Viết ở tầng domain chứ không ở giao diện: cùng một câu phải xuất hiện
// giống nhau trên web, trên thông báo đẩy và trong bản xuất lịch sử chat.
func (c *Call) SystemMessage() string {
	label := "Cuộc gọi thoại"
	if c.Kind == KindVideo {
		label = "Cuộc gọi video"
	}

	switch c.Status {
	case StatusMissed:
		return label + " · không có người nghe"
	case StatusRejected:
		return label + " · đã từ chối"
	case StatusCancelled:
		return label + " · người gọi đã huỷ"
	case StatusFailed:
		return label + " · không kết nối được"
	}

	d := c.Duration()
	if d <= 0 {
		return label
	}
	return label + " · " + humanDuration(d)
}

// humanDuration viết thời lượng theo cách người Việt đọc.
//
// Không dùng time.Duration.String(): nó cho ra "1h2m3s", đúng với lập trình
// viên nhưng không phải thứ hiện trong khung chat của nhân viên.
func humanDuration(d time.Duration) string {
	total := int(d.Seconds())
	if total < 60 {
		return itoa(total) + " giây"
	}

	h := total / 3600
	m := (total % 3600) / 60

	if h == 0 {
		return itoa(m) + " phút"
	}
	if m == 0 {
		return itoa(h) + " giờ"
	}
	return itoa(h) + " giờ " + itoa(m) + " phút"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Participant là một người trong cuộc gọi.
type Participant struct {
	ID         uuid.UUID
	CallID     uuid.UUID
	EmployeeID uuid.UUID

	JoinedAt *time.Time
	LeftAt   *time.Time

	// Ba cờ ghi lại việc ĐÃ TỪNG bật, không phải trạng thái hiện tại.
	HadAudio  bool
	HadVideo  bool
	HadScreen bool

	UsedRelay *bool

	CreatedAt time.Time

	// Nạp kèm lúc đọc.
	EmployeeName string
	AvatarKey    string
}

// InRoom: đã vào và chưa rời.
func (p *Participant) InRoom() bool {
	return p.JoinedAt != nil && p.LeftAt == nil
}

// RoomNameFor sinh tên phòng SFU từ id cuộc gọi.
//
// Có tiền tố để phân biệt với mọi thứ khác có thể dùng chung một máy chủ
// LiveKit, và để đọc log thấy ngay đây là phòng của hệ thống nào.
func RoomNameFor(callID uuid.UUID) string {
	return "manage-call-" + callID.String()
}
