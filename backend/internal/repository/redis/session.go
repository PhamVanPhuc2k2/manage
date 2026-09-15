package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
)

// SessionStore lưu phiên đăng nhập trong Redis.
//
// Dùng hai khoá cho mỗi phiên:
//
//	session:{id}            → JSON của phiên
//	user_sessions:{user_id} → Set chứa id các phiên, phục vụ "đăng xuất tất cả"
//
// Set thứ hai là cần thiết: Redis không cho tìm khoá theo giá trị bên trong,
// nên không có nó thì phải quét toàn bộ keyspace bằng SCAN — chậm và không
// an toàn khi dữ liệu lớn.
type SessionStore struct {
	client *goredis.Client
}

func NewSessionStore(client *goredis.Client) *SessionStore {
	return &SessionStore{client: client}
}

func sessionKey(id uuid.UUID) string      { return "session:" + id.String() }
func userSessionsKey(id uuid.UUID) string { return "user_sessions:" + id.String() }

func (s *SessionStore) Create(ctx context.Context, sess domainauth.Session, ttl time.Duration) error {
	data, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("mã hoá phiên: %w", err)
	}

	// Dùng pipeline để hai lệnh đi trong một vòng mạng.
	pipe := s.client.TxPipeline()
	pipe.Set(ctx, sessionKey(sess.ID), data, ttl)
	pipe.SAdd(ctx, userSessionsKey(sess.UserID), sess.ID.String())
	// Gia hạn cả set, nếu không nó sẽ sống mãi sau khi mọi phiên đã hết hạn.
	pipe.Expire(ctx, userSessionsKey(sess.UserID), ttl)

	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("lưu phiên: %w", err)
	}
	return nil
}

func (s *SessionStore) Get(ctx context.Context, sessionID uuid.UUID) (*domainauth.Session, error) {
	data, err := s.client.Get(ctx, sessionKey(sessionID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, domainauth.ErrNoSession
	}
	if err != nil {
		return nil, fmt.Errorf("đọc phiên: %w", err)
	}

	var sess domainauth.Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, fmt.Errorf("giải mã phiên: %w", err)
	}
	return &sess, nil
}

// Touch cập nhật thời điểm hoạt động cuối, để người dùng thấy thiết bị nào
// đang dùng. Không gia hạn TTL — phiên vẫn hết hạn đúng lịch.
func (s *SessionStore) Touch(ctx context.Context, sessionID uuid.UUID) error {
	sess, err := s.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Chỉ ghi lại nếu đã quá một phút, tránh ghi Redis mỗi request.
	if time.Since(sess.LastSeenAt) < time.Minute {
		return nil
	}
	sess.LastSeenAt = time.Now()

	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	// KEEPTTL giữ nguyên hạn cũ thay vì đặt lại.
	return s.client.Set(ctx, sessionKey(sessionID), data, goredis.KeepTTL).Err()
}

func (s *SessionStore) Delete(ctx context.Context, sessionID uuid.UUID) error {
	sess, err := s.Get(ctx, sessionID)
	if err != nil {
		if errors.Is(err, domainauth.ErrNoSession) {
			return nil // đã không còn, coi như xong
		}
		return err
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, sessionKey(sessionID))
	pipe.SRem(ctx, userSessionsKey(sess.UserID), sessionID.String())
	_, err = pipe.Exec(ctx)
	return err
}

// DeleteAllOfUser cắt sạch mọi phiên của một người.
//
// Gọi khi: đăng xuất tất cả, đổi mật khẩu, đặt lại mật khẩu, và khi phát
// hiện refresh token bị tái sử dụng.
func (s *SessionStore) DeleteAllOfUser(ctx context.Context, userID uuid.UUID) error {
	ids, err := s.client.SMembers(ctx, userSessionsKey(userID)).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return fmt.Errorf("đọc danh sách phiên: %w", err)
	}

	pipe := s.client.TxPipeline()
	for _, id := range ids {
		pipe.Del(ctx, "session:"+id)
	}
	pipe.Del(ctx, userSessionsKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}

func (s *SessionStore) ListOfUser(ctx context.Context, userID uuid.UUID) ([]domainauth.Session, error) {
	ids, err := s.client.SMembers(ctx, userSessionsKey(userID)).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return nil, fmt.Errorf("đọc danh sách phiên: %w", err)
	}

	out := make([]domainauth.Session, 0, len(ids))
	var stale []string

	for _, idStr := range ids {
		id, err := uuid.Parse(idStr)
		if err != nil {
			stale = append(stale, idStr)
			continue
		}
		sess, err := s.Get(ctx, id)
		if err != nil {
			// Phiên đã hết hạn nhưng id vẫn nằm trong set — dọn luôn.
			stale = append(stale, idStr)
			continue
		}
		out = append(out, *sess)
	}

	if len(stale) > 0 {
		members := make([]any, len(stale))
		for i, v := range stale {
			members[i] = v
		}
		_ = s.client.SRem(ctx, userSessionsKey(userID), members...).Err()
	}
	return out, nil
}
