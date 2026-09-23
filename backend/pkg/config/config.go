// Package config đọc cấu hình từ biến môi trường và validate ngay lúc khởi động.
// Nguyên tắc: thà chết ngay lúc start còn hơn chạy được rồi lỗi lúc 2 giờ sáng.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	AppName         string        `mapstructure:"APP_NAME"`
	Env             string        `mapstructure:"ENV"`
	LogLevel        string        `mapstructure:"LOG_LEVEL"`
	HTTPPort        int           `mapstructure:"HTTP_PORT"`
	ShutdownTimeout time.Duration `mapstructure:"SHUTDOWN_TIMEOUT"`

	PostgresDSN     string `mapstructure:"POSTGRES_DSN"`
	PostgresMaxConn int32  `mapstructure:"POSTGRES_MAX_CONN"`

	RedisAddr     string `mapstructure:"REDIS_ADDR"`
	RedisPassword string `mapstructure:"REDIS_PASSWORD"`
	RedisDB       int    `mapstructure:"REDIS_DB"`

	RabbitMQURL string `mapstructure:"RABBITMQ_URL"`
	// Số message worker xử lý song song. Đặt 1 khi cần giữ đúng thứ tự.
	WorkerConcurrency int `mapstructure:"WORKER_CONCURRENCY"`

	JWTSecret     string        `mapstructure:"JWT_SECRET"`
	JWTAccessTTL  time.Duration `mapstructure:"JWT_ACCESS_TTL"`
	JWTRefreshTTL time.Duration `mapstructure:"JWT_REFRESH_TTL"`

	// AuthOTPEnabled bật bước nhập mã xác minh sau khi nhập đúng mật khẩu.
	//
	// Mặc định BẬT. Tắt được để chạy kiểm thử tự động và để cứu hoả khi SMTP
	// chết — nhưng tắt nghĩa là mật khẩu lộ là vào được hệ thống.
	AuthOTPEnabled bool `mapstructure:"AUTH_OTP_ENABLED"`

	CORSAllowedOrigins []string `mapstructure:"CORS_ALLOWED_ORIGINS"`

	// Địa chỉ công khai, dùng để dựng link trong mail đặt lại mật khẩu.
	PublicBaseURL string `mapstructure:"PUBLIC_BASE_URL"`

	SMTPHost string `mapstructure:"SMTP_HOST"`
	SMTPPort int    `mapstructure:"SMTP_PORT"`
	SMTPFrom string `mapstructure:"SMTP_FROM"`
	SMTPUser string `mapstructure:"SMTP_USER"`
	SMTPPass string `mapstructure:"SMTP_PASS"`

	// Cloudflare R2. Để trống thì chức năng tệp tắt, hệ thống vẫn chạy.
	R2AccountID       string `mapstructure:"R2_ACCOUNT_ID"`
	R2AccessKeyID     string `mapstructure:"R2_ACCESS_KEY_ID"`
	R2SecretAccessKey string `mapstructure:"R2_SECRET_ACCESS_KEY"`
	R2Bucket          string `mapstructure:"R2_BUCKET"`
	R2PublicURL       string `mapstructure:"R2_PUBLIC_URL"`

	// LiveKit — máy chủ media cho gọi thoại/video. Để trống thì chức năng
	// gọi tắt, phần còn lại của hệ thống vẫn chạy bình thường.
	//
	// LiveKitURL là địa chỉ NỘI BỘ để backend gọi API quản trị, còn
	// LiveKitPublicURL là địa chỉ WebSocket gửi xuống trình duyệt. Hai
	// cái này khác nhau sau NAT, và lấy nhầm là trình duyệt gọi vào một
	// tên máy chỉ tồn tại trong mạng Docker.
	LiveKitAPIKey    string `mapstructure:"LIVEKIT_API_KEY"`
	LiveKitAPISecret string `mapstructure:"LIVEKIT_API_SECRET"`
	LiveKitURL       string `mapstructure:"LIVEKIT_URL"`
	LiveKitPublicURL string `mapstructure:"LIVEKIT_PUBLIC_URL"`

	// STUN chỉ giúp máy tự biết địa chỉ công khai của mình; TURN mới là thứ
	// cứu được người sau NAT đối xứng và mạng chặn UDP. Thiếu TURN thì
	// một số người kết nối được còn số khác thì không, tùy mạng họ ngồi.
	STUNURLs   []string `mapstructure:"STUN_URLS"`
	TURNURLs   []string `mapstructure:"TURN_URLS"`
	TURNSecret string   `mapstructure:"TURN_SECRET"`
}

func (c Config) IsProduction() bool { return c.Env == "production" }

// Load đọc cấu hình. appName dùng làm nhãn trong log ("api" hoặc "worker").
func Load(appName string) (*Config, error) {
	v := viper.New()
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// setDefault vừa đặt giá trị mặc định, vừa ĐĂNG KÝ key với viper.
	// Key chưa đăng ký thì Unmarshal bỏ qua biến môi trường tương ứng —
	// đây là cái bẫy hay gặp nhất khi dùng viper với env.
	setDefault := func(key string, value any) {
		v.SetDefault(key, value)
		_ = v.BindEnv(key)
	}

	setDefault("APP_NAME", appName)
	setDefault("ENV", "development")
	setDefault("LOG_LEVEL", "debug")
	setDefault("HTTP_PORT", 8080)
	setDefault("SHUTDOWN_TIMEOUT", "30s")

	setDefault("POSTGRES_DSN", "")
	setDefault("POSTGRES_MAX_CONN", 20)

	setDefault("REDIS_ADDR", "redis:6379")
	setDefault("REDIS_PASSWORD", "")
	setDefault("REDIS_DB", 0)

	setDefault("RABBITMQ_URL", "")
	setDefault("WORKER_CONCURRENCY", 5)

	setDefault("JWT_SECRET", "")
	setDefault("JWT_ACCESS_TTL", "15m")
	setDefault("JWT_REFRESH_TTL", "168h")
	setDefault("AUTH_OTP_ENABLED", true)

	setDefault("CORS_ALLOWED_ORIGINS", []string{"http://localhost", "http://localhost:3000"})
	setDefault("PUBLIC_BASE_URL", "http://localhost")

	// Mặc định trỏ vào MailHog của môi trường dev — không cần xác thực,
	// xem mail tại http://localhost:8025
	setDefault("SMTP_HOST", "mailhog")
	setDefault("SMTP_PORT", 1025)
	setDefault("SMTP_FROM", "no-reply@manage.local")
	setDefault("SMTP_USER", "")
	setDefault("SMTP_PASS", "")

	setDefault("R2_ACCOUNT_ID", "")
	setDefault("R2_ACCESS_KEY_ID", "")
	setDefault("R2_SECRET_ACCESS_KEY", "")
	setDefault("R2_BUCKET", "")
	setDefault("R2_PUBLIC_URL", "")

	setDefault("LIVEKIT_API_KEY", "")
	setDefault("LIVEKIT_API_SECRET", "")
	setDefault("LIVEKIT_URL", "http://livekit:7880")
	setDefault("LIVEKIT_PUBLIC_URL", "ws://localhost:7880")

	// Mặc định dùng STUN công cộng của Google cho môi trường dev.
	// Ở production nên trỏ vào STUN của chính mình: mỗi lần gọi là một
	// lần lộ địa chỉ IP nhân viên cho bên thứ ba.
	setDefault("STUN_URLS", []string{"stun:stun.l.google.com:19302"})
	setDefault("TURN_URLS", []string{})
	setDefault("TURN_SECRET", "")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("giải mã cấu hình: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c Config) validate() error {
	var missing []string

	if c.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if c.RabbitMQURL == "" {
		missing = append(missing, "RABBITMQ_URL")
	}
	if c.IsProduction() {
		if c.JWTSecret == "" || strings.HasPrefix(c.JWTSecret, "doi_chuoi_nay") {
			missing = append(missing, "JWT_SECRET (không được dùng giá trị mặc định ở production)")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("thiếu cấu hình bắt buộc: %s", strings.Join(missing, ", "))
	}
	return nil
}
