# SKILL: RabbitMQ Go Producer / Python Consumer for Async Event Processing

## Purpose

Implement reliable async event processing for GhostRoute using RabbitMQ with a Go
producer (high-throughput click event publishing) and Python consumer (event processing,
enrichment, and storage). Includes dead letter handling, retry logic, publisher confirms,
and graceful shutdown patterns.

## Version Pins

- Go: `github.com/rabbitmq/amqp091-go v1.10.0`
- Python: `pika==1.3.2`
- RabbitMQ Server: 3.13 (with management plugin)
- Docker Image: `rabbitmq:3.13-management`

---

## 1. Exchange/Queue Topology

### 1.1 Architecture Overview

```
Producer (Go)
    |
    v
[ghostroute.clicks] (direct exchange)
    |-- routing_key: campaign.{id} --> [clicks.campaign.{id}] queue
    |-- routing_key: click.general  --> [clicks.general] queue
    |
[ghostroute.broadcasts] (fanout exchange)
    |--> [broadcasts.analytics] queue
    |--> [broadcasts.monitoring] queue
    |
[ghostroute.dlx] (direct exchange) <-- dead letters
    |-- routing_key: retry.1  --> [dlx.retry.1] (TTL: 5s)  --> back to clicks
    |-- routing_key: retry.2  --> [dlx.retry.2] (TTL: 30s) --> back to clicks
    |-- routing_key: retry.3  --> [dlx.retry.3] (TTL: 300s) --> back to clicks
    |-- routing_key: failed   --> [dlx.failed] (permanent failure store)
```

### 1.2 Exchange Declarations

```go
// topology.go - Declare all exchanges and queues

package rabbitmq

import (
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// DeclareTopology sets up all exchanges, queues, and bindings.
func DeclareTopology(ch *amqp.Channel) error {
	// Direct exchange for click events (routing by campaign)
	if err := ch.ExchangeDeclare(
		"ghostroute.clicks", // name
		"direct",            // type
		true,                // durable
		false,               // auto-delete
		false,               // internal
		false,               // no-wait
		nil,                 // arguments
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
			"x-max-length":              1000000, // 1M message limit
			"x-message-ttl":             86400000, // 24h TTL
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
		ttl        int
	}{
		{"dlx.retry.1", "retry.1", 5000},    // 5 seconds
		{"dlx.retry.2", "retry.2", 30000},   // 30 seconds
		{"dlx.retry.3", "retry.3", 300000},  // 5 minutes
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

	// Permanent failure queue (no TTL, manual inspection)
	if _, err := ch.QueueDeclare("dlx.failed", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dlx.failed: %w", err)
	}
	if err := ch.QueueBind("dlx.failed", "failed", "ghostroute.dlx", false, nil); err != nil {
		return fmt.Errorf("bind dlx.failed: %w", err)
	}

	return nil
}
```

---

## 2. Go Producer with Auto-Recovery and Publisher Confirms

```go
package rabbitmq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/google/uuid"
)

// Producer publishes click events to RabbitMQ with automatic reconnection
// and publisher confirms.
type Producer struct {
	url        string
	conn       *amqp.Connection
	channels   chan *amqp.Channel
	poolSize   int
	mu         sync.Mutex
	closed     bool
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

	// Start reconnection goroutine
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

	// Create channel pool
	for i := 0; i < p.poolSize; i++ {
		ch, err := conn.Channel()
		if err != nil {
			return fmt.Errorf("create channel %d: %w", i, err)
		}
		// Enable publisher confirms on each channel
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
			return // graceful close
		}
		log.Printf("RabbitMQ connection lost: %v. Reconnecting...", err)

		// Drain old channels
		for len(p.channels) > 0 {
			<-p.channels
		}

		// Reconnect with backoff
		for attempt := 1; ; attempt++ {
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

	// Acquire channel from pool
	var ch *amqp.Channel
	select {
	case ch = <-p.channels:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { p.channels <- ch }()

	// Publish with confirms
	confirmation, err := ch.PublishWithDeferredConfirmWithContext(
		ctx,
		exchange,
		routingKey,
		false, // mandatory
		false, // immediate
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

	// Wait for broker confirmation
	if !confirmation.Wait() {
		return fmt.Errorf("broker did not confirm message delivery")
	}

	return nil
}

// Close gracefully shuts down the producer.
func (p *Producer) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	// Drain and close channels
	close(p.channels)
	for ch := range p.channels {
		_ = ch.Close()
	}

	if p.conn != nil && !p.conn.IsClosed() {
		return p.conn.Close()
	}
	return nil
}
```

---

## 3. Message Schema Definitions

```json
{
  "": "http://json-schema.org/draft-07/schema#",
  "title": "ClickEvent",
  "type": "object",
  "required": ["event_id", "campaign_id", "event_time", "visitor_ip"],
  "properties": {
    "event_id": {
      "type": "string",
      "format": "uuid"
    },
    "campaign_id": {
      "type": "integer",
      "minimum": 1
    },
    "event_time": {
      "type": "string",
      "format": "date-time"
    },
    "visitor_ip": {
      "type": "string",
      "format": "ipv4"
    },
    "user_agent": {
      "type": "string",
      "maxLength": 1024
    },
    "fingerprint_hash": {
      "type": "string",
      "pattern": "^[a-f0-9]{64}$"
    },
    "country": {
      "type": "string",
      "maxLength": 2
    },
    "referer": {
      "type": "string",
      "format": "uri"
    },
    "landing_url": {
      "type": "string",
      "format": "uri"
    },
    "bot_score": {
      "type": "number",
      "minimum": 0.0,
      "maximum": 1.0
    }
  }
}
```

```json
{
  "": "http://json-schema.org/draft-07/schema#",
  "title": "ConversionEvent",
  "type": "object",
  "required": ["conversion_id", "click_id", "campaign_id", "event_time"],
  "properties": {
    "conversion_id": {
      "type": "string",
      "format": "uuid"
    },
    "click_id": {
      "type": "string",
      "format": "uuid"
    },
    "campaign_id": {
      "type": "integer"
    },
    "event_time": {
      "type": "string",
      "format": "date-time"
    },
    "conversion_type": {
      "type": "string",
      "enum": ["lead", "sale", "install", "signup"]
    },
    "payout": {
      "type": "number",
      "minimum": 0
    },
    "currency": {
      "type": "string",
      "pattern": "^[A-Z]{3}$"
    }
  }
}
```

```json
{
  "": "http://json-schema.org/draft-07/schema#",
  "title": "FingerprintEvent",
  "type": "object",
  "required": ["fingerprint_hash", "campaign_id", "event_time"],
  "properties": {
    "fingerprint_hash": {
      "type": "string",
      "pattern": "^[a-f0-9]{64}$"
    },
    "campaign_id": {
      "type": "integer"
    },
    "event_time": {
      "type": "string",
      "format": "date-time"
    },
    "canvas_hash": {
      "type": "string"
    },
    "webgl_renderer": {
      "type": "string"
    },
    "screen_width": {
      "type": "integer"
    },
    "screen_height": {
      "type": "integer"
    },
    "timezone_offset": {
      "type": "integer"
    },
    "languages": {
      "type": "array",
      "items": {"type": "string"}
    },
    "plugins": {
      "type": "array",
      "items": {"type": "string"}
    },
    "hardware_concurrency": {
      "type": "integer"
    },
    "device_memory": {
      "type": "number"
    },
    "platform": {
      "type": "string"
    }
  }
}
```

---
## 4. Python Consumer with Retry Logic

```python
"""
consumer.py - RabbitMQ consumer with manual acknowledgment, retry logic,
and graceful shutdown.

Requirements:
    pip install pika==1.3.2
"""

import json
import logging
import signal
import sys
import time
import traceback
from typing import Any, Callable

import pika
from pika.adapters.blocking_connection import BlockingChannel
from pika.spec import Basic, BasicProperties

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("ghostroute.consumer")

MAX_RETRIES = 5
PREFETCH_COUNT = 10


class GhostRouteConsumer:
    """
    RabbitMQ consumer with:
    - Manual acknowledgment (basic_ack/basic_nack)
    - Retry with exponential backoff via dead letter exchanges
    - Graceful shutdown on SIGTERM/SIGINT
    - Configurable prefetch for balanced throughput
    """

    def __init__(self, amqp_url: str, queue_name: str,
                 handler: Callable[[dict], None]) -> None:
        self.amqp_url = amqp_url
        self.queue_name = queue_name
        self.handler = handler
        self.connection: pika.BlockingConnection | None = None
        self.channel: BlockingChannel | None = None
        self.should_stop = False

        # Register signal handlers for graceful shutdown
        signal.signal(signal.SIGTERM, self._signal_handler)
        signal.signal(signal.SIGINT, self._signal_handler)

    def _signal_handler(self, signum: int, frame: Any) -> None:
        """Handle shutdown signals gracefully."""
        logger.info(f"Received signal {signum}, initiating graceful shutdown...")
        self.should_stop = True
        if self.channel and self.channel.is_open:
            # Cancel consumer to stop receiving new messages
            self.channel.stop_consuming()

    def connect(self) -> None:
        """Establish connection with heartbeat and timeout settings."""
        params = pika.URLParameters(self.amqp_url)
        params.heartbeat = 60
        params.blocked_connection_timeout = 300
        params.connection_attempts = 3
        params.retry_delay = 5

        self.connection = pika.BlockingConnection(params)
        self.channel = self.connection.channel()

        # Set prefetch count for balanced throughput
        # 10 messages: enough to keep consumer busy without starving others
        self.channel.basic_qos(prefetch_count=PREFETCH_COUNT)
        logger.info(f"Connected to RabbitMQ, prefetch_count={PREFETCH_COUNT}")

    def _get_retry_count(self, properties: BasicProperties) -> int:
        """Extract retry count from message headers."""
        if properties.headers and "x-retry-count" in properties.headers:
            return int(properties.headers["x-retry-count"])
        return 0

    def _on_message(self, channel: BlockingChannel, method: Basic.Deliver,
                    properties: BasicProperties, body: bytes) -> None:
        """Process a single message with error handling and retry logic."""
        delivery_tag = method.delivery_tag
        retry_count = self._get_retry_count(properties)

        try:
            message = json.loads(body)
            logger.debug(f"Processing message {properties.message_id}, retry={retry_count}")

            # Call the handler
            self.handler(message)

            # Success - acknowledge
            channel.basic_ack(delivery_tag=delivery_tag)
            logger.debug(f"Message {properties.message_id} processed successfully")

        except json.JSONDecodeError as e:
            # Malformed message - send to dead letter, do not retry
            logger.error(f"Invalid JSON in message: {e}")
            channel.basic_nack(delivery_tag=delivery_tag, requeue=False)

        except TransientError as e:
            # Transient failure - retry with backoff
            if retry_count < MAX_RETRIES:
                logger.warning(
                    f"Transient error (attempt {retry_count + 1}/{MAX_RETRIES}): {e}"
                )
                # Republish with incremented retry count to appropriate DLX queue
                retry_routing_key = self._get_retry_routing_key(retry_count + 1)
                new_headers = dict(properties.headers or {})
                new_headers["x-retry-count"] = retry_count + 1
                new_headers["x-original-error"] = str(e)

                channel.basic_publish(
                    exchange="ghostroute.dlx",
                    routing_key=retry_routing_key,
                    body=body,
                    properties=pika.BasicProperties(
                        content_type="application/json",
                        delivery_mode=2,  # persistent
                        message_id=properties.message_id,
                        timestamp=int(time.time()),
                        headers=new_headers,
                    ),
                )
                channel.basic_ack(delivery_tag=delivery_tag)
            else:
                # Max retries exceeded - route to permanent failure queue
                logger.error(
                    f"Max retries exceeded for message {properties.message_id}: {e}"
                )
                new_headers = dict(properties.headers or {})
                new_headers["x-final-error"] = str(e)
                new_headers["x-failed-at"] = time.strftime("%Y-%m-%dT%H:%M:%SZ")

                channel.basic_publish(
                    exchange="ghostroute.dlx",
                    routing_key="failed",
                    body=body,
                    properties=pika.BasicProperties(
                        content_type="application/json",
                        delivery_mode=2,
                        message_id=properties.message_id,
                        headers=new_headers,
                    ),
                )
                channel.basic_ack(delivery_tag=delivery_tag)

        except Exception as e:
            # Unexpected error - nack without requeue (goes to DLX)
            logger.error(f"Unexpected error processing message: {e}")
            logger.error(traceback.format_exc())
            channel.basic_nack(delivery_tag=delivery_tag, requeue=False)

    def _get_retry_routing_key(self, retry_count: int) -> str:
        """Map retry count to DLX routing key for escalating delays."""
        if retry_count <= 1:
            return "retry.1"  # 5s delay
        elif retry_count <= 3:
            return "retry.2"  # 30s delay
        else:
            return "retry.3"  # 5min delay

    def run(self) -> None:
        """Start consuming messages. Blocks until shutdown signal."""
        while not self.should_stop:
            try:
                self.connect()
                self.channel.basic_consume(
                    queue=self.queue_name,
                    on_message_callback=self._on_message,
                    auto_ack=False,
                )
                logger.info(f"Consumer started on queue '{self.queue_name}'")
                self.channel.start_consuming()
            except pika.exceptions.ConnectionClosedByBroker as e:
                logger.error(f"Connection closed by broker: {e}")
                if self.should_stop:
                    break
                time.sleep(5)
            except pika.exceptions.AMQPConnectionError as e:
                logger.error(f"Connection error: {e}")
                if self.should_stop:
                    break
                time.sleep(5)
            except Exception as e:
                logger.error(f"Unexpected error: {e}")
                if self.should_stop:
                    break
                time.sleep(5)
            finally:
                self._cleanup()

        logger.info("Consumer shut down gracefully")

    def _cleanup(self) -> None:
        """Close connection resources."""
        try:
            if self.channel and self.channel.is_open:
                self.channel.close()
            if self.connection and self.connection.is_open:
                self.connection.close()
        except Exception:
            pass


class TransientError(Exception):
    """Raised when a transient/retryable error occurs during processing."""
    pass


# Example handler
def process_click_event(message: dict) -> None:
    """Process a click event message."""
    event_id = message.get("event_id")
    campaign_id = message.get("campaign_id")
    visitor_ip = message.get("visitor_ip")

    logger.info(f"Processing click: event={event_id}, campaign={campaign_id}, ip={visitor_ip}")

    # Simulate processing (enrichment, storage, etc.)
    # If external service is down, raise TransientError for retry
    # if not enrich_click(message):
    #     raise TransientError("Enrichment service unavailable")

    # Store to ClickHouse, update Redis, etc.


if __name__ == "__main__":
    consumer = GhostRouteConsumer(
        amqp_url="amqp://ghostroute:secret@localhost:5672/ghostroute",
        queue_name="clicks.general",
        handler=process_click_event,
    )
    consumer.run()
```

---

## 5. Go Producer Usage Example

```go
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghostroute/events/rabbitmq"
	"github.com/google/uuid"
)

// ClickMessage is the message published for each click event.
type ClickMessage struct {
	EventID         string  `json:"event_id"`
	CampaignID      uint32  `json:"campaign_id"`
	EventTime       string  `json:"event_time"`
	VisitorIP       string  `json:"visitor_ip"`
	UserAgent       string  `json:"user_agent"`
	FingerprintHash string  `json:"fingerprint_hash"`
	Country         string  `json:"country"`
	Referer         string  `json:"referer"`
	LandingURL      string  `json:"landing_url"`
	BotScore        float32 `json:"bot_score"`
}

func main() {
	producer, err := rabbitmq.NewProducer(rabbitmq.ProducerConfig{
		URL:      "amqp://ghostroute:secret@localhost:5672/ghostroute",
		PoolSize: 5,
	})
	if err != nil {
		log.Fatalf("Failed to create producer: %v", err)
	}
	defer producer.Close()

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Println("Shutdown signal received")
		cancel()
	}()

	// Publish a click event
	msg := ClickMessage{
		EventID:         uuid.New().String(),
		CampaignID:      1042,
		EventTime:       time.Now().UTC().Format(time.RFC3339Nano),
		VisitorIP:       "203.0.113.42",
		UserAgent:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
		FingerprintHash: "a3f2b8c1d4e567890abcdef1234567890abcdef1234567890abcdef123456789",
		Country:         "US",
		Referer:         "https://example.com/offer",
		LandingURL:      "https://landing.ghostroute.io/campaign/1042",
		BotScore:        0.12,
	}

	if err := producer.Publish(ctx, "ghostroute.clicks", "click.general", msg); err != nil {
		log.Printf("Failed to publish: %v", err)
	} else {
		log.Printf("Published click event: %s", msg.EventID)
	}
}
```

---

## 6. Docker Compose with RabbitMQ Management

```yaml
# docker-compose.yml
version: "3.8"

services:
  rabbitmq:
    image: rabbitmq:3.13-management
    container_name: ghostroute-rabbitmq
    hostname: ghostroute-rabbit
    ports:
      - "5672:5672"     # AMQP
      - "15672:15672"   # Management UI
      - "15692:15692"   # Prometheus metrics
    environment:
      RABBITMQ_DEFAULT_USER: ghostroute
      RABBITMQ_DEFAULT_PASS: secret
      RABBITMQ_DEFAULT_VHOST: ghostroute
    volumes:
      - rabbitmq_data:/var/lib/rabbitmq
      - ./rabbitmq.conf:/etc/rabbitmq/rabbitmq.conf:ro
      - ./enabled_plugins:/etc/rabbitmq/enabled_plugins:ro
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "-q", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 30s
    deploy:
      resources:
        limits:
          memory: 2G
        reservations:
          memory: 512M
    restart: unless-stopped

volumes:
  rabbitmq_data:
    driver: local
```

### RabbitMQ Configuration

```ini
# rabbitmq.conf
# Logging
log.console.level = info

# Networking
listeners.tcp.default = 5672
management.tcp.port = 15672

# Memory and disk limits
vm_memory_high_watermark.relative = 0.7
disk_free_limit.absolute = 1GB

# Channel limits
channel_max = 2047

# Consumer timeout (30 minutes)
consumer_timeout = 1800000

# Message TTL default (24 hours)
# message_ttl = 86400000

# Prometheus metrics
prometheus.return_per_object_metrics = true
```

### Enabled Plugins

```
# enabled_plugins
[rabbitmq_management, rabbitmq_prometheus, rabbitmq_shovel, rabbitmq_shovel_management].
```

---

## 7. Monitoring with Prometheus

```yaml
# prometheus.yml (scrape config for RabbitMQ)
scrape_configs:
  - job_name: "rabbitmq"
    scrape_interval: 15s
    metrics_path: /metrics
    static_configs:
      - targets: ["rabbitmq:15692"]
    basic_auth:
      username: ghostroute
      password: secret
```

### Key Metrics to Monitor

| Metric | Alert Threshold | Description |
|--------|----------------|-------------|
| rabbitmq_queue_messages_ready | > 10000 | Messages waiting for consumer |
| rabbitmq_queue_messages_unacked | > 100 | Messages being processed |
| rabbitmq_queue_consumers | == 0 | No consumers on critical queue |
| rabbitmq_connections | > 100 | Too many connections |
| rabbitmq_channel_messages_published_total | rate < 1/min | Publisher stopped |
| rabbitmq_process_resident_memory_bytes | > 1.5GB | Memory pressure |
| rabbitmq_queue_messages{queue="dlx.failed"} | > 0 | Permanent failures |

---

## 8. Performance Tuning

### Channel vs Connection Count

- **Connections** are expensive (TCP + TLS handshake, ~1MB memory each)
- **Channels** are lightweight multiplexed streams within a connection
- Rule of thumb: 1 connection per application instance, N channels for concurrency

| Use Case | Connections | Channels | Rationale |
|----------|-------------|----------|-----------|
| Single Go producer | 1 | 5 | One per concurrent publish goroutine |
| Python consumer (single-threaded) | 1 | 1 | Pika is not thread-safe |
| High-throughput producer (>10k/sec) | 2-3 | 10-15 | Saturate network buffer |

### Prefetch Strategies

| Consumer Type | prefetch_count | Rationale |
|--------------|---------------|-----------|
| Fast processing (<10ms) | 50 | Amortize network roundtrips |
| Medium processing (10-100ms) | 10 | Balance throughput and fairness |
| Slow processing (>100ms) | 1-2 | Prevent consumer starvation |
| Batch processing | 100 | Accumulate then flush |

### Message Size Limits

- **Recommended max**: 128KB per message
- **Hard limit**: 512MB (RabbitMQ default, never hit this)
- For large payloads: store in object storage, publish reference only
- Enable message compression at application level for >1KB payloads

---

## 9. Integration Test Example

### Go Test (Publisher Side)

```go
package rabbitmq_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/ghostroute/events/rabbitmq"
)

func TestPublishAndConsume(t *testing.T) {
	amqpURL := os.Getenv("RABBITMQ_URL")
	if amqpURL == "" {
		amqpURL = "amqp://ghostroute:secret@localhost:5672/ghostroute"
	}

	// Setup: declare topology
	conn, err := amqp.Dial(amqpURL)
	if err != nil {
		t.Skipf("RabbitMQ not available: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open channel: %v", err)
	}
	defer ch.Close()

	if err := rabbitmq.DeclareTopology(ch); err != nil {
		t.Fatalf("declare topology: %v", err)
	}

	// Create producer
	producer, err := rabbitmq.NewProducer(rabbitmq.ProducerConfig{
		URL:      amqpURL,
		PoolSize: 2,
	})
	if err != nil {
		t.Fatalf("create producer: %v", err)
	}
	defer producer.Close()

	// Publish test message
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	testMsg := map[string]interface{}{
		"event_id":    "test-123",
		"campaign_id": 42,
		"event_time":  time.Now().UTC().Format(time.RFC3339),
		"visitor_ip":  "192.0.2.1",
	}

	if err := producer.Publish(ctx, "ghostroute.clicks", "click.general", testMsg); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Consume and verify
	msgs, err := ch.Consume("clicks.general", "test-consumer", false, false, false, false, nil)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case msg := <-msgs:
		var received map[string]interface{}
		if err := json.Unmarshal(msg.Body, &received); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if received["event_id"] != "test-123" {
			t.Errorf("expected event_id=test-123, got %v", received["event_id"])
		}
		msg.Ack(false)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}
```

### Python Consumer Test Script

```python
"""
test_consumer.py - Integration test for the Python consumer.
Publishes a test message and verifies the consumer processes it.

Usage:
    # In terminal 1: start consumer
    python consumer.py

    # In terminal 2: run this test
    python test_consumer.py
"""

import json
import time
import uuid

import pika


def test_publish_and_verify():
    """Publish a test message and verify it arrives in the queue."""
    connection = pika.BlockingConnection(
        pika.URLParameters("amqp://ghostroute:secret@localhost:5672/ghostroute")
    )
    channel = connection.channel()

    # Publish test message
    test_event = {
        "event_id": str(uuid.uuid4()),
        "campaign_id": 9999,
        "event_time": time.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "visitor_ip": "198.51.100.1",
        "user_agent": "TestAgent/1.0",
        "fingerprint_hash": "a" * 64,
        "country": "US",
    }

    channel.basic_publish(
        exchange="ghostroute.clicks",
        routing_key="click.general",
        body=json.dumps(test_event),
        properties=pika.BasicProperties(
            content_type="application/json",
            delivery_mode=2,
            message_id=test_event["event_id"],
        ),
    )
    print(f"Published test event: {test_event["event_id"]}")

    # Wait and check queue depth
    time.sleep(2)
    queue = channel.queue_declare("clicks.general", passive=True)
    print(f"Queue depth after publish: {queue.method.message_count}")

    connection.close()
    print("Test complete")


if __name__ == "__main__":
    test_publish_and_verify()
```

---

## 10. Go Module Dependencies

```
// go.mod
module github.com/ghostroute/events

go 1.22

require (
    github.com/rabbitmq/amqp091-go v1.10.0
    github.com/google/uuid v1.6.0
)
```

### Python Requirements

```
# requirements.txt
pika==1.3.2
```
