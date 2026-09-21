package rabbitmq

import (
	"context"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
)

// JobSendNotification là email nhắc một thông báo quan trọng chưa đọc.
const JobSendNotification = "mail.notification"

// SendNotification hiện thực notification.Mailer.
//
// Đường dẫn đi kèm là đường dẫn TƯƠNG ĐỐI, đúng như lưu trong database.
// Worker ghép với base URL cấu hình sẵn — làm ở đây sẽ khiến mỗi chỗ tạo
// thông báo phải biết tên miền công khai của hệ thống.
func (m *Mailer) SendNotification(ctx context.Context, email, name, title, body, link string) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendNotification,
		Payload: map[string]any{
			"email": email,
			"name":  name,
			"title": title,
			"body":  body,
			"link":  link,
		},
	})
}
