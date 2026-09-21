package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	domainrealtime "github.com/PhamVanPhuc2k2/manage/internal/domain/realtime"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// RealtimeBus hiện thực cả Broadcaster lẫn Dispatcher của module realtime.
//
// Gộp hai vai vào một kiểu vì chúng là hai đầu của CÙNG một đường ống và
// phải dùng chung đúng một exchange. Tách ra hai kiểu chỉ tạo cơ hội cho
// một bên đổi tên exchange mà bên kia không biết.
type RealtimeBus struct {
	client *mq.Client
}

func NewRealtimeBus(client *mq.Client) *RealtimeBus {
	return &RealtimeBus{client: client}
}

// Broadcast phát bản tin tới mọi instance api.
func (b *RealtimeBus) Broadcast(ctx context.Context, msg domainrealtime.Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("mã hoá bản tin realtime: %w", err)
	}

	ch := b.client.Channel()
	if ch == nil {
		return mq.ErrNotConnected
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return ch.PublishWithContext(
		ctx,
		mq.ExchangeRealtime,
		"",    // fanout bỏ qua routing key
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			ContentType: "application/json",
			// DeliveryMode Transient, NGƯỢC với job nghiệp vụ.
			//
			// Bản tin realtime chỉ có giá trị ngay lúc đó. Ghi xuống đĩa để
			// bảo đảm không mất là trả một cái giá thật (I/O) cho một lợi
			// ích không tồn tại: nếu RabbitMQ restart, thứ người dùng cần
			// không phải là thông báo cũ được phát lại, mà là client tự nối
			// lại và tải trạng thái mới.
			DeliveryMode: amqp.Transient,
			Timestamp:    time.Now(),
			Body:         body,
		},
	)
}

// Subscribe nhận bản tin realtime cho tới khi ctx bị huỷ.
//
// Mỗi instance api tạo một queue RIÊNG, không tên (RabbitMQ tự sinh), và
// queue đó tự biến mất khi instance ngắt kết nối. Đây là điểm khác biệt cốt
// lõi so với queue job: job dùng CHUNG một queue để chia việc (mỗi message
// một worker xử lý), còn realtime cần MỌI instance đều nhận được BẢN SAO
// của cùng một bản tin — vì không ai biết người nhận đang nối vào đâu.
func (b *RealtimeBus) Subscribe(
	ctx context.Context,
	handle func(domainrealtime.Message),
) error {
	ch := b.client.Channel()
	if ch == nil {
		return mq.ErrNotConnected
	}

	q, err := ch.QueueDeclare(
		"",    // tên rỗng: để RabbitMQ tự sinh tên duy nhất
		false, // durable = false: queue của một instance không cần sống sót
		true,  // autoDelete: instance ngắt là queue biến mất
		true,  // exclusive: không ai khác được dùng queue này
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("khai báo queue realtime: %w", err)
	}

	if err := ch.QueueBind(q.Name, "", mq.ExchangeRealtime, false, nil); err != nil {
		return fmt.Errorf("bind queue realtime: %w", err)
	}

	deliveries, err := ch.Consume(
		q.Name,
		"",
		true, // autoAck = true, NGƯỢC với queue job.
		//      Bản tin realtime không đáng để ack thủ công: nếu xử lý lỗi
		//      thì phát lại cũng vô nghĩa (xem ghi chú DeliveryMode), còn
		//      ack thủ công thì thêm một vòng mạng cho mỗi bản tin.
		true,  // exclusive
		false, // noLocal
		false, // noWait
		nil,
	)
	if err != nil {
		return fmt.Errorf("đăng ký consume realtime: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case d, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("kênh consume realtime đã đóng")
			}
			var msg domainrealtime.Message
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				continue // bản tin hỏng: bỏ qua, không làm chết vòng lặp
			}
			handle(msg)
		}
	}
}
