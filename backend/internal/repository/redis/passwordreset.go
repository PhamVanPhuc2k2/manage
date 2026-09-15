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

// PasswordResetStore lưu token đặt lại mật khẩu, dùng một lần.
type PasswordResetStore struct {
	client *goredis.Client
}

func NewPasswordResetStore(client *goredis.Client) *PasswordResetStore {
	return &PasswordResetStore{client: client}
}

func resetKey(hash string) string { return "pwreset:" + hash }

func (s *PasswordResetStore) Save(
	ctx context.Context,
	tokenHash string,
	userID uuid.UUID,
	ttl time.Duration,
) error {
	if err := s.client.Set(ctx, resetKey(tokenHash), userID.String(), ttl).Err(); err != nil {
		return fmt.Errorf("lưu token đặt lại mật khẩu: %w", err)
	}
	return nil
}

// Consume đọc và XOÁ token trong một thao tác nguyên khối.
//
// GETDEL thay vì GET rồi DEL: hai request đến cùng lúc với cùng một token
// sẽ chỉ có một request nhận được giá trị. Tách hai lệnh thì cả hai cùng
// đọc được và token dùng được hai lần.
func (s *PasswordResetStore) Consume(ctx context.Context, tokenHash string) (uuid.UUID, error) {
	val, err := s.client.GetDel(ctx, resetKey(tokenHash)).Result()
	if errors.Is(err, goredis.Nil) {
		return uuid.Nil, domainauth.ErrTokenInvalid
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("đọc token đặt lại mật khẩu: %w", err)
	}

	id, err := uuid.Parse(val)
	if err != nil {
		return uuid.Nil, domainauth.ErrTokenInvalid
	}
	return id, nil
}
