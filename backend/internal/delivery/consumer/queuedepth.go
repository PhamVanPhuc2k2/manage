package consumer

import (
	"context"
	"fmt"

	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// watchedQueues là những hàng đợi được theo dõi độ sâu.
//
// Chỉ hai cái, và cả hai đều trả lời một câu hỏi vận hành cụ thể:
//
//   - manage.jobs.queue tăng đều nghĩa là worker xử lý không kịp tốc độ nạp
//     vào. Không có chỉ số này thì hàng đợi ứ đọng hàng giờ mà không ai biết:
//     job vẫn "đang chạy", chỉ là chậm hơn tốc độ đến.
//   - manage.jobs.dead khác 0 nghĩa là CÓ job đã thất bại và bị bỏ. Đó luôn
//     là việc cần người xem, nên ngưỡng cảnh báo của nó là 0.
var watchedQueues = []string{mq.QueueJobs, mq.QueueJobsDead}

// SampleQueueDepth đọc số message đang chờ của các hàng đợi theo dõi.
//
// Dùng QueueDeclarePassive chứ không gọi HTTP management API của RabbitMQ:
// passive declare đi qua đúng kết nối AMQP đã có, không cần mở thêm cổng
// quản trị và không cần thêm một bộ thông tin đăng nhập nữa. Đổi lại nó chỉ
// cho biết số message chờ và số consumer — vừa đủ cho việc cảnh báo.
//
// Mỗi lần gọi mở một channel RIÊNG rồi đóng. Lý do: khi passive declare thất
// bại (hàng đợi không tồn tại), RabbitMQ đóng luôn channel đó. Dùng chung
// channel với việc consume job sẽ biến một lần đo lỗi thành mất cả luồng xử
// lý job.
func SampleQueueDepth(ctx context.Context, client *mq.Client) error {
	log := logger.FromContext(ctx)

	for _, name := range watchedQueues {
		ch, err := client.NewChannel()
		if err != nil {
			return fmt.Errorf("mở channel đo hàng đợi: %w", err)
		}

		q, err := ch.QueueDeclarePassive(name, true, false, false, false, nil)
		_ = ch.Close()

		if err != nil {
			// Không trả lỗi ra ngoài: một hàng đợi chưa tồn tại không phải lý
			// do để job đo dừng lại và bỏ luôn hàng đợi còn lại.
			log.Warn().Err(err).Str("queue", name).Msg("không đo được độ sâu hàng đợi")
			continue
		}

		metrics.QueueDepth.WithLabelValues(name).Set(float64(q.Messages))
	}
	return nil
}
