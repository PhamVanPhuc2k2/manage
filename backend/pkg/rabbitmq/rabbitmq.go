// Package rabbitmq quản lý kết nối và khai báo topology.
//
// Topology của dự án:
//
//	manage.jobs (exchange direct)
//	  └─ routing key = tên job ─▶ manage.jobs.queue
//	                                   │ (message bị nack hoặc quá hạn)
//	                                   ▼
//	                             manage.jobs.dlx ─▶ manage.jobs.dead
//
//	manage.realtime (exchange fanout)
//	  ├─▶ queue riêng của api instance 1 (exclusive, auto-delete)
//	  ├─▶ queue riêng của api instance 2
//	  └─▶ ...
//
// Dead-letter queue rất quan trọng: không có nó, một message lỗi sẽ bị
// requeue vô hạn và ăn hết CPU của worker.
//
// Exchange realtime KHÔNG có dead-letter, và điều đó là cố ý: bản tin
// realtime chỉ có giá trị NGAY LÚC ĐÓ. Một thông báo "có người vừa giao việc
// cho bạn" được phát lại sau mười phút chỉ gây bối rối. Queue của mỗi
// instance vì vậy cũng không bền (auto-delete) — instance chết thì mọi bản
// tin đang chờ nó cũng hết ý nghĩa.
package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

const (
	ExchangeJobs    = "manage.jobs"
	ExchangeJobsDLX = "manage.jobs.dlx"
	QueueJobs       = "manage.jobs.queue"
	QueueJobsDead   = "manage.jobs.dead"

	// ExchangeRealtime fan-out bản tin WebSocket tới MỌI instance api.
	//
	// Cần thiết ngay từ instance đầu tiên, không phải chỉ khi scale: giữ
	// trạng thái kết nối trong bộ nhớ một tiến trình là kiểu thiết kế chạy
	// được với một replica rồi hỏng lặng lẽ ngay khi có replica thứ hai —
	// người dùng A nối vào instance 1 sẽ không bao giờ nhận được tin nhắn
	// gửi từ instance 2, mà không có lỗi nào xuất hiện ở đâu cả.
	ExchangeRealtime = "manage.realtime"
)

var ErrNotConnected = errors.New("rabbitmq không có kết nối")

type Client struct {
	url string
	log zerolog.Logger

	// ctx sống cùng vòng đời của client. Nó bị huỷ khi ứng dụng tắt hoặc
	// khi gọi Close(), để vòng lặp kết nối lại dừng ngay thay vì tiếp tục
	// thử thêm gần một phút sau khi tiến trình đã được yêu cầu dừng.
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.RWMutex
	conn   *amqp.Connection
	ch     *amqp.Channel
	closed bool

	// Tên các job đã đăng ký, để bind lại sau khi kết nối lại.
	boundJobs map[string]struct{}
}

func New(ctx context.Context, url string, log zerolog.Logger) (*Client, error) {
	clientCtx, cancel := context.WithCancel(ctx)

	c := &Client{
		url:       url,
		log:       log,
		ctx:       clientCtx,
		cancel:    cancel,
		boundJobs: make(map[string]struct{}),
	}
	if err := c.connect(clientCtx); err != nil {
		cancel()
		return nil, err
	}
	go c.watchAndReconnect()
	return c, nil
}

func (c *Client) connect(ctx context.Context) error {
	const maxAttempts = 10
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		conn, err := amqp.Dial(c.url)
		if err == nil {
			ch, chErr := conn.Channel()
			if chErr == nil {
				c.mu.Lock()
				c.conn, c.ch = conn, ch
				c.mu.Unlock()

				if err := c.declareTopology(); err != nil {
					return err
				}
				c.log.Info().Msg("đã kết nối rabbitmq")
				return nil
			}
			_ = conn.Close()
			lastErr = chErr
		} else {
			lastErr = err
		}

		wait := time.Duration(attempt) * time.Second
		c.log.Warn().Err(lastErr).
			Int("attempt", attempt).
			Dur("retry_in", wait).
			Msg("chưa kết nối được rabbitmq, thử lại")

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return fmt.Errorf("không kết nối được rabbitmq sau %d lần: %w", maxAttempts, lastErr)
}

// declareTopology tạo exchange và queue. Gọi mỗi lần kết nối lại —
// các lệnh declare đều idempotent nên chạy nhiều lần vô hại.
func (c *Client) declareTopology() error {
	ch := c.Channel()
	if ch == nil {
		return ErrNotConnected
	}

	if err := ch.ExchangeDeclare(ExchangeJobs, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo exchange %s: %w", ExchangeJobs, err)
	}
	if err := ch.ExchangeDeclare(ExchangeJobsDLX, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo exchange %s: %w", ExchangeJobsDLX, err)
	}
	if err := ch.ExchangeDeclare(ExchangeRealtime, "fanout", true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo exchange %s: %w", ExchangeRealtime, err)
	}

	// Queue chính, có gắn dead-letter exchange.
	if _, err := ch.QueueDeclare(QueueJobs, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": ExchangeJobsDLX,
	}); err != nil {
		return fmt.Errorf("khai báo queue %s: %w", QueueJobs, err)
	}
	if _, err := ch.QueueDeclare(QueueJobsDead, true, false, false, false, nil); err != nil {
		return fmt.Errorf("khai báo queue %s: %w", QueueJobsDead, err)
	}
	if err := ch.QueueBind(QueueJobsDead, "", ExchangeJobsDLX, false, nil); err != nil {
		return fmt.Errorf("bind queue chết: %w", err)
	}

	// Bind lại các job đã đăng ký trước khi mất kết nối.
	c.mu.RLock()
	jobs := make([]string, 0, len(c.boundJobs))
	for name := range c.boundJobs {
		jobs = append(jobs, name)
	}
	c.mu.RUnlock()

	for _, name := range jobs {
		if err := ch.QueueBind(QueueJobs, name, ExchangeJobs, false, nil); err != nil {
			return fmt.Errorf("bind lại job %s: %w", name, err)
		}
	}
	return nil
}

// BindJob gắn một tên job vào queue chính. Gọi lúc khởi động worker
// cho mỗi loại job mà worker biết xử lý.
func (c *Client) BindJob(jobName string) error {
	ch := c.Channel()
	if ch == nil {
		return ErrNotConnected
	}
	if err := ch.QueueBind(QueueJobs, jobName, ExchangeJobs, false, nil); err != nil {
		return err
	}

	c.mu.Lock()
	c.boundJobs[jobName] = struct{}{}
	c.mu.Unlock()
	return nil
}

// watchAndReconnect lắng nghe sự kiện đóng kết nối và tự kết nối lại.
// RabbitMQ restart hoặc mạng chớp là chuyện bình thường, không được chết theo.
func (c *Client) watchAndReconnect() {
	for {
		c.mu.RLock()
		conn := c.conn
		closed := c.closed
		c.mu.RUnlock()

		if conn == nil || closed {
			return
		}

		reason, ok := <-conn.NotifyClose(make(chan *amqp.Error))
		if !ok {
			return // đóng chủ động, không phải lỗi
		}

		c.mu.RLock()
		closed = c.closed
		c.mu.RUnlock()
		if closed {
			return
		}

		c.log.Error().Str("reason", reason.Error()).Msg("rabbitmq mất kết nối, đang kết nối lại")
		if err := c.connect(c.ctx); err != nil {
			c.log.Error().Err(err).Msg("kết nối lại rabbitmq thất bại")
			return
		}
	}
}

func (c *Client) Channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ch
}

// NewChannel mở một channel RIÊNG trên kết nối hiện có.
//
// Tồn tại cho những thao tác mà RabbitMQ đóng channel khi thất bại — điển
// hình là passive declare một hàng đợi không tồn tại. Dùng chung channel
// chính cho việc đó sẽ biến một lần thăm dò lỗi thành mất cả luồng consume
// job, và luồng đó không tự mở lại (chỉ kết nối mới có cơ chế nối lại).
//
// Người gọi chịu trách nhiệm Close. Channel KHÔNG tự mở lại sau khi kết nối
// đứt, nên chỉ dùng cho thao tác ngắn, xong là đóng.
func (c *Client) NewChannel() (*amqp.Channel, error) {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil || conn.IsClosed() {
		return nil, ErrNotConnected
	}
	return conn.Channel()
}

// HealthCheck dùng cho endpoint /ready.
func (c *Client) HealthCheck(context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil || c.conn.IsClosed() {
		return ErrNotConnected
	}
	return nil
}

func (c *Client) Close() error {
	// Huỷ ctx trước để vòng lặp kết nối lại thoát ngay, không cố nối lại
	// đúng lúc ta đang chủ động đóng.
	c.cancel()

	c.mu.Lock()
	c.closed = true
	ch, conn := c.ch, c.conn
	c.mu.Unlock()

	if ch != nil {
		_ = ch.Close()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}
