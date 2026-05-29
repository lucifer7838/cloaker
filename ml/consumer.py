"""
consumer.py - RabbitMQ consumer with manual acknowledgment, retry logic,
and graceful shutdown for GhostRoute ML scoring.

Consumes click events, runs ML prediction, and caches scores in Redis.
"""

import json
import logging
import os
import signal
import sys
import time
import traceback
from typing import Any, Callable

import pika
import redis
from pika.adapters.blocking_connection import BlockingChannel
from pika.spec import Basic, BasicProperties

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("ghostroute.consumer")

MAX_RETRIES = 5
PREFETCH_COUNT = 10


class TransientError(Exception):
    """Raised when a transient/retryable error occurs during processing."""

    pass


class GhostRouteConsumer:
    """
    RabbitMQ consumer with:
    - Manual acknowledgment (basic_ack/basic_nack)
    - Retry with exponential backoff via dead letter exchanges
    - Graceful shutdown on SIGTERM/SIGINT
    - Configurable prefetch for balanced throughput
    """

    def __init__(
        self,
        amqp_url: str,
        queue_name: str,
        handler: Callable[[dict], None],
    ) -> None:
        self.amqp_url = amqp_url
        self.queue_name = queue_name
        self.handler = handler
        self.connection = None
        self.channel = None
        self.should_stop = False

        signal.signal(signal.SIGTERM, self._signal_handler)
        signal.signal(signal.SIGINT, self._signal_handler)

    def _signal_handler(self, signum: int, frame: Any) -> None:
        """Handle shutdown signals gracefully."""
        logger.info(
            f"Received signal {signum}, initiating graceful shutdown..."
        )
        self.should_stop = True
        if self.channel and self.channel.is_open:
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

        self.channel.basic_qos(prefetch_count=PREFETCH_COUNT)
        logger.info(
            f"Connected to RabbitMQ, prefetch_count={PREFETCH_COUNT}"
        )

    def _get_retry_count(self, properties: BasicProperties) -> int:
        """Extract retry count from message headers."""
        if properties.headers and "x-retry-count" in properties.headers:
            return int(properties.headers["x-retry-count"])
        return 0

    def _on_message(
        self,
        channel: BlockingChannel,
        method: Basic.Deliver,
        properties: BasicProperties,
        body: bytes,
    ) -> None:
        """Process a single message with error handling and retry logic."""
        delivery_tag = method.delivery_tag
        retry_count = self._get_retry_count(properties)

        try:
            message = json.loads(body)
            logger.debug(
                f"Processing message {properties.message_id}, retry={retry_count}"
            )

            self.handler(message)

            channel.basic_ack(delivery_tag=delivery_tag)
            logger.debug(
                f"Message {properties.message_id} processed successfully"
            )

        except json.JSONDecodeError as e:
            logger.error(f"Invalid JSON in message: {e}")
            channel.basic_nack(delivery_tag=delivery_tag, requeue=False)

        except TransientError as e:
            if retry_count < MAX_RETRIES:
                logger.warning(
                    f"Transient error (attempt {retry_count + 1}/{MAX_RETRIES}): {e}"
                )
                retry_routing_key = self._get_retry_routing_key(
                    retry_count + 1
                )
                new_headers = dict(properties.headers or {})
                new_headers["x-retry-count"] = retry_count + 1
                new_headers["x-original-error"] = str(e)

                channel.basic_publish(
                    exchange="ghostroute.dlx",
                    routing_key=retry_routing_key,
                    body=body,
                    properties=pika.BasicProperties(
                        content_type="application/json",
                        delivery_mode=2,
                        message_id=properties.message_id,
                        timestamp=int(time.time()),
                        headers=new_headers,
                    ),
                )
                channel.basic_ack(delivery_tag=delivery_tag)
            else:
                logger.error(
                    f"Max retries exceeded for message {properties.message_id}: {e}"
                )
                new_headers = dict(properties.headers or {})
                new_headers["x-final-error"] = str(e)
                new_headers["x-failed-at"] = time.strftime(
                    "%Y-%m-%dT%H:%M:%SZ"
                )

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
                logger.info(
                    f"Consumer started on queue '{self.queue_name}'"
                )
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


def make_click_handler(
    redis_client: redis.Redis, ml_service_url: str
) -> Callable[[dict], None]:
    """Create a click event handler that scores via ML and caches in Redis."""
    import urllib.request
    import urllib.error

    from train import FeatureEngineer

    feature_engineer = FeatureEngineer()

    def process_click_event(message: dict) -> None:
        """Process a click event: extract features, predict, cache score."""
        visitor_ip = message.get("visitor_ip", "")
        campaign_id = message.get("campaign_id", "")

        click_data = {
            "timestamp": time.time(),
            "recent_clicks_same_ip": [],
            "fingerprint": {
                "canvas_hash": message.get("fingerprint_hash", ""),
            },
            "headers": {},
            "mouse_data": [],
            "js_timing": {},
            "ja3": message.get("ja3_hash", ""),
        }

        features = feature_engineer.extract_features(click_data)

        try:
            payload = json.dumps(
                {"features": features, "visitor_id": visitor_ip}
            ).encode()
            req = urllib.request.Request(
                f"{ml_service_url}/predict",
                data=payload,
                headers={"Content-Type": "application/json"},
                method="POST",
            )
            with urllib.request.urlopen(req, timeout=5) as resp:
                result = json.loads(resp.read())
                score = result.get("probability", 0.0)
        except (urllib.error.URLError, OSError) as e:
            raise TransientError(f"ML service unavailable: {e}")

        cache_key = f"ml:score:{visitor_ip}:{campaign_id}"
        redis_client.setex(cache_key, 3600, str(score))

        logger.info(
            f"Scored click: ip={visitor_ip}, campaign={campaign_id}, score={score:.4f}"
        )

    return process_click_event


if __name__ == "__main__":
    amqp_url = os.environ.get(
        "RABBITMQ_URL", "amqp://ghostroute:secret@localhost:5672/"
    )
    redis_url = os.environ.get("REDIS_URL", "redis://localhost:6379/0")
    ml_url = os.environ.get("ML_SERVICE_URL", "http://localhost:8000")

    redis_client = redis.from_url(redis_url)

    handler = make_click_handler(redis_client, ml_url)

    consumer = GhostRouteConsumer(
        amqp_url=amqp_url,
        queue_name="clicks.general",
        handler=handler,
    )
    consumer.run()
