// Package system là tầng nghiệp vụ của module system.
package system

import (
	"context"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

type PingUsecase struct {
	clock     domainsystem.Clock
	cache     domainsystem.Cache
	publisher domainsystem.JobPublisher
}

func NewPingUsecase(
	clock domainsystem.Clock,
	cache domainsystem.Cache,
	publisher domainsystem.JobPublisher,
) *PingUsecase {
	return &PingUsecase{clock: clock, cache: cache, publisher: publisher}
}

// Ping chạm vào cả ba thành phần hạ tầng rồi báo cáo kết quả.
//
// Chỉ PostgreSQL là bắt buộc — hỏng thì trả lỗi. Redis và RabbitMQ hỏng thì
// vẫn trả về được, chỉ đánh dấu là false. Cách phân biệt này về sau áp dụng
// cho nghiệp vụ thật: mất Redis thì chậm chứ không sai, mất database thì sai.
func (u *PingUsecase) Ping(
	ctx context.Context,
	requestID string,
) (domainsystem.HealthSnapshot, error) {
	log := logger.FromContext(ctx)

	dbTime, err := u.clock.Now(ctx)
	if err != nil {
		return domainsystem.HealthSnapshot{}, apperror.Wrap(
			apperror.KindInternal, "không đọc được giờ từ database", err)
	}

	redisOK := true
	if err := u.cache.Ping(ctx); err != nil {
		log.Warn().Err(err).Msg("redis không phản hồi")
		redisOK = false
	}

	jobQueued := true
	if err := u.publisher.Publish(ctx, domainsystem.Job{
		Name:      domainsystem.JobSystemPing,
		RequestID: requestID,
		Payload:   map[string]any{"source": "api"},
	}); err != nil {
		log.Warn().Err(err).Msg("không đẩy được job lên hàng đợi")
		jobQueued = false
	}

	return domainsystem.HealthSnapshot{
		DatabaseTime: dbTime,
		RedisOK:      redisOK,
		JobQueued:    jobQueued,
		RequestID:    requestID,
	}, nil
}
