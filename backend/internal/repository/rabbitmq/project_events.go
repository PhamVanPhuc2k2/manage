package rabbitmq

import (
	"context"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// ProjectEventPublisher đẩy sự kiện của module dự án lên hàng đợi.
//
// Vì sao đi qua hàng đợi mà không gọi thẳng: người bấm nút "giao việc" không
// nên phải chờ hệ thống gửi xong thông báo, và Phase 5 sẽ có thêm người tiêu
// thụ những sự kiện này (đẩy WebSocket, gửi mail cho người offline) mà không
// phải sửa một dòng nào trong module dự án.
type ProjectEventPublisher struct {
	publisher *JobPublisher
}

func NewProjectEventPublisher(client *mq.Client) *ProjectEventPublisher {
	return &ProjectEventPublisher{publisher: NewJobPublisher(client)}
}

func (p *ProjectEventPublisher) PublishEvent(
	ctx context.Context,
	e domainproject.Event,
) error {
	// Payload là map chứ không phải struct để khớp với hình dạng Job dùng
	// chung. Worker đọc lại bằng khoá — xem consumer/project.go.
	recipients := make([]string, 0, len(e.Recipients))
	for _, id := range e.Recipients {
		recipients = append(recipients, id.String())
	}

	return p.publisher.Publish(ctx, domainsystem.Job{
		Name: e.Name,
		Payload: map[string]any{
			"task_id":    e.TaskID.String(),
			"task_code":  e.TaskCode,
			"task_title": e.TaskTitle,
			"project_id": e.ProjectID.String(),
			"actor_id":   e.ActorID.String(),
			"actor_name": e.ActorName,
			"recipients": recipients,
			"old_value":  e.OldValue,
			"new_value":  e.NewValue,
		},
	})
}
