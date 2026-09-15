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

// RefreshStore lưu refresh token đã băm.
//
// Mỗi bản ghi là một Hash gồm:
//
//	session_id — phiên gắn với token
//	user_id    — chủ nhân của token
//	used       — "0" chưa dùng, "1" đã dùng
//
// Token đã dùng KHÔNG bị xoá ngay mà giữ lại tới khi hết hạn, để phát hiện
// tái sử dụng. Xoá ngay thì lần dùng lại sẽ chỉ báo "không tồn tại" và ta
// mất tín hiệu quan trọng nhất về việc token bị đánh cắp.
//
// user_id phải lưu riêng chứ không tra ngược từ session: mỗi lần refresh
// thành công thì phiên cũ bị xoá, nên lúc phát hiện tái sử dụng, phiên gắn
// với token cũ đã không còn để tra.
type RefreshStore struct {
	client *goredis.Client
}

func NewRefreshStore(client *goredis.Client) *RefreshStore {
	return &RefreshStore{client: client}
}

func refreshKey(hash string) string { return "refresh:" + hash }

func (s *RefreshStore) Save(
	ctx context.Context,
	tokenHash string,
	sessionID, userID uuid.UUID,
	ttl time.Duration,
) error {
	key := refreshKey(tokenHash)

	pipe := s.client.TxPipeline()
	pipe.HSet(ctx, key,
		"session_id", sessionID.String(),
		"user_id", userID.String(),
		"used", "0")
	pipe.Expire(ctx, key, ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("lưu refresh token: %w", err)
	}
	return nil
}

// consumeScript đánh dấu token đã dùng và trả về kết quả, chạy nguyên khối
// trong Redis.
//
// PHẢI dùng Lua chứ không đọc-rồi-ghi từ Go: hai request refresh đến cùng
// lúc (rất hay xảy ra khi nhiều tab cùng mở) sẽ cùng đọc thấy used=0 rồi
// cùng đi tiếp — một trong hai sẽ bị coi nhầm là đánh cắp và người dùng bị
// đá ra ngoài vô cớ. Lua bảo đảm chỉ một request thắng.
//
// Trả về:
//
//	{0}                       — token không tồn tại
//	{1, session_id, user_id}  — hợp lệ, vừa đánh dấu đã dùng
//	{2, session_id, user_id}  — dùng lại SAU thời gian ân hạn → nghi đánh cắp
//	{3, session_id, user_id}  — dùng lại TRONG thời gian ân hạn → chấp nhận
//
// ARGV[1] là thời điểm hiện tại (mili giây), ARGV[2] là độ dài ân hạn.
// Thời gian do Go truyền vào chứ không lấy từ Redis: lệnh Lua phải tất định
// để nhân bản Redis không lệch nhau.
const consumeScript = `
local used = redis.call('HGET', KEYS[1], 'used')
if not used then
    return {0}
end

local sid = redis.call('HGET', KEYS[1], 'session_id')
local uid = redis.call('HGET', KEYS[1], 'user_id')

if used == '1' then
    local usedAt = tonumber(redis.call('HGET', KEYS[1], 'used_at') or '0')
    local now = tonumber(ARGV[1])
    local grace = tonumber(ARGV[2])
    if usedAt > 0 and (now - usedAt) <= grace then
        return {3, sid, uid}
    end
    return {2, sid, uid}
end

redis.call('HSET', KEYS[1], 'used', '1', 'used_at', ARGV[1])
return {1, sid, uid}
`

func (s *RefreshStore) Consume(
	ctx context.Context,
	tokenHash string,
) (sessionID, userID uuid.UUID, err error) {
	now := time.Now().UnixMilli()
	grace := domainauth.RefreshGracePeriod.Milliseconds()

	res, evalErr := s.client.Eval(ctx, consumeScript,
		[]string{refreshKey(tokenHash)}, now, grace).Slice()
	if evalErr != nil && !errors.Is(evalErr, goredis.Nil) {
		return uuid.Nil, uuid.Nil, fmt.Errorf("tiêu refresh token: %w", evalErr)
	}
	if len(res) == 0 {
		return uuid.Nil, uuid.Nil, domainauth.ErrTokenInvalid
	}

	code, _ := res[0].(int64)
	if code == 0 || len(res) < 3 {
		return uuid.Nil, uuid.Nil, domainauth.ErrTokenInvalid
	}

	sidStr, _ := res[1].(string)
	uidStr, _ := res[2].(string)

	sessionID, _ = uuid.Parse(sidStr)
	userID, parseErr := uuid.Parse(uidStr)
	if parseErr != nil {
		return uuid.Nil, uuid.Nil, domainauth.ErrTokenInvalid
	}

	switch code {
	case 2:
		// Trả kèm userID để usecase huỷ được TOÀN BỘ phiên của người này,
		// kể cả khi phiên gắn với token cũ đã bị xoá từ lần refresh trước.
		return sessionID, userID, domainauth.ErrTokenReused
	case 3:
		return sessionID, userID, domainauth.ErrTokenReusedInGrace
	}
	return sessionID, userID, nil
}
