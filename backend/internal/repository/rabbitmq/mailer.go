package rabbitmq

import (
	"context"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

// Tên các job gửi mail. Worker đăng ký handler theo đúng các tên này.
const (
	JobSendPasswordReset      = "mail.password_reset"
	JobSendSuspiciousActivity = "mail.suspicious_activity"
	JobSendWelcome            = "mail.welcome"
	JobSendLoginOTP           = "mail.login_otp"
)

// Mailer đẩy việc gửi mail sang worker.
//
// Gửi mail qua hàng đợi chứ không gọi SMTP trực tiếp trong request: SMTP có
// thể chậm vài giây hoặc timeout, và người dùng không nên phải chờ điều đó
// khi bấm "quên mật khẩu".
type Mailer struct {
	publisher *JobPublisher
}

func NewMailer(client *mq.Client) *Mailer {
	return &Mailer{publisher: NewJobPublisher(client)}
}

func (m *Mailer) SendPasswordReset(ctx context.Context, email, name, resetURL string) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendPasswordReset,
		Payload: map[string]any{
			"email":     email,
			"name":      name,
			"reset_url": resetURL,
		},
	})
}

func (m *Mailer) SendSuspiciousActivity(ctx context.Context, email, name, ip, userAgent string) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendSuspiciousActivity,
		Payload: map[string]any{
			"email":      email,
			"name":       name,
			"ip":         ip,
			"user_agent": userAgent,
		},
	})
}

// SendLoginOTP đẩy mã đăng nhập sang worker.
//
// Mã đi qua hàng đợi như mọi mail khác, nhưng đây là mail DUY NHẤT mà người
// dùng đang ngồi chờ ngay lúc đó. Hàng đợi tắc thì họ không đăng nhập được —
// đó là cái giá của việc bắt buộc OTP, và là lý do có công tắc AUTH_OTP_ENABLED.
//
// Mã nằm trong payload của message. Đừng log payload này ở bất cứ đâu.
func (m *Mailer) SendLoginOTP(
	ctx context.Context,
	email, name, code string,
	ttlMinutes int,
	ip string,
) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendLoginOTP,
		Payload: map[string]any{
			"email":       email,
			"name":        name,
			"code":        code,
			"ttl_minutes": ttlMinutes,
			"ip":          ip,
		},
	})
}

func (m *Mailer) SendWelcome(ctx context.Context, email, name, tempPassword string) error {
	return m.publisher.Publish(ctx, domainsystem.Job{
		Name: JobSendWelcome,
		Payload: map[string]any{
			"email":         email,
			"name":          name,
			"temp_password": tempPassword,
		},
	})
}
