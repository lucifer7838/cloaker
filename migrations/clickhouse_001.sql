-- GhostRoute ClickHouse schema - Initial migration
-- Following SKILL_clickhouse_ghostroute.md patterns

CREATE DATABASE IF NOT EXISTS ghostroute;

-- Main visits/click tracking table
CREATE TABLE IF NOT EXISTS ghostroute.visits
(
    event_id     UUID DEFAULT generateUUIDv4(),
    campaign_id  UInt32,
    event_time   DateTime64(3, 'UTC'),
    visitor_ip   IPv6,
    user_agent   String,
    country      LowCardinality(String) DEFAULT '',
    device_type  Enum8(
                     'desktop' = 1,
                     'mobile' = 2,
                     'tablet' = 3,
                     'bot' = 4,
                     'unknown' = 5
                 ) DEFAULT 'unknown',
    os           LowCardinality(String) DEFAULT '',
    browser      LowCardinality(String) DEFAULT '',
    referer      String DEFAULT '',
    landing_url  String DEFAULT '',
    is_bot       UInt8 DEFAULT 0,
    bot_score    Float32 DEFAULT 0.0,
    decision     LowCardinality(String) DEFAULT '',
    reason       String DEFAULT '',
    asn_number   UInt32 DEFAULT 0,
    asn_name     LowCardinality(String) DEFAULT '',
    ja3_hash     String DEFAULT ''
)
ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(event_time)
ORDER BY (campaign_id, event_time)
TTL event_time + INTERVAL 90 DAY DELETE
SETTINGS index_granularity = 8192, ttl_only_drop_parts = 1;

-- Materialized view: hourly stats (SummingMergeTree)
CREATE MATERIALIZED VIEW IF NOT EXISTS ghostroute.mv_hourly_stats
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
FROM ghostroute.visits
GROUP BY campaign_id, hour, country, device_type;

-- Materialized view: daily campaign rollups (SummingMergeTree)
CREATE MATERIALIZED VIEW IF NOT EXISTS ghostroute.mv_campaign_daily
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
    sum(is_bot)                      AS total_bots,
    countIf(device_type = 'mobile')  AS mobile_clicks,
    countIf(device_type = 'desktop') AS desktop_clicks
FROM ghostroute.visits
GROUP BY campaign_id, day;
