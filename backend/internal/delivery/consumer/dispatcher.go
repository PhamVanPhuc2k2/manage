// Package consumer là tầng delivery của worker: nhận message RabbitMQ
// rồi gọi xuống usecase. Đối xứng với tầng http của api.
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
	"github.com/PhamVanPhuc2k2/manage/pkg/metrics"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

type HandlerFunc func(ctx context.Context, job domainsystem.Job) error

// Dispatcher định tuyến message tới handler theo tên job.
// Thêm loại job mới ở phase sau chỉ là gọi thêm một dòng Register.
type Dispatcher struct {
	client   *mq.Client
	log      zerolog.Logger
	handlers map[string]HandlerFunc
}

func NewDispatcher(client *mq.Client, log zerolog.Logger) *Dispatcher {
	return &Dispatcher{
		client:   client,
		log:      log,
		handlers: make(map[string]HandlerFunc),
	}
}

func (d *Dispatcher) Register(jobName string, fn HandlerFunc) error {
	d.handlers[jobName] = fn
	return d.client.BindJob(jobName)
}

// Run nhận message cho tới khi ctx bị huỷ.
func (d *Dispatcher) Run(ctx context.Context, concurrency int) error {
	ch := d.client.Channel()
	if ch == nil {
		return mq.ErrNotConnected
	}

	// Prefetch: mỗi worker chỉ giữ tối đa `concurrency` message chưa ack.
	// Không đặt QoS thì RabbitMQ đẩy toàn bộ queue vào một worker,
	// các worker khác ngồi không.
	if err := ch.Qos(concurrency, 0, false); err != nil {
		return fmt.Errorf("đặt QoS: %w", err)
	}

	deliveries, err := ch.Consume(
		mq.QueueJobs,
		"",    // consumer tag, để rỗng cho RabbitMQ tự sinh
		false, // autoAck = false: TỰ ack sau khi xử lý xong.
		//       Bật autoAck sẽ mất message khi worker chết giữa chừng.
		false, false, false, nil,
	)
	if err != nil {
		return fmt.Errorf("đăng ký consume: %w", err)
	}

	sem := make(chan struct{}, concurrency)

	for {
		select {
		case <-ctx.Done():
			d.log.Info().Msg("dừng nhận message mới")
			return nil

		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("kênh consume đã đóng")
			}

			sem <- struct{}{}
			go func(msg amqp.Delivery) {
				defer func() { <-sem }()
				d.handle(ctx, msg)
			}(delivery)
		}
	}
}

func (d *Dispatcher) handle(ctx context.Context, msg amqp.Delivery) {
	start := time.Now()

	var job domainsystem.Job
	if err := json.Unmarshal(msg.Body, &job); err != nil {
		d.log.Error().Err(err).Bytes("body", msg.Body).
			Msg("message hỏng, đẩy sang dead-letter")
		// requeue = false → message đi thẳng sang dead-letter queue.
		// Requeue một message hỏng chỉ tạo vòng lặp vô tận.
		_ = msg.Nack(false, false)
		metrics.JobsProcessed.WithLabelValues("unparseable", "dead_letter").Inc()
		return
	}

	handler, found := d.handlers[job.Name]
	if !found {
		d.log.Error().Str("job", job.Name).Msg("không có handler cho job này")
		_ = msg.Nack(false, false)
		// Nhãn là TÊN JOB lấy từ message, nhưng chỉ ở nhánh này thì nó đến từ
		// bên ngoài. Vẫn ghi vì tên job do chính hệ thống sinh ra, không phải
		// người dùng nhập — và "job nào không có handler" là câu hỏi đầu tiên
		// khi dead-letter queue bắt đầu đầy lên.
		metrics.JobsProcessed.WithLabelValues(job.Name, "no_handler").Inc()
		return
	}

	log := d.log.With().
		Str("job", job.Name).
		Str("request_id", job.RequestID).
		Logger()

	jobCtx, cancel := context.WithTimeout(logger.WithContext(ctx, log), 5*time.Minute)
	defer cancel()

	err := handler(jobCtx, job)
	metrics.JobDuration.WithLabelValues(job.Name).Observe(time.Since(start).Seconds())

	if err != nil {
		log.Error().Err(err).Msg("xử lý job thất bại")
		_ = msg.Nack(false, false)
		metrics.JobsProcessed.WithLabelValues(job.Name, "error").Inc()
		return
	}

	log.Debug().Dur("duration", time.Since(start)).Msg("xử lý job xong")
	_ = msg.Ack(false)
	metrics.JobsProcessed.WithLabelValues(job.Name, "ok").Inc()
}
