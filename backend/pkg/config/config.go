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

	CORSAllowedOrigins []string `mapstructure:"CORS_ALLOWED_ORIGINS"`

	// Địa chỉ công khai, dùng để dựng link trong mail đặt lại mật khẩu.
	PublicBaseURL string `mapstructure:"PUBLIC_BASE_URL"`

	SMTPHost string `mapstructure:"SMTP_HOST"`
	SMTPPort int    `mapstructure:"SMTP_PORT"`
	SMTPFrom string `mapstructure:"SMTP_FROM"`
	SMTPUser string `mapstructure:"SMTP_USER"`
	SMTPPass string `mapstructure:"SMTP_PASS"`
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

	setDefault("CORS_ALLOWED_ORIGINS", []string{"http://localhost", "http://localhost:3000"})
	setDefault("PUBLIC_BASE_URL", "http://localhost")

	// Mặc định trỏ vào MailHog của môi trường dev — không cần xác thực,
	// xem mail tại http://localhost:8025
	setDefault("SMTP_HOST", "mailhog")
	setDefault("SMTP_PORT", 1025)
	setDefault("SMTP_FROM", "no-reply@manage.local")
	setDefault("SMTP_USER", "")
	setDefault("SMTP_PASS", "")

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
