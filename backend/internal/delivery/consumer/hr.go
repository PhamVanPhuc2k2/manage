package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
)

// HRConsumer xử lý job của module nhân sự.
type HRConsumer struct {
	uc *uchr.Usecase
}

func NewHRConsumer(uc *uchr.Usecase) *HRConsumer {
	return &HRConsumer{uc: uc}
}

// HandleImport chạy một lượt nhập nhân viên hàng loạt.
//
// Chạy lại an toàn: ProcessImport làm tiếp từ dòng đầu tiên chưa có kết
// quả, nên message được giao lại không tạo trùng nhân viên.
func (c *HRConsumer) HandleImport(ctx context.Context, job domainsystem.Job) error {
	raw, _ := job.Payload["import_id"].(string)
	id, err := uuid.Parse(raw)
	if err != nil {
		return fmt.Errorf("import_id không hợp lệ: %q", raw)
	}
	if err := c.uc.ProcessImport(ctx, id); err != nil {
		return fmt.Errorf("nhập nhân viên %s: %w", id, err)
	}
	return nil
}
