package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Producer publishes events to RabbitMQ with automatic reconnection
// and publisher confirms.
type Producer struct {
	url         string
	conn        *amqp.Connection
	channels    chan *amqp.Channel
	poolSize    int
	mu          sync.Mutex
	closed      bool
	closeOnce   sync.Once
	notifyClose chan *amqp.Error
}

// ProducerConfig holds producer configuration.
type ProducerConfig struct {
	URL      string // amqp://user:pass@host:5672/vhost
	PoolSize int    // number of channels in the pool (default: 5)
}

// NewProducer creates a producer with channel pooling and auto-recovery.
func NewProducer(cfg ProducerConfig) (*Producer, error) {
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 5
	}

	p := &Producer{
		url:      cfg.URL,
		channels: make(chan *amqp.Channel, cfg.PoolSize),
		poolSize: cfg.PoolSize,
	}

	if err := p.connect(); err != nil {
		return nil, err
	}

	go p.handleReconnect()

	return p, nil
}

func (p *Producer) connect() error {
	conn, err := amqp.Dial(p.url)
	if err != nil {
		return fmt.Errorf("amqp dial: %w", err)
	}

	p.mu.Lock()
	p.conn = conn
	p.notifyClose = conn.NotifyClose(make(chan *amqp.Error, 1))
	p.mu.Unlock()

	for i := 0; i < p.poolSize; i++ {
		ch, err := conn.Channel()
		if err != nil {
			return fmt.Errorf("create channel %d: %w", i, err)
		}
		if err := ch.Confirm(false); err != nil {
			return fmt.Errorf("enable confirms on channel %d: %w", i, err)
		}
		p.channels <- ch
	}

	return nil
}

func (p *Producer) handleReconnect() {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}
		notifyClose := p.notifyClose
		p.mu.Unlock()

		err := <-notifyClose
		if err == nil {
			return
		}
		log.Printf("RabbitMQ connection lost: %v. Reconnecting...", err)

		// Drain old channels
		for len(p.channels) > 0 {
			<-p.channels
		}

		// Reconnect with backoff
		for attempt := 1; ; attempt++ {
			p.mu.Lock()
			if p.closed {
				p.mu.Unlock()
				return
			}
			p.mu.Unlock()

			backoff := time.Duration(attempt) * 2 * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)

			if err := p.connect(); err != nil {
				log.Printf("Reconnect attempt %d failed: %v", attempt, err)
				continue
			}
			log.Printf("Reconnected to RabbitMQ after %d attempts", attempt)
			break
		}
	}
}

// Publish sends a message to the specified exchange with publisher confirms.
func (p *Producer) Publish(ctx context.Context, exchange, routingKey string, msg interface{}) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("producer is closed")
	}
	p.mu.Unlock()

	var ch *amqp.Channel
	select {
	case ch = <-p.channels:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() {
		p.mu.Lock()
		closed := p.closed
		p.mu.Unlock()
		if !closed {
			p.channels <- ch
		}
	}()

	confirmation, err := ch.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			Timestamp:    time.Now().UTC(),
			MessageId:    uuid.New().String(),
			Body:         body,
		},
	)
	if err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	if !confirmation.Wait() {
		return fmt.Errorf("broker did not confirm message delivery")
	}

	return nil
}

// Close gracefully shuts down the producer.
func (p *Producer) Close() error {
	var closeErr error
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()

		close(p.channels)
		for ch := range p.channels {
			_ = ch.Close()
		}

		if p.conn != nil && !p.conn.IsClosed() {
			closeErr = p.conn.Close()
		}
	})
	return closeErr
}
