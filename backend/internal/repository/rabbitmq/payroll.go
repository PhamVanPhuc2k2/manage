package rabbitmq

import (
	"context"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// Tên job của module lương.
const (
	JobCalculatePayroll = "payroll.calculate"
	JobSendPayslip      = "mail.payslip"
)

// PayrollJobs đẩy việc tính lương sang worker.
type PayrollJobs struct {
	publisher *JobPublisher
}

func NewPayrollJobs(client *mq.Client) *PayrollJobs {
	return &PayrollJobs{publisher: NewJobPublisher(client)}
}

func (p *PayrollJobs) PublishCalculate(
	ctx context.Context,
	periodID uuid.UUID,
	requestID string,
) error {
	return p.publisher.Publish(ctx, domainsystem.Job{
		Name:      JobCalculatePayroll,
		RequestID: requestID,
		Payload:   map[string]any{"period_id": periodID.String()},
	})
}

// SendPayslip đẩy phiếu lương sang worker để gửi mail.
//
// Nội dung phiếu lương đi TRONG payload của message. Đây là dữ liệu nhạy
// cảm nằm trong hàng đợi, nên queue phải được bảo vệ như chính database —
// và tuyệt đối không log payload này ở bất cứ đâu.
func (m *Mailer) SendPayslip(ctx context.Context, email, name, subject, htmlBody string) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendPayslip,
		Payload: map[string]any{
			"email":   email,
			"name":    name,
			"subject": subject,
			"html":    htmlBody,
		},
	})
}
