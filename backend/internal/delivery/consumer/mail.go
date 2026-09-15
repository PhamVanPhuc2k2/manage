package consumer

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// MailSender gửi mail qua SMTP.
//
// Ở dev trỏ vào MailHog (không cần xác thực, xem tại http://localhost:8025).
// Ở production trỏ vào SMTP thật.
type MailSender struct {
	host string
	port int
	from string
	user string
	pass string
}

func NewMailSender(host string, port int, from, user, pass string) *MailSender {
	return &MailSender{host: host, port: port, from: from, user: user, pass: pass}
}

func (s *MailSender) send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	msg := strings.Join([]string{
		"From: " + s.from,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body,
	}, "\r\n")

	// MailHog không yêu cầu xác thực. Chỉ dùng auth khi có cấu hình user,
	// nếu không smtp.SendMail sẽ lỗi vì server từ chối lệnh AUTH.
	var auth smtp.Auth
	if s.user != "" {
		auth = smtp.PlainAuth("", s.user, s.pass, s.host)
	}
	return smtp.SendMail(addr, auth, s.from, []string{to}, []byte(msg))
}

func (s *MailSender) HandlePasswordReset(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	email, _ := job.Payload["email"].(string)
	name, _ := job.Payload["name"].(string)
	resetURL, _ := job.Payload["reset_url"].(string)

	if email == "" || resetURL == "" {
		// Dữ liệu hỏng: trả nil để message không quay lại hàng đợi vô hạn.
		log.Error().Interface("payload", job.Payload).Msg("job gửi mail thiếu dữ liệu")
		return nil
	}

	body := fmt.Sprintf(`Xin chào %s,

Bạn (hoặc ai đó) vừa yêu cầu đặt lại mật khẩu cho tài khoản này.

Bấm vào liên kết dưới đây để đặt mật khẩu mới:
%s

Liên kết có hiệu lực trong 30 phút và chỉ dùng được một lần.

Nếu bạn không yêu cầu điều này, hãy bỏ qua email. Mật khẩu hiện tại của bạn
vẫn giữ nguyên.

--
Hệ thống Quản lý Công ty`, name, resetURL)

	if err := s.send(email, "Đặt lại mật khẩu", body); err != nil {
		return fmt.Errorf("gửi mail đặt lại mật khẩu: %w", err)
	}
	log.Info().Str("to", email).Msg("đã gửi mail đặt lại mật khẩu")
	return nil
}

func (s *MailSender) HandleSuspiciousActivity(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	email, _ := job.Payload["email"].(string)
	name, _ := job.Payload["name"].(string)
	ip, _ := job.Payload["ip"].(string)
	ua, _ := job.Payload["user_agent"].(string)

	if email == "" {
		return nil
	}

	body := fmt.Sprintf(`Xin chào %s,

Hệ thống phát hiện dấu hiệu bất thường với tài khoản của bạn: một phiên đăng
nhập cũ được sử dụng lại. Điều này có thể do thông tin đăng nhập bị lộ.

Vì lý do an toàn, TOÀN BỘ phiên đăng nhập của bạn đã bị huỷ. Vui lòng đăng
nhập lại và cân nhắc đổi mật khẩu.

Thông tin ghi nhận:
  Địa chỉ IP: %s
  Trình duyệt: %s

Nếu đây không phải là bạn, hãy đổi mật khẩu ngay và báo bộ phận kỹ thuật.

--
Hệ thống Quản lý Công ty`, name, ip, ua)

	if err := s.send(email, "Cảnh báo bảo mật tài khoản", body); err != nil {
		return fmt.Errorf("gửi mail cảnh báo: %w", err)
	}
	log.Warn().Str("to", email).Msg("đã gửi mail cảnh báo bảo mật")
	return nil
}

func (s *MailSender) HandleLoginOTP(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	email, _ := job.Payload["email"].(string)
	name, _ := job.Payload["name"].(string)
	code, _ := job.Payload["code"].(string)
	ip, _ := job.Payload["ip"].(string)

	// JSON không có kiểu số nguyên: mọi số về tới đây đều là float64.
	ttl := 5
	if v, ok := job.Payload["ttl_minutes"].(float64); ok && v > 0 {
		ttl = int(v)
	}

	if email == "" || code == "" {
		log.Error().Msg("job gửi mã đăng nhập thiếu dữ liệu")
		return nil
	}

	body := fmt.Sprintf(`Xin chào %s,

Mã xác minh đăng nhập của bạn là:

    %s

Mã có hiệu lực trong %d phút và chỉ dùng được một lần.

Yêu cầu đăng nhập đến từ địa chỉ IP: %s

Nếu bạn KHÔNG đăng nhập, ai đó đang giữ mật khẩu của bạn. Hãy đổi mật khẩu
ngay và báo bộ phận kỹ thuật. Không đưa mã này cho bất kỳ ai, kể cả người
tự xưng là nhân viên hỗ trợ.

--
Hệ thống Quản lý Công ty`, name, code, ttl, ip)

	if err := s.send(email, "Mã xác minh đăng nhập", body); err != nil {
		return fmt.Errorf("gửi mã đăng nhập: %w", err)
	}

	// Chỉ log người nhận, TUYỆT ĐỐI không log mã. Log thường được gom về
	// một nơi mà nhiều người đọc được — mã nằm trong đó là OTP thành vô nghĩa.
	log.Info().Str("to", email).Msg("đã gửi mã đăng nhập")
	return nil
}

func (s *MailSender) HandleWelcome(ctx context.Context, job domainsystem.Job) error {
	log := logger.FromContext(ctx)

	email, _ := job.Payload["email"].(string)
	name, _ := job.Payload["name"].(string)
	pass, _ := job.Payload["temp_password"].(string)

	if email == "" || pass == "" {
		return nil
	}

	body := fmt.Sprintf(`Xin chào %s,

Tài khoản của bạn trên hệ thống quản lý công ty đã được tạo.

  Email đăng nhập: %s
  Mật khẩu tạm:    %s

Bạn sẽ được yêu cầu đổi mật khẩu ngay trong lần đăng nhập đầu tiên.

--
Hệ thống Quản lý Công ty`, name, email, pass)

	if err := s.send(email, "Tài khoản của bạn đã được tạo", body); err != nil {
		return fmt.Errorf("gửi mail chào mừng: %w", err)
	}
	log.Info().Str("to", email).Msg("đã gửi mail chào mừng")
	return nil
}
