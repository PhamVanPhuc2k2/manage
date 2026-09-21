package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	ucpay "github.com/PhamVanPhuc2k2/manage/internal/usecase/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// PayrollConsumer xử lý job tính lương.
type PayrollConsumer struct {
	uc *ucpay.Usecase
}

func NewPayrollConsumer(uc *ucpay.Usecase) *PayrollConsumer {
	return &PayrollConsumer{uc: uc}
}

// HandleCalculate chạy máy tính lương cho một kỳ.
//
// Chạy ở worker vì kỳ lương của công ty vài trăm người mất vài giây tới vài
// chục giây — quá lâu cho một request HTTP, và người dùng đóng tab giữa
// chừng sẽ để kỳ lương kẹt ở trạng thái 'calculating'.
//
// Job này PHẢI chịu được chạy lại: RabbitMQ có thể giao lại message khi
// worker chết giữa chừng. ReplaceForPeriod xoá sạch phiếu cũ rồi ghi bộ mới
// trong một giao dịch, nên chạy hai lần cho ra đúng một bộ kết quả.
func (c *PayrollConsumer) HandleCalculate(
	ctx context.Context,
	job domainsystem.Job,
) error {
	log := logger.FromContext(ctx)

	raw, _ := job.Payload["period_id"].(string)
	periodID, err := uuid.Parse(raw)
	if err != nil {
		// Message hỏng: trả lỗi để dispatcher đẩy sang dead-letter queue.
		// Requeue một message không bao giờ phân tích được chỉ tạo vòng lặp.
		return fmt.Errorf("period_id không hợp lệ: %q", raw)
	}

	res, err := c.uc.Calculate(ctx, periodID)
	if err != nil {
		return fmt.Errorf("tính lương kỳ %s: %w", periodID, err)
	}

	log.Info().
		Str("period_id", periodID.String()).
		Int("processed", res.Processed).
		Int("skipped", res.Skipped).
		Msg("đã tính xong kỳ lương")

	return nil
}
