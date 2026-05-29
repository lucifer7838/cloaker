package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DeclareTopology sets up all exchanges, queues, and bindings.
func DeclareTopology(ch *amqp.Channel) error {
	// Direct exchange for click events
	if err := ch.ExchangeDeclare(
		"ghostroute.clicks",
		"direct",
		true,  // durable
		false, // auto-delete
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return fmt.Errorf("declare clicks exchange: %w", err)
	}

	// Fanout exchange for system-wide broadcasts
	if err := ch.ExchangeDeclare(
		"ghostroute.broadcasts",
		"fanout",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare broadcasts exchange: %w", err)
	}

	// Dead letter exchange
	if err := ch.ExchangeDeclare(
		"ghostroute.dlx",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	); err != nil {
		return fmt.Errorf("declare dlx exchange: %w", err)
	}

	// Main click queue with dead letter routing
	_, err := ch.QueueDeclare(
		"clicks.general",
		true,  // durable
		false, // auto-delete
		false, // exclusive
		false, // no-wait
		amqp.Table{
			"x-dead-letter-exchange":    "ghostroute.dlx",
			"x-dead-letter-routing-key": "retry.1",
			"x-max-length":              int32(1000000),
			"x-message-ttl":             int32(86400000),
		},
	)
	if err != nil {
		return fmt.Errorf("declare clicks.general queue: %w", err)
	}

	// Bind click queue to clicks exchange
	if err := ch.QueueBind(
		"clicks.general",
		"click.general",
		"ghostroute.clicks",
		false,
		nil,
	); err != nil {
		return fmt.Errorf("bind clicks.general: %w", err)
	}

	// Broadcast queues
	for _, qName := range []string{"broadcasts.analytics", "broadcasts.monitoring"} {
		if _, err := ch.QueueDeclare(qName, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare %s: %w", qName, err)
		}
		if err := ch.QueueBind(qName, "", "ghostroute.broadcasts", false, nil); err != nil {
			return fmt.Errorf("bind %s: %w", qName, err)
		}
	}

	// Dead letter retry queues with escalating TTLs
	retryConfigs := []struct {
		queue      string
		routingKey string
		ttl        int32
	}{
		{"dlx.retry.1", "retry.1", 5000},
		{"dlx.retry.2", "retry.2", 30000},
		{"dlx.retry.3", "retry.3", 300000},
	}

	for _, rc := range retryConfigs {
		_, err := ch.QueueDeclare(
			rc.queue,
			true, false, false, false,
			amqp.Table{
				"x-dead-letter-exchange":    "ghostroute.clicks",
				"x-dead-letter-routing-key": "click.general",
				"x-message-ttl":             rc.ttl,
			},
		)
		if err != nil {
			return fmt.Errorf("declare %s: %w", rc.queue, err)
		}
		if err := ch.QueueBind(rc.queue, rc.routingKey, "ghostroute.dlx", false, nil); err != nil {
			return fmt.Errorf("bind %s: %w", rc.queue, err)
		}
	}

	// Permanent failure queue
	if _, err := ch.QueueDeclare("dlx.failed", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx.failed: %w", err)
	}
	if err := ch.QueueBind("dlx.failed", "failed", "ghostroute.dlx", false, nil); err != nil {
		return fmt.Errorf("bind dlx.failed: %w", err)
	}

	return nil
}
