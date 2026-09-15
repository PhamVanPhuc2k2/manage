package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
)

// OTPStore lưu thử thách OTP của bước đăng nhập thứ hai.
//
// Mỗi bản ghi là một Hash gồm:
//
//	user_id     — người đang đăng nhập
//	email       — để gửi lại mã mà không phải tra database
//	ip, ua, dev — bối cảnh của BƯỚC MỘT, dùng cho phiên cấp ra ở bước hai
//	code_hash   — băm của mã, không bao giờ lưu mã thô
//	attempts    — số lần nhập sai
//	resends     — số lần đã gửi lại
//	sent_at     — thời điểm gửi gần nhất (mili giây), để chặn gửi lại quá dày
//
// TTL của khoá chính là hạn của mã. Hết hạn thì Redis tự xoá — không cần
// job dọn rác, cũng không có bản ghi cũ nào nằm lại chờ bị dùng nhầm.
type OTPStore struct {
	client *goredis.Client
}

func NewOTPStore(client *goredis.Client) *OTPStore {
	return &OTPStore{client: client}
}

func otpKey(hash string) string { return "otp:" + hash }

func (s *OTPStore) Create(
	ctx context.Context,
	challengeHash string,
	c domainauth.OTPChallenge,
	codeHash string,
	ttl time.Duration,
) error {
	key := otpKey(challengeHash)

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key,
		"user_id", c.UserID.String(),
		"email", c.Email,
		"ip", c.IP,
		"ua", c.UserAgent,
		"dev", c.DeviceName,
		"code_hash", codeHash,
		"attempts", "0",
		"resends", "0",
		"sent_at", time.Now().UnixMilli())
	pipe.Expire(ctx, key, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("lưu thử thách OTP: %w", err)
	}
	return nil
}

// verifyScript so mã và đếm số lần sai nguyên khối.
//
// So sánh ở đây là so BĂM với BĂM, không phải mã với mã. Lua không so sánh
// theo thời gian hằng, nhưng kẻ tấn công đo được thời gian cũng chỉ biết
// băm của mình trùng băm lưu tới đâu — muốn dùng được điều đó phải giải
// ngược SHA-256. Tấm chắn thật vẫn là bộ đếm 5 lần ngay dưới đây.
//
// Trả về:
//
//	{0}                                — không tồn tại hoặc đã hết hạn
//	{1, user_id, email, ip, ua, dev}   — đúng mã, thử thách đã bị xoá
//	{2}                                — sai mã lần cuối, thử thách đã bị xoá
//	{3, attemptsLeft}                  — sai mã, còn được thử tiếp
const otpVerifyScript = `
local stored = redis.call('HGET', KEYS[1], 'code_hash')
if not stored then
    return {0}
end

if stored == ARGV[1] then
    local uid = redis.call('HGET', KEYS[1], 'user_id')
    local email = redis.call('HGET', KEYS[1], 'email') or ''
    local ip = redis.call('HGET', KEYS[1], 'ip') or ''
    local ua = redis.call('HGET', KEYS[1], 'ua') or ''
    local dev = redis.call('HGET', KEYS[1], 'dev') or ''
    redis.call('DEL', KEYS[1])
    return {1, uid, email, ip, ua, dev}
end

local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
local maxAttempts = tonumber(ARGV[2])
if attempts >= maxAttempts then
    redis.call('DEL', KEYS[1])
    return {2}
end
return {3, maxAttempts - attempts}
`

func (s *OTPStore) Verify(
	ctx context.Context,
	challengeHash, codeHash string,
	maxAttempts int,
) (domainauth.OTPVerified, int, error) {
	var out domainauth.OTPVerified

	res, err := s.client.Eval(ctx, otpVerifyScript,
		[]string{otpKey(challengeHash)}, codeHash, maxAttempts).Slice()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return out, 0, fmt.Errorf("xác minh OTP: %w", err)
	}
	if len(res) == 0 {
		return out, 0, domainauth.ErrOTPNotFound
	}

	code, _ := res[0].(int64)
	switch code {
	case 0:
		return out, 0, domainauth.ErrOTPNotFound
	case 2:
		return out, 0, domainauth.ErrOTPTooManyAttempts
	case 3:
		left, _ := res[1].(int64)
		return out, int(left), domainauth.ErrOTPWrongCode
	}

	if len(res) < 6 {
		return out, 0, domainauth.ErrOTPNotFound
	}
	uidStr, _ := res[1].(string)
	userID, parseErr := uuid.Parse(uidStr)
	if parseErr != nil {
		return out, 0, domainauth.ErrOTPNotFound
	}

	email, _ := res[2].(string)
	ip, _ := res[3].(string)
	ua, _ := res[4].(string)
	dev, _ := res[5].(string)

	out.Challenge = domainauth.OTPChallenge{
		UserID:     userID,
		Email:      email,
		IP:         ip,
		UserAgent:  ua,
		DeviceName: dev,
	}
	return out, 0, nil
}

// otpResendScript thay mã mới, giữ nguyên bộ đếm số lần nhập sai.
//
// Trả về:
//
//	{0}                              — không tồn tại hoặc đã hết hạn
//	{1, user_id, email, ip, ua, dev} — đã thay mã
//	{2, giây_còn_phải_chờ}           — bấm gửi lại quá sớm
//	{3}                              — đã gửi lại quá số lần cho phép
const otpResendScript = `
local sentAt = redis.call('HGET', KEYS[1], 'sent_at')
if not sentAt then
    return {0}
end

local now = tonumber(ARGV[2])
local cooldown = tonumber(ARGV[3])
local elapsed = now - tonumber(sentAt)
if elapsed < cooldown then
    return {2, math.ceil((cooldown - elapsed) / 1000)}
end

local resends = tonumber(redis.call('HGET', KEYS[1], 'resends') or '0')
if resends >= tonumber(ARGV[4]) then
    return {3}
end

redis.call('HSET', KEYS[1], 'code_hash', ARGV[1], 'sent_at', ARGV[2])
redis.call('HINCRBY', KEYS[1], 'resends', 1)

local uid = redis.call('HGET', KEYS[1], 'user_id')
local email = redis.call('HGET', KEYS[1], 'email') or ''
local ip = redis.call('HGET', KEYS[1], 'ip') or ''
local ua = redis.call('HGET', KEYS[1], 'ua') or ''
local dev = redis.call('HGET', KEYS[1], 'dev') or ''
return {1, uid, email, ip, ua, dev}
`

func (s *OTPStore) Resend(
	ctx context.Context,
	challengeHash, newCodeHash string,
	cooldown time.Duration,
	maxResends int,
	now time.Time,
) (domainauth.OTPChallenge, time.Duration, error) {
	var out domainauth.OTPChallenge

	res, err := s.client.Eval(ctx, otpResendScript,
		[]string{otpKey(challengeHash)},
		newCodeHash, now.UnixMilli(), cooldown.Milliseconds(), maxResends).Slice()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return out, 0, fmt.Errorf("gửi lại OTP: %w", err)
	}
	if len(res) == 0 {
		return out, 0, domainauth.ErrOTPNotFound
	}

	switch code, _ := res[0].(int64); code {
	case 0:
		return out, 0, domainauth.ErrOTPNotFound
	case 2:
		wait, _ := res[1].(int64)
		return out, time.Duration(wait) * time.Second, domainauth.ErrOTPResendTooSoon
	case 3:
		return out, 0, domainauth.ErrOTPTooManyResends
	}

	if len(res) < 6 {
		return out, 0, domainauth.ErrOTPNotFound
	}
	uidStr, _ := res[1].(string)
	userID, parseErr := uuid.Parse(uidStr)
	if parseErr != nil {
		return out, 0, domainauth.ErrOTPNotFound
	}

	email, _ := res[2].(string)
	ip, _ := res[3].(string)
	ua, _ := res[4].(string)
	dev, _ := res[5].(string)

	out = domainauth.OTPChallenge{
		UserID:     userID,
		Email:      email,
		IP:         ip,
		UserAgent:  ua,
		DeviceName: dev,
	}
	return out, 0, nil
}

func (s *OTPStore) Delete(ctx context.Context, challengeHash string) error {
	if err := s.client.Del(ctx, otpKey(challengeHash)).Err(); err != nil {
		return fmt.Errorf("xoá thử thách OTP: %w", err)
	}
	return nil
}
