package rabbitmq

import (
	"context"

	"github.com/google/uuid"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// JobExportAttendance dựng tệp Excel báo cáo chấm công của một tháng.
const JobExportAttendance = "attendance.export"

// AttendanceJobs đẩy việc của module chấm công sang worker. Message chỉ
// mang id — tham số và phạm vi quyền đã nằm trong bảng attendance_exports.
type AttendanceJobs struct {
	publisher *JobPublisher
}

func NewAttendanceJobs(client *mq.Client) *AttendanceJobs {
	return &AttendanceJobs{publisher: NewJobPublisher(client)}
}

func (p *AttendanceJobs) PublishAttendanceExport(ctx context.Context, exportID uuid.UUID) error {
	return p.publisher.Publish(ctx, domainsystem.Job{
		Name:    JobExportAttendance,
		Payload: map[string]any{"export_id": exportID.String()},
	})
}
