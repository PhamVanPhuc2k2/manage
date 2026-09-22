package chat

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ConversationRepository làm việc với hội thoại và thành viên.
type ConversationRepository interface {
	Create(ctx context.Context, c *Conversation) error
	Update(ctx context.Context, c *Conversation) error
	SoftDelete(ctx context.Context, id uuid.UUID) error

	// ByID trả về hội thoại kèm các trường tính theo người xem.
	ByID(ctx context.Context, id, viewerID uuid.UUID) (*Conversation, error)

	// ByDirectKey dùng để tái sử dụng hội thoại 1-1 đã có.
	ByDirectKey(ctx context.Context, companyID uuid.UUID, key string) (*Conversation, error)

	// BySource tìm nhóm tự động của một phòng ban hoặc dự án.
	BySource(ctx context.Context, field string, sourceID uuid.UUID) (*Conversation, error)

	// ListFor liệt kê hội thoại của một người: ghim trước, rồi mới nhất trước.
	ListFor(ctx context.Context, viewerID uuid.UUID, search string) ([]*Conversation, error)

	// Members trả về thành viên còn trong hội thoại.
	Members(ctx context.Context, conversationID uuid.UUID) ([]*Member, error)

	// MemberIDs chỉ lấy id, dùng cho fan-out realtime và cho việc tạo thông báo.
	MemberIDs(ctx context.Context, conversationID uuid.UUID) ([]uuid.UUID, error)

	// Member trả về ErrNotMember khi người này không (còn) ở trong hội thoại.
	//
	// Mọi thao tác chat đều đi qua đây trước. Kiểm tra quyền ở tầng middleware
	// chỉ trả lời được "người này có được dùng chat không", không trả lời được
	// "người này có được đọc hội thoại NÀY không".
	Member(ctx context.Context, conversationID, employeeID uuid.UUID) (*Member, error)

	AddMembers(ctx context.Context, conversationID uuid.UUID, employeeIDs []uuid.UUID, admin bool) error
	RemoveMember(ctx context.Context, conversationID, employeeID uuid.UUID) error
	SetAdmin(ctx context.Context, conversationID, employeeID uuid.UUID, admin bool) error

	// SyncMembers đưa danh sách thành viên của nhóm tự động về đúng nguồn gốc.
	//
	// Thêm người mới, gỡ người đã rời. Trả về số thêm và số gỡ để job đồng bộ
	// ghi log có ích thay vì chỉ "đã chạy".
	SyncMembers(ctx context.Context, conversationID uuid.UUID, want []uuid.UUID) (added, removed int, err error)

	SetPinned(ctx context.Context, conversationID, employeeID uuid.UUID, pinned bool) error
	SetMuted(ctx context.Context, conversationID, employeeID uuid.UUID, muted bool) error

	// MarkRead dời mốc đã đọc và trả về số chưa đọc còn lại.
	MarkRead(ctx context.Context, conversationID, employeeID, messageID uuid.UUID) error

	// TotalUnread là tổng số tin chưa đọc của một người trên mọi hội thoại,
	// phục vụ huy hiệu trên biểu tượng chat.
	TotalUnread(ctx context.Context, employeeID uuid.UUID) (int, error)
}

// MessageRepository làm việc với tin nhắn và tệp đính kèm.
type MessageRepository interface {
	Create(ctx context.Context, m *Message) error

	// ByClientID tra tin đã ghi theo mã client gửi lên.
	//
	// Dùng để trả về ĐÚNG tin cũ khi client gửi lại sau khi mất mạng, thay vì
	// ghi thêm một bản sao.
	ByClientID(ctx context.Context, conversationID, senderID uuid.UUID, clientID string) (*Message, error)

	ByID(ctx context.Context, id uuid.UUID) (*Message, error)
	List(ctx context.Context, f MessageFilter) ([]*Message, error)

	Edit(ctx context.Context, id uuid.UUID, content string) error
	SoftDelete(ctx context.Context, id uuid.UUID) error

	AddAttachments(ctx context.Context, items []*Attachment) error

	// AttachmentsOf nạp tệp đính kèm cho nhiều tin trong một lượt, tránh N+1
	// khi trả về một trang lịch sử.
	AttachmentsOf(ctx context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]*Attachment, error)
}

// Pusher đẩy sự kiện chat tới người đang online.
//
// Khai báo phía người dùng, giống notification.Pusher: module chat không được
// biết về hub hay RabbitMQ.
type Pusher interface {
	// PushChat gửi một sự kiện chat tới danh sách người nhận.
	//
	// KHÔNG trả lỗi: tin nhắn đã ghi vào database rồi, đẩy realtime hỏng thì
	// người dùng vẫn thấy nó ở lần tải sau. Làm hỏng cả lời gọi gửi tin vì
	// RabbitMQ chập là đánh đổi sai.
	PushChat(ctx context.Context, recipients []uuid.UUID, eventType string, payload any)
}

// Notifier tạo thông báo cho tin nhắn khi người nhận offline.
//
// Khai báo lại ở đây thay vì dùng thẳng notification.Request: module chat chỉ
// cần đúng một việc, và phụ thuộc hai chiều giữa hai module là thứ về sau rất
// khó gỡ.
type Notifier interface {
	NotifyNewMessage(ctx context.Context, recipients []uuid.UUID,
		senderID uuid.UUID, senderName, conversationName, preview, link string) error
}

// PresenceReader cho biết ai đang online, dùng cho chấm xanh và cho việc
// quyết định có tạo thông báo hay không.
type PresenceReader interface {
	StatusOf(ctx context.Context, employeeIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// EmployeeLookup là phần module chat cần biết về nhân viên.
type EmployeeLookup interface {
	NamesOf(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)

	// SameCompany kiểm tra mọi id đều thuộc công ty này.
	//
	// Chặn việc thêm người ngoài công ty vào nhóm bằng cách gọi thẳng API.
	SameCompany(ctx context.Context, companyID uuid.UUID, ids []uuid.UUID) (bool, error)

	// ByDepartment và ByProject cấp danh sách thành viên cho nhóm tự động.
	ByDepartment(ctx context.Context, departmentID uuid.UUID) ([]uuid.UUID, error)
	ByProject(ctx context.Context, projectID uuid.UUID) ([]uuid.UUID, error)
}

// SourceLister liệt kê phòng ban và dự án đang hoạt động của một công ty.
//
// Dùng cho job dựng nhóm chat tự động. Tách khỏi EmployeeLookup vì nó hỏi về
// tổ chức chứ không về con người, và gộp lại sẽ buộc mọi chỗ chỉ cần tra tên
// nhân viên phải mang theo cả hai phép liệt kê này.
type SourceLister interface {
	Sources(ctx context.Context, companyID uuid.UUID) ([]GroupSource, error)
}

// Storage là cổng lưu trữ tệp đính kèm.
type Storage interface {
	PresignPut(ctx context.Context, key, contentType string, expires time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, expires time.Duration) (string, error)

	// Stat trả về kích thước và kiểu tệp THẬT trên R2.
	//
	// Cần vì client tải tệp lên THẲNG R2, không qua api — nên api không hề
	// thấy nội dung và không biết tệp có đúng như đã khai hay không, kể cả
	// kích thước.
	Stat(ctx context.Context, key string) (size int64, contentType string, err error)

	// DetectContentType đọc 512 byte đầu và suy ra kiểu tệp từ NỘI DUNG.
	//
	// Content-Type client khai lúc xin URL chỉ là lời khai, sửa được tuỳ ý.
	// Đây là thứ duy nhất nói lên tệp đó thật sự là gì.
	DetectContentType(ctx context.Context, key string) (string, error)

	// Delete dọn tệp bị từ chối. Không dọn thì R2 tích dần những tệp không
	// bản ghi nào trỏ tới, và không có cách nào biết chúng là rác.
	Delete(ctx context.Context, key string) error
}
