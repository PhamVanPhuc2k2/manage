// Package redis hiện thực các port cần bộ nhớ đệm.
package redis

import (
	"context"

	goredis "github.com/redis/go-redis/v9"
)

type CacheRepository struct {
	client *goredis.Client
}

func NewCacheRepository(client *goredis.Client) *CacheRepository {
	return &CacheRepository{client: client}
}

func (r *CacheRepository) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
