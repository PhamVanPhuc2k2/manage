// Package rabbitmq hiện thực port JobPublisher.
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	domainsystem "github.com/PhamVanPhuc2k2/manage/internal/domain/system"
	mq "github.com/PhamVanPhuc2k2/manage/pkg/rabbitmq"
)

type JobPublisher struct {
	client *mq.Client
}

func NewJobPublisher(client *mq.Client) *JobPublisher {
	return &JobPublisher{client: client}
}

func (p *JobPublisher) Publish(ctx context.Context, job domainsystem.Job) error {
	body, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("mã hoá job: %w", err)
	}

	ch := p.client.Channel()
	if ch == nil {
		return mq.ErrNotConnected
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return ch.PublishWithContext(
		ctx,
		mq.ExchangeJobs,
		job.Name, // routing key = tên job
		false,    // mandatory
		false,    // immediate
		amqp.Publishing{
			ContentType: "application/json",
			// DeliveryMode 2 = persistent: message được ghi xuống đĩa,
			// không mất khi RabbitMQ restart. Chậm hơn nhưng với job
			// nghiệp vụ thì mất message là không chấp nhận được.
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now(),
			MessageId:    job.RequestID,
			Body:         body,
		},
	)
}
