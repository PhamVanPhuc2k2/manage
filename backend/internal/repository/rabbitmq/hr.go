package rabbitmq

import (
	"context"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// JobImportEmployees chạy một lượt nhập nhân viên hàng loạt.
const JobImportEmployees = "hr.import_employees"

// HRJobs đẩy việc của module nhân sự sang worker.
//
// Message chỉ mang id của lượt nhập, không mang dữ liệu nhân viên: các dòng
// đã nằm trong bảng employee_imports, và thông tin cá nhân không nên đi qua
// hàng đợi khi không cần.
type HRJobs struct {
	publisher *JobPublisher
}

func NewHRJobs(client *mq.Client) *HRJobs {
	return &HRJobs{publisher: NewJobPublisher(client)}
}

func (p *HRJobs) PublishEmployeeImport(ctx context.Context, importID uuid.UUID, requestID string) error {
	return p.publisher.Publish(ctx, domainsystem.Job{
		Name:      JobImportEmployees,
		RequestID: requestID,
		Payload:   map[string]any{"import_id": importID.String()},
	})
}
