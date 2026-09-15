package redis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// LoginThrottle chặn dò mật khẩu.
//
// Đếm theo CẢ HAI chiều:
//   - theo IP: chặn một máy thử nhiều tài khoản
//   - theo tài khoản: chặn nhiều máy cùng thử một tài khoản
//
// Chỉ đếm theo IP thì kẻ tấn công đổi IP là thoát. Chỉ đếm theo tài khoản
// thì ai cũng khoá được tài khoản người khác bằng cách cố tình nhập sai —
// đó là tấn công từ chối dịch vụ nhắm vào một người cụ thể.
type LoginThrottle struct {
	client *goredis.Client

	maxPerAccount int
	maxPerIP      int
	window        time.Duration
}

func NewLoginThrottle(client *goredis.Client) *LoginThrottle {
	return &LoginThrottle{
		client:        client,
		maxPerAccount: 5,
		maxPerIP:      20,
		window:        15 * time.Minute,
	}
}

func accountKey(email string) string {
	return "login_fail:user:" + strings.ToLower(strings.TrimSpace(email))
}
func ipKey(ip string) string { return "login_fail:ip:" + ip }

// Check trả về thời gian còn phải chờ. Bằng 0 nghĩa là được phép thử.
func (t *LoginThrottle) Check(ctx context.Context, email, ip string) (time.Duration, error) {
	keys := []struct {
		key string
		max int
	}{
		{accountKey(email), t.maxPerAccount},
		{ipKey(ip), t.maxPerIP},
	}

	for _, k := range keys {
		n, err := t.client.Get(ctx, k.key).Int()
		if errors.Is(err, goredis.Nil) {
			continue
		}
		if err != nil {
			// Redis hỏng thì CHO PHÉP thử tiếp, không chặn người dùng.
			//
			// Chặn đăng nhập toàn hệ thống vì Redis trục trặc là tệ hơn việc
			// tạm thời mất khả năng chống dò mật khẩu — mật khẩu vẫn được
			// bcrypt bảo vệ, còn khoá cả công ty ra ngoài thì không ai làm
			// việc được.
			//
			//nolint:nilerr // fail-open có chủ ý, xem giải thích ở trên
			return 0, nil
		}
		if n >= k.max {
			ttl, err := t.client.TTL(ctx, k.key).Result()
			if err != nil || ttl < 0 {
				ttl = t.window
			}
			return ttl, nil
		}
	}
	return 0, nil
}

func (t *LoginThrottle) RecordFailure(ctx context.Context, email, ip string) error {
	pipe := t.client.TxPipeline()

	ak, ik := accountKey(email), ipKey(ip)
	pipe.Incr(ctx, ak)
	pipe.Expire(ctx, ak, t.window)
	pipe.Incr(ctx, ik)
	pipe.Expire(ctx, ik, t.window)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ghi nhận đăng nhập thất bại: %w", err)
	}
	return nil
}

// Reset xoá bộ đếm sau khi đăng nhập thành công.
func (t *LoginThrottle) Reset(ctx context.Context, email, ip string) error {
	return t.client.Del(ctx, accountKey(email), ipKey(ip)).Err()
}
