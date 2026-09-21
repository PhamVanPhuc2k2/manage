package consumer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainnotif "github.com/PhamVanPhuc2k2/manage/internal/domain/notification"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// NotificationCreator là phần module thông báo mà consumer cần.
type NotificationCreator interface {
	Create(ctx context.Context, req domainnotif.Request) error
}

// ProjectConsumer biến sự kiện của module dự án thành thông báo.
//
// Đi qua hàng đợi chứ không gọi thẳng từ usecase dự án: tạo thông báo cho cả
// danh sách người nhận là việc chậm và không ai đang ngồi chờ nó. Quan trọng
// hơn, nó tách hai module ra — module dự án không cần biết module thông báo
// tồn tại, nó chỉ phát ra một sự kiện.
type ProjectConsumer struct {
	notif NotificationCreator
}

func NewProjectConsumer(notif NotificationCreator) *ProjectConsumer {
	return &ProjectConsumer{notif: notif}
}

// eventMap gắn tên job với loại thông báo tương ứng.
var eventMap = map[string]domainnotif.Type{
	domainproject.JobTaskAssigned:      domainnotif.TypeTaskAssigned,
	domainproject.JobTaskStatusChanged: domainnotif.TypeTaskStatusChanged,
	domainproject.JobTaskMentioned:     domainnotif.TypeTaskMentioned,
	domainproject.JobTaskDueSoon:       domainnotif.TypeTaskDueSoon,
}

// Handle xử lý mọi sự kiện của module dự án.
//
// Một handler cho cả bốn loại vì payload của chúng giống hệt nhau — xem
// domainproject.Event. Chỗ khác nhau duy nhất là câu tiêu đề.
func (c *ProjectConsumer) Handle(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	typ, ok := eventMap[job.Name]
	if !ok {
		log.Warn().Str("event", job.Name).Msg("sự kiện dự án không rõ loại")
		return nil
	}

	recipients := uuidListField(job.Payload, "recipients")
	if len(recipients) == 0 {
		return nil
	}

	taskID := uuidField(job.Payload, "task_id")
	actorID := uuidField(job.Payload, "actor_id")

	req := domainnotif.Request{
		Recipients: recipients,
		Type:       typ,
		Title:      eventTitle(typ, job.Payload),
		Body:       eventBody(typ, job.Payload),
		Resource:   "task",
	}
	if taskID != nil {
		req.ResourceID = taskID
		req.Link = "/tasks/" + taskID.String()
	}
	if actorID != nil {
		req.ActorID = actorID
	}

	if err := c.notif.Create(ctx, req); err != nil {
		// Trả lỗi để RabbitMQ đưa message lại: tạo thông báo là việc đáng thử
		// lại, khác với dữ liệu hỏng ở trên (trả nil để không lặp vô hạn).
		return fmt.Errorf("tạo thông báo từ sự kiện %s: %w", job.Name, err)
	}

	// Log kèm MÃ công việc, không chỉ id: khi đi tìm "vì sao người này không
	// nhận được thông báo", mã là thứ người dùng đọc cho mình, còn id thì họ
	// không bao giờ nhìn thấy.
	log.Info().
		Str("event", job.Name).
		Str("task_code", stringField(job.Payload, "task_code")).
		Str("task_title", stringField(job.Payload, "task_title")).
		Int("recipients", len(recipients)).
		Msg("đã tạo thông báo từ sự kiện dự án")
	return nil
}

func eventTitle(typ domainnotif.Type, payload map[string]any) string {
	actor := stringField(payload, "actor_name")
	if actor == "" {
		actor = "Một thành viên"
	}

	switch typ {
	case domainnotif.TypeTaskAssigned:
		return actor + " đã giao việc cho bạn"
	case domainnotif.TypeTaskStatusChanged:
		return actor + " đã đổi trạng thái công việc"
	case domainnotif.TypeTaskMentioned:
		return actor + " đã nhắc tên bạn"
	case domainnotif.TypeTaskDueSoon:
		return "Công việc sắp đến hạn"
	}
	return typ.Label()
}

func eventBody(typ domainnotif.Type, payload map[string]any) string {
	code := stringField(payload, "task_code")
	title := stringField(payload, "task_title")

	line := title
	if code != "" {
		line = code + " — " + title
	}

	if typ == domainnotif.TypeTaskStatusChanged {
		old := stringField(payload, "old_value")
		nw := stringField(payload, "new_value")
		if old != "" || nw != "" {
			return fmt.Sprintf("%s (%s → %s)", line, old, nw)
		}
	}
	return line
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

func uuidField(payload map[string]any, key string) *uuid.UUID {
	s, _ := payload[key].(string)
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil {
		return nil
	}
	return &id
}

// uuidListField đọc một mảng id, BỎ QUA phần tử hỏng thay vì hỏng cả lượt.
//
// Một id sai định dạng trong danh sách mười người nhận không phải lý do để
// chín người còn lại mất thông báo.
func uuidListField(payload map[string]any, key string) []uuid.UUID {
	raw, _ := payload[key].([]any)
	out := make([]uuid.UUID, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil {
			continue
		}
		out = append(out, id)
	}
	return out
}
