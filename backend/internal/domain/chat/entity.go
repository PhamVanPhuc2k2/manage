// Package chat chứa entity và port của module chat.
package chat

import (
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound  = errors.New("không tìm thấy")
	ErrNotMember = errors.New("không phải thành viên hội thoại")
)

// MaxMessageLength giới hạn độ dài một tin nhắn.
//
// 4000 ký tự đủ cho mọi tin nhắn công việc thật. Không giới hạn thì một
// lần dán nhầm cả tệp log vào khung chat sẽ làm chậm mọi người đang mở
// hội thoại đó.
const MaxMessageLength = 4000

// =========================================================================
// HỘI THOẠI
// =========================================================================

type Kind string

const (
	KindDirect     Kind = "direct"
	KindGroup      Kind = "group"
	KindDepartment Kind = "department"
	KindProject    Kind = "project"
)

func (k Kind) Valid() bool {
	switch k {
	case KindDirect, KindGroup, KindDepartment, KindProject:
		return true
	}
	return false
}

// Managed cho biết thành viên nhóm do hệ thống quản lý, không sửa tay được.
//
// Nhóm phòng ban và nhóm dự án lấy thành viên từ chính phòng ban / dự án
// đó. Cho sửa tay sẽ tạo ra hai nguồn sự thật, và người vừa chuyển phòng
// vẫn đọc được tin nhắn của phòng cũ.
func (k Kind) Managed() bool { return k == KindDepartment || k == KindProject }

type Conversation struct {
	ID        uuid.UUID
	CompanyID uuid.UUID

	Kind Kind
	Name string

	DepartmentID *uuid.UUID
	ProjectID    *uuid.UUID
	DirectKey    string

	CreatedBy     *uuid.UUID
	LastMessageAt *time.Time

	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time

	// Các trường dưới đây tính theo NGƯỜI ĐANG XEM, nạp lúc đọc.
	//
	// Hội thoại 1-1 không có tên cố định: với A thì nó tên là B, với B thì
	// nó tên là A. Vì vậy tên hiển thị phải tính theo người xem chứ không
	// lưu trong bảng.
	DisplayName string
	MemberCount int
	UnreadCount int
	IsPinned    bool
	IsMuted     bool
	IsAdmin     bool

	// PeerID là người đối diện, chỉ có với hội thoại 1-1.
	PeerID     *uuid.UUID
	PeerStatus string

	LastMessage *Message
}

// DirectKeyFor dựng khoá duy nhất cho một cặp hội thoại 1-1.
//
// Sắp xếp hai id trước khi ghép, để (A,B) và (B,A) cho CÙNG một khoá. Thiếu
// bước sắp xếp thì hai người cùng bấm "nhắn tin" một lúc sẽ tạo ra hai hội
// thoại song song, và tin nhắn của họ đi vào hai chỗ khác nhau.
func DirectKeyFor(a, b uuid.UUID) string {
	ids := []string{a.String(), b.String()}
	sort.Strings(ids)
	return ids[0] + ":" + ids[1]
}

// GroupSource là một nguồn sinh nhóm tự động: một phòng ban hoặc một dự án.
//
// Một kiểu chung cho cả hai vì thuật toán đồng bộ giống hệt nhau; chỗ khác
// nhau duy nhất là nơi lấy danh sách thành viên.
type GroupSource struct {
	Kind Kind
	ID   uuid.UUID
	Name string
}

type Member struct {
	ConversationID uuid.UUID
	EmployeeID     uuid.UUID

	IsAdmin bool

	LastReadMessageID *uuid.UUID
	LastReadAt        *time.Time

	IsPinned bool
	IsMuted  bool

	JoinedAt time.Time
	LeftAt   *time.Time

	// JOIN lúc đọc.
	EmployeeName string
	EmployeeCode string
	Status       string
}

func (m *Member) Active() bool { return m.LeftAt == nil }

// =========================================================================
// TIN NHẮN
// =========================================================================

type MessageKind string

const (
	MessageText   MessageKind = "text"
	MessageFile   MessageKind = "file"
	MessageImage  MessageKind = "image"
	MessageSystem MessageKind = "system"
)

func (k MessageKind) Valid() bool {
	switch k {
	case MessageText, MessageFile, MessageImage, MessageSystem:
		return true
	}
	return false
}

type Message struct {
	ID             uuid.UUID
	ConversationID uuid.UUID
	SenderID       *uuid.UUID

	Kind      MessageKind
	Content   string
	ReplyToID *uuid.UUID

	ClientMessageID string

	EditedAt  *time.Time
	DeletedAt *time.Time
	CreatedAt time.Time

	// JOIN hoặc nạp lúc đọc.
	SenderName  string
	Attachments []*Attachment

	// ReplyTo là bản rút gọn của tin được trả lời, đủ để hiển thị trích dẫn.
	ReplyToSender  string
	ReplyToContent string
}

// Deleted cho biết tin đã bị thu hồi.
func (m *Message) Deleted() bool { return m.DeletedAt != nil }

// DisplayContent trả về nội dung an toàn để hiển thị.
//
// Tin đã thu hồi KHÔNG trả về nội dung gốc, kể cả khi bản ghi vẫn còn.
// Xoá mềm là để giữ dấu vết cho quản trị, không phải để nội dung vẫn rò ra
// qua API — chỉ cần một chỗ quên kiểm tra DeletedAt là việc thu hồi thành
// vô nghĩa.
func (m *Message) DisplayContent() string {
	if m.Deleted() {
		return ""
	}
	return m.Content
}

type Attachment struct {
	ID          uuid.UUID
	MessageID   uuid.UUID
	StorageKey  string
	FileName    string
	ContentType string
	SizeBytes   int64
	Width       *int
	Height      *int
	CreatedAt   time.Time

	// URL ký lúc đọc, không lưu database.
	URL string
}

// MessageFilter gom điều kiện đọc lịch sử tin nhắn.
type MessageFilter struct {
	ConversationID uuid.UUID

	// Before là cursor: chỉ lấy tin CŨ HƠN mốc này. Dùng cho cuộn ngược lên.
	//
	// Cursor chứ không offset: chat có tin mới liên tục ở cuối, và offset
	// sẽ khiến mỗi lần tải thêm bị lặp hoặc nhảy cóc.
	Before *time.Time
	Search string
	Limit  int
}

// =========================================================================
// SỰ KIỆN REALTIME
// =========================================================================

// Loại bản tin chat gửi qua WebSocket. Client và server phải dùng chung
// đúng các chuỗi này.
const (
	EventMessageNew     = "chat.message"
	EventMessageEdited  = "chat.message_edited"
	EventMessageDeleted = "chat.message_deleted"
	EventTyping         = "chat.typing"
	EventRead           = "chat.read"
	EventConversation   = "chat.conversation"
)

// TypingPayload là chỉ báo "đang nhập".
//
// KHÔNG lưu database: nó hết giá trị sau vài giây, và ghi mỗi lần gõ phím
// sẽ tạo ra lượng ghi lớn hơn cả tin nhắn thật.
type TypingPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	EmployeeID     uuid.UUID `json:"employee_id"`
	EmployeeName   string    `json:"employee_name"`
}

// ReadPayload báo người khác biết tin đã được đọc tới đâu.
type ReadPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	EmployeeID     uuid.UUID `json:"employee_id"`
	MessageID      uuid.UUID `json:"message_id"`
}
