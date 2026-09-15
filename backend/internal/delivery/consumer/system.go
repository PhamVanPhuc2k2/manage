package consumer

import (
	"context"

	domainsystem "github.com/yourorg/manage/internal/domain/system"
	"github.com/yourorg/manage/pkg/logger"
)

// HandleSystemPing xử lý job system.ping.
//
// Ở Phase 0 nó chỉ ghi log — nhưng chính dòng log đó là bằng chứng
// chuỗi api → RabbitMQ → worker đã thông, và request_id truyền được qua.
func HandleSystemPing(ctx context.Context, job domainsystem.Job) error {
	// Gán ra biến trước: zerolog.Logger có method với pointer receiver,
	// gọi trực tiếp trên giá trị trả về từ hàm sẽ không biên dịch được.
	log := logger.FromContext(ctx)
	log.Info().
		Str("job", job.Name).
		Str("request_id", job.RequestID).
		Interface("payload", job.Payload).
		Msg("worker đã xử lý job")
	return nil
}
