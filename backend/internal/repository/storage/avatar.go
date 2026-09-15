// Package storage nối cổng FileStorage của domain với client R2 ở pkg.
//
// Lớp mỏng này tồn tại để tầng usecase không phải import pkg/storage —
// giữ đúng quy tắc: usecase chỉ biết interface do domain định nghĩa.
package storage

import (
	"context"
	"time"

	pkgstorage "github.com/PhamVanPhuc2k2/manage/pkg/storage"
)

type Repository struct {
	s *pkgstorage.Storage
}

func NewRepository(s *pkgstorage.Storage) *Repository {
	return &Repository{s: s}
}

func (r *Repository) PresignPut(
	ctx context.Context,
	key, contentType string,
	ttl time.Duration,
) (string, error) {
	return r.s.PresignPut(ctx, key, contentType, ttl)
}

func (r *Repository) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	return r.s.PresignGet(ctx, key, ttl)
}

func (r *Repository) Stat(ctx context.Context, key string) (int64, string, error) {
	info, err := r.s.Stat(ctx, key)
	if err != nil {
		return 0, "", err
	}
	return info.Size, info.ContentType, nil
}

func (r *Repository) DetectContentType(ctx context.Context, key string) (string, error) {
	return r.s.DetectContentType(ctx, key)
}

func (r *Repository) Delete(ctx context.Context, key string) error {
	return r.s.Delete(ctx, key)
}
