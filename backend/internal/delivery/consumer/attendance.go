package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	ucatt "github.com/PhamVanPhuc2k2/manage/internal/usecase/attendance"
)

// AttendanceConsumer xử lý job của module chấm công.
type AttendanceConsumer struct {
	uc *ucatt.Usecase
}

func NewAttendanceConsumer(uc *ucatt.Usecase) *AttendanceConsumer {
	return &AttendanceConsumer{uc: uc}
}

// HandleExport dựng tệp Excel cho một lượt xuất. Chạy lại an toàn: tệp
// được dựng lại từ đầu và ghi đè.
func (c *AttendanceConsumer) HandleExport(ctx context.Context, job domainsystem.Job) error {
	raw, _ := job.Payload["export_id"].(string)
	id, err := uuid.Parse(raw)
	if err != nil {
		return fmt.Errorf("export_id không hợp lệ: %q", raw)
	}
	if err := c.uc.ProcessExport(ctx, id); err != nil {
		return fmt.Errorf("xuất chấm công %s: %w", id, err)
	}
	return nil
}
