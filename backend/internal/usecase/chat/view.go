package chat

import (
	"time"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

// MessageView là hình dạng tin nhắn trả ra API VÀ đẩy qua WebSocket.
//
// Dùng CHUNG một kiểu cho cả hai đường là bắt buộc, không phải tiện tay: client
// dựng một khung tin nhắn duy nhất và khớp tin lạc quan với tin thật bằng
// client_message_id. Nếu hai đường có hai hình dạng khác nhau, bản tin realtime
// sẽ về với tên trường khác và client im lặng bỏ qua — lỗi chỉ lộ ra khi có
// người thứ hai đang mở cùng hội thoại, nên rất dễ lọt qua kiểm thử.
//
// Đây cũng là lý do kiểu này nằm ở tầng usecase chứ không ở handler: tầng đẩy
// realtime không đi qua handler, nên một DTO chỉ handler biết là không đủ.
type MessageView struct {
	ID             uuid.UUID  `json:"id"`
	ConversationID uuid.UUID  `json:"conversation_id"`
	SenderID       *uuid.UUID `json:"sender_id,omitempty"`
	SenderName     string     `json:"sender_name,omitempty"`

	Kind    domainchat.MessageKind `json:"kind"`
	Content string                 `json:"content"`

	ReplyToID      *uuid.UUID `json:"reply_to_id,omitempty"`
	ReplyToSender  string     `json:"reply_to_sender,omitempty"`
	ReplyToContent string     `json:"reply_to_content,omitempty"`

	ClientMessageID string `json:"client_message_id,omitempty"`

	Attachments []AttachmentView `json:"attachments,omitempty"`

	Deleted   bool       `json:"deleted"`
	EditedAt  *time.Time `json:"edited_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type AttachmentView struct {
	ID          uuid.UUID `json:"id"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Width       *int      `json:"width,omitempty"`
	Height      *int      `json:"height,omitempty"`
	URL         string    `json:"url,omitempty"`
}

func toMessageView(m *domainchat.Message) *MessageView {
	v := &MessageView{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		SenderID:       m.SenderID,
		SenderName:     m.SenderName,
		Kind:           m.Kind,
		// DisplayContent chứ không phải Content: tin đã thu hồi không được
		// trả nội dung gốc ra ngoài, kể cả khi bản ghi vẫn còn.
		Content:         m.DisplayContent(),
		ReplyToID:       m.ReplyToID,
		ReplyToSender:   m.ReplyToSender,
		ReplyToContent:  m.ReplyToContent,
		ClientMessageID: m.ClientMessageID,
		Deleted:         m.Deleted(),
		EditedAt:        m.EditedAt,
		CreatedAt:       m.CreatedAt,
	}

	for _, a := range m.Attachments {
		v.Attachments = append(v.Attachments, AttachmentView{
			ID:          a.ID,
			FileName:    a.FileName,
			ContentType: a.ContentType,
			SizeBytes:   a.SizeBytes,
			Width:       a.Width,
			Height:      a.Height,
			URL:         a.URL,
		})
	}
	return v
}

func toMessageViews(items []*domainchat.Message) []*MessageView {
	out := make([]*MessageView, 0, len(items))
	for _, m := range items {
		out = append(out, toMessageView(m))
	}
	return out
}

// DeletedPayload là bản tin báo một tin nhắn vừa bị thu hồi.
//
// Chỉ mang id: nội dung đã bị xoá, và gửi lại cả tin nhắn chỉ để nói "nó không
// còn nữa" là gửi đúng thứ vừa phải giấu đi.
type DeletedPayload struct {
	ID             uuid.UUID `json:"id"`
	ConversationID uuid.UUID `json:"conversation_id"`
}
