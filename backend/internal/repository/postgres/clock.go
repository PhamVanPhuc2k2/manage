// Package postgres hiện thực các port của domain bằng PostgreSQL.
package postgres

import (
	"context"
	"time"

	"github.com/yourorg/manage/pkg/postgres"
)

type ClockRepository struct {
	db *postgres.DB
}

func NewClockRepository(db *postgres.DB) *ClockRepository {
	return &ClockRepository{db: db}
}

// Now đọc giờ từ chính database — kiểm chứng kết nối còn sống thật,
// không phải chỉ còn nằm trong pool.
func (r *ClockRepository) Now(ctx context.Context) (time.Time, error) {
	var t time.Time
	if err := r.db.QueryRow(ctx, "SELECT NOW()").Scan(&t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}
