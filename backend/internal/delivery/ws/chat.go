package ws

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
	ucchat "github.com/PhamVanPhuc2k2/manage/internal/usecase/chat"
)

// ChatService là phần module chat mà tầng WebSocket cần.
//
// Khai báo ở ĐÂY, phía người dùng, và cố ý rất hẹp: chỉ ba thao tác đáng đi
// qua WebSocket. Mọi việc còn lại (tạo nhóm, sửa thành viên, thu hồi tin) đi
// qua REST, nơi đã có sẵn phân quyền, nhật ký và mã lỗi HTTP tử tế.
type ChatService interface {
	Send(ctx context.Context, actor *domainauth.Actor, in ucchat.SendInput) (*ucchat.MessageView, error)
	Typing(ctx context.Context, actor *domainauth.Actor, conversationID uuid.UUID) error
	MarkRead(ctx context.Context, actor *domainauth.Actor, conversationID, messageID uuid.UUID) (int, error)
}

// Bản tin chat client GỬI LÊN. Tách khỏi các hằng trong domain/chat (vốn là
// sự kiện server gửi xuống) để không lẫn hai chiều.
const (
	typeChatSend   = "chat.send"
	typeChatTyping = "chat.typing"
	typeChatRead   = "chat.read"
)

type chatSendPayload struct {
	ConversationID  uuid.UUID `json:"conversation_id"`
	Content         string    `json:"content"`
	Kind            string    `json:"kind,omitempty"`
	ReplyToID       string    `json:"reply_to_id,omitempty"`
	ClientMessageID string    `json:"client_message_id,omitempty"`
}

type chatRoomPayload struct {
	ConversationID uuid.UUID `json:"conversation_id"`
	MessageID      uuid.UUID `json:"message_id,omitempty"`
}

// handleChat xử lý ba loại bản tin chat client gửi lên.
//
// Trả về false khi `type` không phải của chat, để handle() đi tiếp xuống
// nhánh mặc định.
func (c *Client) handleChat(ctx context.Context, e domainrealtime.Envelope) bool {
	switch e.Type {
	case typeChatSend, typeChatTyping, typeChatRead:
	default:
		return false
	}

	if c.hub.chat == nil {
		c.chatError("CHAT_UNAVAILABLE", "Chức năng chat chưa sẵn sàng")
		return true
	}
	// Kiểm tra quyền ĐÚNG như REST. Không kiểm ở đây thì WebSocket thành một
	// cửa sau đi vòng qua toàn bộ phân quyền của HTTP.
	if !c.actor.Can(domainauth.PermChatRead) {
		c.chatError("FORBIDDEN", "Không có quyền dùng chat")
		return true
	}

	switch e.Type {
	case typeChatSend:
		c.chatSend(ctx, e.Payload)
	case typeChatTyping:
		c.chatTyping(ctx, e.Payload)
	case typeChatRead:
		c.chatRead(ctx, e.Payload)
	}
	return true
}

func (c *Client) chatSend(ctx context.Context, raw json.RawMessage) {
	var p chatSendPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.ConversationID == uuid.Nil {
		c.chatError("BAD_MESSAGE", "Thiếu hoặc sai conversation_id")
		return
	}

	in := ucchat.SendInput{
		ConversationID:  p.ConversationID,
		Content:         p.Content,
		Kind:            domainchat.MessageKind(p.Kind),
		ClientMessageID: p.ClientMessageID,
	}
	if p.ReplyToID != "" {
		id, err := uuid.Parse(p.ReplyToID)
		if err != nil {
			c.chatError("BAD_MESSAGE", "reply_to_id không hợp lệ")
			return
		}
		in.ReplyToID = &id
	}

	// Usecase tự phát tin cho mọi thành viên, KỂ CẢ người gửi. Nhờ vậy tin
	// nhắn về tới mọi tab của người gửi theo đúng một đường, và client chỉ
	// cần một chỗ xử lý thay vì hai.
	if _, err := c.hub.chat.Send(ctx, c.actor, in); err != nil {
		c.chatError("SEND_FAILED", err.Error())
	}
}

func (c *Client) chatTyping(ctx context.Context, raw json.RawMessage) {
	var p chatRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.ConversationID == uuid.Nil {
		return
	}
	// Lỗi chỉ báo "đang nhập" thì im lặng bỏ qua: nó là tín hiệu trang trí,
	// và một bản tin lỗi cho mỗi lần gõ phím còn phiền hơn việc thiếu nó.
	_ = c.hub.chat.Typing(ctx, c.actor, p.ConversationID)
}

func (c *Client) chatRead(ctx context.Context, raw json.RawMessage) {
	var p chatRoomPayload
	if err := json.Unmarshal(raw, &p); err != nil ||
		p.ConversationID == uuid.Nil || p.MessageID == uuid.Nil {
		return
	}
	_, _ = c.hub.chat.MarkRead(ctx, c.actor, p.ConversationID, p.MessageID)
}

func (c *Client) chatError(code, message string) {
	c.sendEnvelope(domainrealtime.TypeError, domainrealtime.ErrorPayload{Code: code, Message: message})
}
