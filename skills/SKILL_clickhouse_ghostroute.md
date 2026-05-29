# SKILL: ClickHouse Schema and Go Bulk Insert for GhostRoute Analytics

## Purpose

Complete ClickHouse DDL and Go insertion patterns for GhostRoute click tracking,
fingerprint storage, conversion tracking, and real-time analytics aggregation.
Uses MergeTree family engines with proper partitioning, TTL-based retention,
and materialized views for sub-second dashboard queries.

## Version Pins

- Go: `github.com/ClickHouse/clickhouse-go/v2 v2.30.0`
- ClickHouse Server: 24.3 LTS
- Docker Image: `clickhouse/clickhouse-server:24.3`

```
// go.mod
module github.com/ghostroute/analytics

go 1.22

require (
    github.com/ClickHouse/clickhouse-go/v2 v2.30.0
    github.com/google/uuid v1.6.0
)
```

---

## 1. Column Type Rationale

| Column | Type | Reason |
|--------|------|--------|
| event_id | UUID | Native 128-bit UUID, no string overhead |
| campaign_id | UInt32 | Up to ~4 billion campaigns, 4 bytes vs 8 for UInt64 |
| event_time | DateTime64(3) | Millisecond precision for click deduplication |
| visitor_ip | IPv6 | Stores both IPv4 (mapped as ::ffff:x.x.x.x) and IPv6 in 16 bytes |
| user_agent | String | Variable length, no LowCardinality due to high cardinality |
| fingerprint_hash | FixedString(32) | SHA-256 binary is always 32 bytes fixed |
| country | LowCardinality(String) | ~250 countries, dictionary encoding saves 90%+ space |
| device_type | Enum8 | 1 byte, type-safe, no string comparison cost |
| os | LowCardinality(String) | ~50 OS variants, dictionary encoding |
| browser | LowCardinality(String) | ~30 browsers, dictionary encoding |
| referer | String | Variable length URLs, too many unique values for LowCardinality |
| landing_url | String | Variable length, high cardinality |
| is_bot | UInt8 | Boolean as 0/1, 1 byte |
| bot_score | Float32 | ML model confidence, 4 bytes sufficient precision |
| ja3_hash | FixedString(32) | JA3 MD5 hash stored as fixed 32 bytes |

---

## 2. Click Tracking Table (Primary)

```sql
CREATE DATABASE IF NOT EXISTS ghostroute;

CREATE TABLE ghostroute.clicks
(
    event_id         UUID DEFAULT generateUUIDv4(),
    campaign_id      UInt32,
    event_time       DateTime64(3, 'UTC'),
    visitor_ip       IPv6,
    user_agent       String,
    fingerprint_hash FixedString(32),
    country          LowCardinality(String) DEFAULT '',
    device_type      Enum8(
                         'desktop' = 1,
                         'mobile' = 2,
                         'tablet' = 3,
                         'bot' = 4,
                         'unknown' = 5
                     ) DEFAULT 'unknown',
    os               LowCardinality(String) DEFAULT '',
    browser          LowCardinality(String) DEFAULT '',
    referer          String DEFAULT '',
    landing_url      String DEFAULT '',
    is_bot           UInt8 DEFAULT 0,
    bot_score        Float32 DEFAULT 0.0,
    ja3_hash         FixedString(32) DEFAULT unhex('00000000000000000000000000000000')
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (campaign_id, event_time)
TTL event_time + INTERVAL 90 DAY DELETE
SETTINGS
    index_granularity = 8192,
    ttl_only_drop_parts = 1;
```

---

## 3. Visitor Fingerprint Storage (Deduplication)

```sql
CREATE TABLE ghostroute.fingerprints
(
    fingerprint_hash     FixedString(32),
    first_seen           DateTime64(3, 'UTC'),
    last_seen            DateTime64(3, 'UTC'),
    visit_count          UInt32 DEFAULT 1,
    canvas_hash          String DEFAULT '',
    webgl_renderer       String DEFAULT '',
    screen_width         UInt16 DEFAULT 0,
    screen_height        UInt16 DEFAULT 0,
    timezone_offset      Int16 DEFAULT 0,
    language             LowCardinality(String) DEFAULT '',
    platform             LowCardinality(String) DEFAULT '',
    hardware_concurrency UInt8 DEFAULT 0,
    device_memory        Float32 DEFAULT 0,
    is_known_bot         UInt8 DEFAULT 0,
    cluster_id           UInt32 DEFAULT 0
)
ENGINE = ReplacingMergeTree(last_seen)
ORDER BY fingerprint_hash
TTL last_seen + INTERVAL 365 DAY DELETE
SETTINGS index_granularity = 8192;
```

The `ReplacingMergeTree(last_seen)` ensures that on merge, only the row with the
latest `last_seen` value is kept per unique `fingerprint_hash` (the ORDER BY key).
This provides natural deduplication without explicit UPSERT logic.

---

## 4. Conversion/Postback Events Table

```sql
CREATE TABLE ghostroute.conversions
(
    conversion_id    UUID DEFAULT generateUUIDv4(),
    click_id         UUID,
    campaign_id      UInt32,
    event_time       DateTime64(3, 'UTC'),
    conversion_type  LowCardinality(String) DEFAULT 'lead',
    payout           Decimal64(4) DEFAULT 0,
    currency         LowCardinality(String) DEFAULT 'USD',
    sub_id           String DEFAULT '',
    postback_url     String DEFAULT '',
    postback_status  UInt16 DEFAULT 0,
    visitor_ip       IPv6,
    fingerprint_hash FixedString(32)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (campaign_id, click_id, event_time)
TTL event_time + INTERVAL 180 DAY DELETE
SETTINGS index_granularity = 8192;
```

---

## 5. Real-Time Aggregation Materialized Views

### 5.1 Hourly Click Stats

```sql
CREATE MATERIALIZED VIEW ghostroute.mv_hourly_stats
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (campaign_id, hour, country, device_type)
TTL hour + INTERVAL 730 DAY DELETE
AS
SELECT
    campaign_id,
    toStartOfHour(event_time) AS hour,
    country,
    device_type,
    count()               AS click_count,
    uniqExact(visitor_ip) AS unique_visitors,
    sum(is_bot)           AS bot_count,
    avg(bot_score)        AS avg_bot_score
FROM ghostroute.clicks
GROUP BY campaign_id, hour, country, device_type;
```

### 5.2 Campaign Rollups (Daily)

```sql
CREATE MATERIALIZED VIEW ghostroute.mv_campaign_daily
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(day)
ORDER BY (campaign_id, day)
TTL day + INTERVAL 730 DAY DELETE
AS
SELECT
    campaign_id,
    toDate(event_time) AS day,
    count()                          AS total_clicks,
    uniqExact(visitor_ip)            AS unique_ips,
    uniqExact(fingerprint_hash)      AS unique_fingerprints,
    sum(is_bot)                      AS total_bots,
    countIf(device_type = 'mobile')  AS mobile_clicks,
    countIf(device_type = 'desktop') AS desktop_clicks
FROM ghostroute.clicks
GROUP BY campaign_id, day;
```

### 5.3 Geo Breakdown

```sql
CREATE MATERIALIZED VIEW ghostroute.mv_geo_breakdown
ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (campaign_id, country, hour)
TTL hour + INTERVAL 365 DAY DELETE
AS
SELECT
    campaign_id,
    country,
    toStartOfHour(event_time) AS hour,
    count()               AS clicks,
    uniqExact(visitor_ip) AS unique_ips,
    sum(is_bot)           AS bots
FROM ghostroute.clicks
GROUP BY campaign_id, country, hour;
```

---

## 6. Go Connection Setup with TLS and Compression

```go
package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Config holds ClickHouse connection parameters.
type Config struct {
	Hosts        []string
	Database     string
	Username     string
	Password     string
	MaxOpenConns int
	MaxIdleConns int
	TLSEnabled   bool
}

// NewConnection creates a new ClickHouse connection with TLS and LZ4 compression.
func NewConnection(cfg Config) (driver.Conn, error) {
	opts := &clickhouse.Options{
		Addr: cfg.Hosts,
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		Compression: &clickhouse.Compression{
			Method: clickhouse.CompressionLZ4,
		},
		DialTimeout:     10 * time.Second,
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: 1 * time.Hour,
	}

	if cfg.TLSEnabled {
		opts.TLS = &tls.Config{
			InsecureSkipVerify: false,
			MinVersion:         tls.VersionTLS12,
		}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	return conn, nil
}
```

---

## 7. Batch Insert Using PrepareBatch Pattern

```go
package clickhouse

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// ClickEvent represents a single click event for insertion.
type ClickEvent struct {
	EventID         uuid.UUID
	CampaignID      uint32
	EventTime       time.Time
	VisitorIP       net.IP
	UserAgent       string
	FingerprintHash [32]byte
	Country         string
	DeviceType      string
	OS              string
	Browser         string
	Referer         string
	LandingURL      string
	IsBot           uint8
	BotScore        float32
	JA3Hash         [32]byte
}

// BatchInsertClicks inserts a batch of click events using the PrepareBatch pattern.
// This is the recommended approach for maximum throughput. Do NOT use PrepareAsync
// as it does not guarantee delivery and complicates error handling.
func BatchInsertClicks(ctx context.Context, conn driver.Conn, events []ClickEvent) error {
	batch, err := conn.PrepareBatch(ctx, `
		INSERT INTO ghostroute.clicks (
			event_id, campaign_id, event_time, visitor_ip, user_agent,
			fingerprint_hash, country, device_type, os, browser,
			referer, landing_url, is_bot, bot_score, ja3_hash
		)
	`)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, e := range events {
		err := batch.Append(
			e.EventID,
			e.CampaignID,
			e.EventTime,
			e.VisitorIP,
			e.FingerprintHash[:],
			e.Country,
			e.DeviceType,
			e.OS,
			e.Browser,
			e.Referer,
			e.LandingURL,
			e.IsBot,
			e.BotScore,
			e.JA3Hash[:],
		)
		if err != nil {
			return fmt.Errorf("batch append: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("batch send: %w", err)
	}

	return nil
}
```

---

## 8. Column-Oriented Inserts for Maximum Throughput

```go
package clickhouse

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// ColumnInsertClicks uses column-oriented insertion for maximum throughput.
// This avoids per-row overhead and allows ClickHouse to process data in native
// columnar format without row-to-column conversion.
func ColumnInsertClicks(ctx context.Context, conn driver.Conn, events []ClickEvent) error {
	batch, err := conn.PrepareBatch(ctx, `
		INSERT INTO ghostroute.clicks (
			event_id, campaign_id, event_time, visitor_ip, user_agent,
			fingerprint_hash, country, device_type, os, browser,
			referer, landing_url, is_bot, bot_score, ja3_hash
		)
	`)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	n := len(events)
	eventIDs := make([]uuid.UUID, n)
	campaignIDs := make([]uint32, n)
	eventTimes := make([]time.Time, n)
	visitorIPs := make([]net.IP, n)
	userAgents := make([]string, n)
	fingerprintHashes := make([][32]byte, n)
	countries := make([]string, n)
	deviceTypes := make([]string, n)
	oses := make([]string, n)
	browsers := make([]string, n)
	referers := make([]string, n)
	landingURLs := make([]string, n)
	isBots := make([]uint8, n)
	botScores := make([]float32, n)
	ja3Hashes := make([][32]byte, n)

	for i, e := range events {
		eventIDs[i] = e.EventID
		campaignIDs[i] = e.CampaignID
		eventTimes[i] = e.EventTime
		visitorIPs[i] = e.VisitorIP
		userAgents[i] = e.UserAgent
		fingerprintHashes[i] = e.FingerprintHash
		countries[i] = e.Country
		deviceTypes[i] = e.DeviceType
		oses[i] = e.OS
		browsers[i] = e.Browser
		referers[i] = e.Referer
		landingURLs[i] = e.LandingURL
		isBots[i] = e.IsBot
		botScores[i] = e.BotScore
		ja3Hashes[i] = e.JA3Hash
	}

	if err := batch.Column(0).Append(eventIDs); err != nil {
		return fmt.Errorf("column event_id: %w", err)
	}
	if err := batch.Column(1).Append(campaignIDs); err != nil {
		return fmt.Errorf("column campaign_id: %w", err)
	}
	if err := batch.Column(2).Append(eventTimes); err != nil {
		return fmt.Errorf("column event_time: %w", err)
	}
	if err := batch.Column(3).Append(visitorIPs); err != nil {
		return fmt.Errorf("column visitor_ip: %w", err)
	}
	if err := batch.Column(4).Append(userAgents); err != nil {
		return fmt.Errorf("column user_agent: %w", err)
	}
	if err := batch.Column(5).Append(fingerprintHashes); err != nil {
		return fmt.Errorf("column fingerprint_hash: %w", err)
	}
	if err := batch.Column(6).Append(countries); err != nil {
		return fmt.Errorf("column country: %w", err)
	}
	if err := batch.Column(7).Append(deviceTypes); err != nil {
		return fmt.Errorf("column device_type: %w", err)
	}
	if err := batch.Column(8).Append(oses); err != nil {
		return fmt.Errorf("column os: %w", err)
	}
	if err := batch.Column(9).Append(browsers); err != nil {
		return fmt.Errorf("column browser: %w", err)
	}
	if err := batch.Column(10).Append(referers); err != nil {
		return fmt.Errorf("column referer: %w", err)
	}
	if err := batch.Column(11).Append(landingURLs); err != nil {
		return fmt.Errorf("column landing_url: %w", err)
	}
	if err := batch.Column(12).Append(isBots); err != nil {
		return fmt.Errorf("column is_bot: %w", err)
	}
	if err := batch.Column(13).Append(botScores); err != nil {
		return fmt.Errorf("column bot_score: %w", err)
	}
	if err := batch.Column(14).Append(ja3Hashes); err != nil {
		return fmt.Errorf("column ja3_hash: %w", err)
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("column batch send: %w", err)
	}

	return nil
}
```

---

## 9. Connection Pooling Configuration

```go
// Recommended production settings for connection pooling:
//
// MaxOpenConns: 10
//   - ClickHouse handles parallelism internally per query
//   - Too many connections waste server memory (~1MB each)
//   - 10 connections support 10 concurrent batch inserts
//
// MaxIdleConns: 5
//   - Keep half the pool warm for burst traffic
//   - Idle connections are cheap but still consume server slots
//
// ConnMaxLifetime: 1 hour
//   - Prevents stale connections after network changes
//   - Forces periodic reconnection for load balancer redistribution

func DefaultConfig() Config {
	return Config{
		Hosts:        []string{"clickhouse-node1:9440", "clickhouse-node2:9440"},
		Database:     "ghostroute",
		Username:     "ghostroute_app",
		Password:     "",  // from environment variable
		MaxOpenConns: 10,
		MaxIdleConns: 5,
		TLSEnabled:   true,
	}
}
```

---

## 10. Retention Policies Summary

| Data Type | Engine | TTL | Rationale |
|-----------|--------|-----|-----------|
| Raw clicks | MergeTree | 90 days | High volume, only needed for debugging |
| Conversions | MergeTree | 180 days | Revenue data, keep longer for reconciliation |
| Fingerprints | ReplacingMergeTree | 365 days | Deduplication reference, moderate volume |
| Hourly stats (MV) | SummingMergeTree | 730 days (2 years) | Dashboard queries, tiny rows |
| Campaign daily (MV) | SummingMergeTree | 730 days (2 years) | Reporting, tiny rows |
| Geo breakdown (MV) | SummingMergeTree | 365 days | Geographic trends |

The `ttl_only_drop_parts = 1` setting ensures TTL only drops entire parts (not individual rows),
which is dramatically more efficient for large tables.

---

## 11. Sample Analytical Queries

### CTR Calculation (Click-Through Rate)

```sql
SELECT
    campaign_id,
    day,
    total_clicks,
    total_clicks / nullIf(impressions, 0) AS ctr
FROM ghostroute.mv_campaign_daily
WHERE campaign_id = 1042
  AND day >= today() - 30
ORDER BY day DESC;
```

### Conversion Rate by Campaign

```sql
SELECT
    c.campaign_id,
    countDistinct(c.click_id) AS converting_clicks,
    cl.total AS total_clicks,
    converting_clicks / nullIf(cl.total, 0) AS conversion_rate,
    sum(c.payout) AS total_revenue
FROM ghostroute.conversions c
INNER JOIN (
    SELECT campaign_id, count() AS total
    FROM ghostroute.clicks
    WHERE event_time >= now() - INTERVAL 7 DAY
    GROUP BY campaign_id
) cl ON c.campaign_id = cl.campaign_id
WHERE c.event_time >= now() - INTERVAL 7 DAY
GROUP BY c.campaign_id, cl.total
ORDER BY total_revenue DESC
LIMIT 50;
```

### Geo Distribution (Top Countries)

```sql
SELECT
    country,
    sum(clicks) AS total_clicks,
    sum(unique_ips) AS total_unique_ips,
    sum(bots) AS total_bots,
    round(sum(bots) / nullIf(sum(clicks), 0) * 100, 2) AS bot_percentage
FROM ghostroute.mv_geo_breakdown
WHERE campaign_id = 1042
  AND hour >= now() - INTERVAL 24 HOUR
GROUP BY country
ORDER BY total_clicks DESC
LIMIT 20;
```

### Device Breakdown

```sql
SELECT
    device_type,
    count() AS clicks,
    uniqExact(visitor_ip) AS unique_visitors,
    round(avg(bot_score), 3) AS avg_bot_score,
    sum(is_bot) AS flagged_bots
FROM ghostroute.clicks
WHERE campaign_id = 1042
  AND event_time >= now() - INTERVAL 24 HOUR
GROUP BY device_type
ORDER BY clicks DESC;
```

### Bot Rate Over Time (Hourly Trend)

```sql
SELECT
    hour,
    sum(click_count) AS total_clicks,
    sum(bot_count) AS total_bots,
    round(sum(bot_count) / nullIf(sum(click_count), 0) * 100, 2) AS bot_rate_pct
FROM ghostroute.mv_hourly_stats
WHERE campaign_id = 1042
  AND hour >= now() - INTERVAL 72 HOUR
GROUP BY hour
ORDER BY hour ASC;
```

---

## 12. Performance Tuning Settings

```sql
-- Apply to the ghostroute user profile or per-session:

-- Maximum block size for inserts (default 1048449)
-- Increase for large batches to reduce merge overhead
SET max_insert_block_size = 1048576;

-- Enable async inserts for high-frequency small inserts
-- Buffers inserts server-side and flushes periodically
SET async_insert = 1;
SET wait_for_async_insert = 0;

-- Minimum rows before flushing an async insert buffer
SET async_insert_max_data_size = 10485760;  -- 10MB buffer

-- Minimum rows to trigger flush
SET min_insert_block_size_rows = 100000;

-- Minimum bytes to trigger flush
SET min_insert_block_size_bytes = 10485760;  -- 10MB
```

### User Profile for GhostRoute Application

```sql
CREATE SETTINGS PROFILE ghostroute_app_profile
SETTINGS
    max_insert_block_size = 1048576,
    async_insert = 1,
    wait_for_async_insert = 0,
    min_insert_block_size_rows = 100000,
    min_insert_block_size_bytes = 10485760,
    max_execution_time = 60,
    max_memory_usage = 10000000000  -- 10GB per query
TO ghostroute_app;
```

### Recommended Insert Strategy

| Scenario | Strategy | Settings |
|----------|----------|----------|
| Real-time clicks (< 100/sec) | Async inserts | async_insert=1, buffer 1s |
| Batch import (millions) | PrepareBatch, column-oriented | max_insert_block_size=1M |
| Postback/conversion | Sync insert, small batches | Default settings |

---

## 13. Docker Run Command for Local Development

```bash
# Single-node ClickHouse for development
docker run -d \
  --name clickhouse-dev \
  -p 8123:8123 \
  -p 9000:9000 \
  -p 9440:9440 \
  -v clickhouse_data:/var/lib/clickhouse \
  -v clickhouse_logs:/var/log/clickhouse-server \
  -e CLICKHOUSE_USER=ghostroute_app \
  -e CLICKHOUSE_PASSWORD=dev_password_123 \
  -e CLICKHOUSE_DB=ghostroute \
  -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 \
  --ulimit nofile=262144:262144 \
  clickhouse/clickhouse-server:24.3

# Verify it is running
curl http://localhost:8123/ping

# Connect with clickhouse-client
docker exec -it clickhouse-dev clickhouse-client \
  --user ghostroute_app \
  --password dev_password_123 \
  --database ghostroute

# Apply schema
docker exec -i clickhouse-dev clickhouse-client \
  --user ghostroute_app \
  --password dev_password_123 \
  < schema.sql
```

---

## 14. Complete Schema Migration Script

```sql
-- schema.sql - Run this to set up the full GhostRoute analytics schema

CREATE DATABASE IF NOT EXISTS ghostroute;

-- Main click tracking table
CREATE TABLE IF NOT EXISTS ghostroute.clicks
(
    event_id         UUID DEFAULT generateUUIDv4(),
    campaign_id      UInt32,
    event_time       DateTime64(3, 'UTC'),
    visitor_ip       IPv6,
    user_agent       String,
    fingerprint_hash FixedString(32),
    country          LowCardinality(String) DEFAULT '',
    device_type      Enum8(
                         'desktop' = 1,
                         'mobile' = 2,
                         'tablet' = 3,
                         'bot' = 4,
                         'unknown' = 5
                     ) DEFAULT 'unknown',
    os               LowCardinality(String) DEFAULT '',
    browser          LowCardinality(String) DEFAULT '',
    referer          String DEFAULT '',
    landing_url      String DEFAULT '',
    is_bot           UInt8 DEFAULT 0,
    bot_score        Float32 DEFAULT 0.0,
    ja3_hash         FixedString(32) DEFAULT unhex('00000000000000000000000000000000')
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (campaign_id, event_time)
TTL event_time + INTERVAL 90 DAY DELETE
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- Fingerprint deduplication table
CREATE TABLE IF NOT EXISTS ghostroute.fingerprints
(
    fingerprint_hash     FixedString(32),
    first_seen           DateTime64(3, 'UTC'),
    last_seen            DateTime64(3, 'UTC'),
    visit_count          UInt32 DEFAULT 1,
    canvas_hash          String DEFAULT '',
    webgl_renderer       String DEFAULT '',
    screen_width         UInt16 DEFAULT 0,
    screen_height        UInt16 DEFAULT 0,
    timezone_offset      Int16 DEFAULT 0,
    language             LowCardinality(String) DEFAULT '',
    platform             LowCardinality(String) DEFAULT '',
    hardware_concurrency UInt8 DEFAULT 0,
    device_memory        Float32 DEFAULT 0,
    is_known_bot         UInt8 DEFAULT 0,
    cluster_id           UInt32 DEFAULT 0
)
ENGINE = ReplacingMergeTree(last_seen)
ORDER BY fingerprint_hash
TTL last_seen + INTERVAL 365 DAY DELETE
SETTINGS index_granularity = 8192;

-- Conversion tracking
CREATE TABLE IF NOT EXISTS ghostroute.conversions
(
    conversion_id    UUID DEFAULT generateUUIDv4(),
    click_id         UUID,
    campaign_id      UInt32,
    event_time       DateTime64(3, 'UTC'),
    conversion_type  LowCardinality(String) DEFAULT 'lead',
    payout           Decimal64(4) DEFAULT 0,
    currency         LowCardinality(String) DEFAULT 'USD',
    sub_id           String DEFAULT '',
    postback_url     String DEFAULT '',
    postback_status  UInt16 DEFAULT 0,
    visitor_ip       IPv6,
    fingerprint_hash FixedString(32)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (campaign_id, click_id, event_time)
TTL event_time + INTERVAL 180 DAY DELETE
SETTINGS index_granularity = 8192;
```

---

## 15. Monitoring Queries

```sql
-- Table sizes
SELECT
    table,
    formatReadableSize(sum(bytes_on_disk)) AS disk_size,
    sum(rows) AS total_rows,
    count() AS parts
FROM system.parts
WHERE database = 'ghostroute' AND active
GROUP BY table
ORDER BY sum(bytes_on_disk) DESC;

-- Insert rate (last hour)
SELECT
    toStartOfMinute(event_time) AS minute,
    count() AS inserts_per_minute
FROM system.query_log
WHERE query_kind = 'Insert'
  AND database = 'ghostroute'
  AND event_time >= now() - INTERVAL 1 HOUR
GROUP BY minute
ORDER BY minute DESC;

-- Partition stats
SELECT
    table,
    partition,
    count() AS parts,
    sum(rows) AS rows,
    formatReadableSize(sum(bytes_on_disk)) AS size
FROM system.parts
WHERE database = 'ghostroute' AND active
GROUP BY table, partition
ORDER BY table, partition DESC;
```
