package ws

import (
	"context"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
)

// Pusher là bộ chuyển đổi giữa Hub và các cổng Pusher của module nghiệp vụ.
//
// Tồn tại để module thông báo và module chat KHÔNG phải biết về Envelope,
// Message hay RabbitMQ. Chúng khai báo một interface hẹp theo nhu cầu của
// mình, và composition root cắm kiểu này vào.
type Pusher struct {
	hub *Hub
}

func NewPusher(hub *Hub) *Pusher { return &Pusher{hub: hub} }

// publish gói payload rồi phát ra toàn hệ thống.
//
// Nuốt lỗi mã hoá thay vì trả lên trên: các cổng Pusher cố ý không trả lỗi,
// vì dữ liệu đã ghi vào database rồi và làm hỏng cả lời gọi nghiệp vụ chỉ vì
// đẩy realtime trục trặc là đánh đổi sai.
func (p *Pusher) publish(
	ctx context.Context,
	recipients []uuid.UUID,
	eventType string,
	payload any,
) {
	if p.hub == nil || len(recipients) == 0 {
		return
	}

	env, err := domainrealtime.NewEnvelope(eventType, payload, chimw.GetReqID(ctx))
	if err != nil {
		p.hub.log.Error().Err(err).Str("type", eventType).
			Msg("không mã hoá được bản tin realtime")
		return
	}

	p.hub.Publish(ctx, domainrealtime.Message{Recipients: recipients, Envelope: env})
}

// PushNotification hiện thực notification.Pusher.
func (p *Pusher) PushNotification(ctx context.Context, recipients []uuid.UUID, payload any) {
	p.publish(ctx, recipients, domainrealtime.TypeNotification, payload)
}

// PushBadge hiện thực notification.Pusher.
func (p *Pusher) PushBadge(ctx context.Context, employeeID uuid.UUID, unread int) {
	p.publish(ctx, []uuid.UUID{employeeID}, domainrealtime.TypeNotificationBadge,
		map[string]int{"unread": unread})
}

// PushChat hiện thực chat.Pusher. Loại sự kiện do module chat quyết định, nên
// nó là tham số chứ không phải hằng số ở đây.
func (p *Pusher) PushChat(
	ctx context.Context,
	recipients []uuid.UUID,
	eventType string,
	payload any,
) {
	p.publish(ctx, recipients, eventType, payload)
}

// PushChatBadge gửi tổng số tin chưa đọc.
func (p *Pusher) PushChatBadge(ctx context.Context, employeeID uuid.UUID, unread int) {
	p.publish(ctx, []uuid.UUID{employeeID}, domainrealtime.TypeChatBadge,
		map[string]int{"unread": unread})
}
