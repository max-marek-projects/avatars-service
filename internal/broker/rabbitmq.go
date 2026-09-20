// Package broker provides a RabbitMQ implementation of services.Publisher.
package broker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/max-marek-projects/avatars-service/internal/models"
)

// RabbitConfig holds connection and routing parameters.
type RabbitConfig struct {
	URL          string // amqp://guest:guest@rabbitmq:5672/
	ExchangeName string // avatars.exchange
	UploadKey    string // avatar.uploaded
	DeleteKey    string // avatar.deleted
}

// rabbitPublisher is a concurrency-safe RabbitMQ publisher.
type rabbitPublisher struct {
	conn     *amqp.Connection
	ch       *amqp.Channel
	exchange string
	upKey    string
	delKey   string
	logger   *slog.Logger
	mu       sync.Mutex
}

// NewRabbitPublisher dials RabbitMQ and declares a topic exchange.
func NewRabbitPublisher(cfg RabbitConfig, logger *slog.Logger) (*rabbitPublisher, error) {
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("rabbit dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbit channel: %w", err)
	}
	if err := ch.ExchangeDeclare(
		cfg.ExchangeName,
		"topic",
		true,  // durable
		false, // autoDelete
		false, // internal
		false, // noWait
		nil,
	); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("exchange declare: %w", err)
	}
	return &rabbitPublisher{
		conn:     conn,
		ch:       ch,
		exchange: cfg.ExchangeName,
		upKey:    cfg.UploadKey,
		delKey:   cfg.DeleteKey,
		logger:   logger,
	}, nil
}

// PublishUpload routes an AvatarUploadEvent to the upload routing key.
func (p *rabbitPublisher) PublishUpload(ctx context.Context, event models.AvatarUploadEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal upload event: %w", err)
	}
	return p.publish(ctx, p.upKey, event.AvatarID, body)
}

// PublishDelete routes an AvatarDeleteEvent to the delete routing key.
func (p *rabbitPublisher) PublishDelete(ctx context.Context, event models.AvatarDeleteEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal delete event: %w", err)
	}
	return p.publish(ctx, p.delKey, event.AvatarID, body)
}

func (p *rabbitPublisher) publish(ctx context.Context, key, id string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ch.PublishWithContext(ctx, p.exchange, key, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    id,
		Timestamp:    time.Now(),
		Body:         body,
	}); err != nil {
		return fmt.Errorf("rabbit publish %s: %w", key, err)
	}
	return nil
}

// Ping checks the connection is alive.
func (p *rabbitPublisher) Ping(_ context.Context) error {
	if p.conn == nil || p.conn.IsClosed() {
		return fmt.Errorf("rabbit connection closed")
	}
	return nil
}

// Close releases the channel and connection.
func (p *rabbitPublisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ch != nil {
		_ = p.ch.Close()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// rabbitConsumer consumes avatar events from RabbitMQ with manual ack.
type rabbitConsumer struct {
	conn     *amqp.Connection
	ch       *amqp.Channel
	exchange string
	uploadQ  string
	deleteQ  string
	upKey    string
	delKey   string
	logger   *slog.Logger
	mu       sync.Mutex
}

// NewRabbitConsumer dials RabbitMQ, declares the same topic exchange,
// binds two durable queues for upload/delete events.
func NewRabbitConsumer(cfg RabbitConfig, logger *slog.Logger) (*rabbitConsumer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	conn, err := amqp.Dial(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("rabbit dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbit channel: %w", err)
	}
	if err := ch.ExchangeDeclare(cfg.ExchangeName, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("exchange declare: %w", err)
	}

	uploadQ := "avatars.upload"
	deleteQ := "avatars.delete"

	if _, err := ch.QueueDeclare(uploadQ, true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("upload queue declare: %w", err)
	}
	if err := ch.QueueBind(uploadQ, cfg.UploadKey, cfg.ExchangeName, false, nil); err != nil {
		return nil, fmt.Errorf("upload queue bind: %w", err)
	}

	if _, err := ch.QueueDeclare(deleteQ, true, false, false, false, nil); err != nil {
		return nil, fmt.Errorf("delete queue declare: %w", err)
	}
	if err := ch.QueueBind(deleteQ, cfg.DeleteKey, cfg.ExchangeName, false, nil); err != nil {
		return nil, fmt.Errorf("delete queue bind: %w", err)
	}

	// Prefetch 1 to keep idempotency simple.
	if err := ch.Qos(1, 0, false); err != nil {
		return nil, fmt.Errorf("qos: %w", err)
	}

	return &rabbitConsumer{
		conn:     conn,
		ch:       ch,
		exchange: cfg.ExchangeName,
		uploadQ:  uploadQ,
		deleteQ:  deleteQ,
		upKey:    cfg.UploadKey,
		delKey:   cfg.DeleteKey,
		logger:   logger,
	}, nil
}

// ConsumeUpload blocks and calls handler for each AvatarUploadEvent.
// On handler error it Nacks without requeue; the handler itself must do
// bounded retries.
func (c *rabbitConsumer) ConsumeUpload(ctx context.Context, handler func(context.Context, models.AvatarUploadEvent) error) error {
	deliveries, err := c.ch.Consume(c.uploadQ, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume upload: %w", err)
	}
	return c.loop(ctx, deliveries, func(body []byte) error {
		var ev models.AvatarUploadEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return fmt.Errorf("unmarshal upload event: %w", err)
		}
		return handler(ctx, ev)
	})
}

// ConsumeDelete blocks and calls handler for each AvatarDeleteEvent.
func (c *rabbitConsumer) ConsumeDelete(ctx context.Context, handler func(context.Context, models.AvatarDeleteEvent) error) error {
	deliveries, err := c.ch.Consume(c.deleteQ, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume delete: %w", err)
	}
	return c.loop(ctx, deliveries, func(body []byte) error {
		var ev models.AvatarDeleteEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			return fmt.Errorf("unmarshal delete event: %w", err)
		}
		return handler(ctx, ev)
	})
}

func (c *rabbitConsumer) loop(
	ctx context.Context,
	deliveries <-chan amqp.Delivery,
	handle func([]byte) error,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("rabbit deliveries channel closed")
			}
			if err := handle(d.Body); err != nil {
				c.logger.Error("worker: event processing failed",
					slog.String("message_id", d.MessageId),
					slog.Any("error", err))
				_ = d.Nack(false, false)
				continue
			}
			_ = d.Ack(false)
		}
	}
}

// Ping checks the broker connection is alive.
func (c *rabbitConsumer) Ping(_ context.Context) error {
	if c.conn == nil || c.conn.IsClosed() {
		return errors.New("rabbit connection closed")
	}
	return nil
}

// Close releases the channel and connection.
func (c *rabbitConsumer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		_ = c.ch.Close()
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
