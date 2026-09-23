package rabbitmq

import (
	"context"

	"github.com/google/uuid"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// Pusher đẩy bản tin realtime từ một tiến trình KHÔNG giữ kết nối WebSocket.
//
// Worker là trường hợp duy nhất: nó tạo thông báo từ job nền nhưng không có
// hub nào cả. Phát thẳng vào exchange fanout để mọi instance api nhận và
// chuyển tiếp cho người đang nối vào chính nó.
//
// api dùng ws.Pusher (bọc Hub) thay vì kiểu này: ở đó gửi cho người ngồi
// cùng instance không cần đi vòng qua RabbitMQ.
type Pusher struct {
	bus *RealtimeBus
}

func NewPusher(client *mq.Client) *Pusher {
	return &Pusher{bus: NewRealtimeBus(client)}
}

func (p *Pusher) publish(
	ctx context.Context,
	recipients []uuid.UUID,
	eventType string,
	payload any,
) {
	if len(recipients) == 0 {
		return
	}
	log := logger.FromContext(ctx)

	env, err := domainrealtime.NewEnvelope(eventType, payload, "")
	if err != nil {
		log.Warn().Err(err).Str("type", eventType).Msg("không mã hoá được bản tin realtime")
		return
	}

	// Nuốt lỗi có chủ ý: dữ liệu đã ghi vào database rồi, và làm job thất bại
	// (rồi chạy lại, rồi ghi trùng) chỉ vì đẩy realtime hỏng là đánh đổi sai.
	if err := p.bus.Broadcast(ctx, domainrealtime.Message{
		Recipients: recipients,
		Envelope:   env,
	}); err != nil {
		log.Warn().Err(err).Str("type", eventType).Msg("không phát được bản tin realtime")
	}
}

func (p *Pusher) PushNotification(ctx context.Context, recipients []uuid.UUID, payload any) {
	p.publish(ctx, recipients, domainrealtime.TypeNotification, payload)
}

func (p *Pusher) PushBadge(ctx context.Context, employeeID uuid.UUID, unread int) {
	p.publish(ctx, []uuid.UUID{employeeID}, domainrealtime.TypeNotificationBadge,
		map[string]int{"unread": unread})
}

func (p *Pusher) PushChat(
	ctx context.Context,
	recipients []uuid.UUID,
	eventType string,
	payload any,
) {
	p.publish(ctx, recipients, eventType, payload)
}

// PushCall hiện thực call.Signaler cho worker.
//
// Worker cần nó cho job dọn cuộc gọi quá hạn: người đang đổ chuông phải
// thấy màn hình gọi tắt đi, chứ không ngồi nghe chuông mãi.
func (p *Pusher) PushCall(
	ctx context.Context,
	recipients []uuid.UUID,
	eventType string,
	payload any,
) {
	p.publish(ctx, recipients, eventType, payload)
}
