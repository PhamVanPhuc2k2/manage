package consumer

import (
	"context"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// HandleProjectEvent xử lý sự kiện của module dự án.
//
// Ở Phase 2 nó mới chỉ ghi log. Đó là CÓ CHỦ Ý, không phải việc làm dở:
// thông báo thật cần bảng `notifications` và hạ tầng WebSocket của Phase 5.
// Đăng ký handler ngay từ bây giờ để:
//
//   - sự kiện không rơi vào dead-letter queue vì "không có handler"
//   - đường đi api → RabbitMQ → worker của module dự án được kiểm chứng ngay,
//     thay vì phát hiện hỏng vào lúc Phase 5 đang bận việc khác
//
// Phase 5 chỉ việc thay phần thân hàm này bằng lời gọi xuống usecase thông
// báo — danh sách người nhận đã nằm sẵn trong payload.
func HandleProjectEvent(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	recipients, _ := job.Payload["recipients"].([]any)

	log.Info().
		Str("event", job.Name).
		Str("task_code", stringField(job.Payload, "task_code")).
		Str("task_title", stringField(job.Payload, "task_title")).
		Str("actor_id", stringField(job.Payload, "actor_id")).
		Int("recipients", len(recipients)).
		Msg("nhận sự kiện dự án (Phase 5 sẽ sinh thông báo từ đây)")

	return nil
}

// stringField đọc một trường chuỗi khỏi payload, trả về rỗng nếu thiếu
// hoặc sai kiểu.
//
// Payload đi qua JSON nên mọi thứ về đây là `any`. Ép kiểu trực tiếp mà
// message hỏng sẽ panic trong worker — dùng dạng hai giá trị để hỏng dữ
// liệu chỉ làm mất một dòng log, không làm chết goroutine.
func stringField(payload map[string]any, key string) string {
	s, _ := payload[key].(string)
	return s
}
