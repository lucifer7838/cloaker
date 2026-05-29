package subnet

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultThreshold is the default number of bot hits before banning a /24 subnet.
	DefaultThreshold = 5
	// DefaultWindow is the default time window for counting bot hits.
	DefaultWindow = 1 * time.Hour
)

// SubnetBanner manages dynamic /24 subnet banning using Redis sorted sets.
type SubnetBanner struct {
	rdb       *redis.Client
	threshold int
	window    time.Duration
}

// NewSubnetBanner creates a new SubnetBanner with the given Redis client,
// threshold, and window. If threshold <= 0, DefaultThreshold is used.
// If window <= 0, DefaultWindow is used.
func NewSubnetBanner(rdb *redis.Client, threshold int, window time.Duration) *SubnetBanner {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	if window <= 0 {
		window = DefaultWindow
	}
	return &SubnetBanner{
		rdb:       rdb,
		threshold: threshold,
		window:    window,
	}
}

// extractSubnet24 returns the /24 prefix string for a given IP address.
func extractSubnet24(ipStr string) (string, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return "", fmt.Errorf("invalid IP address: %s", ipStr)
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return "", fmt.Errorf("not an IPv4 address: %s", ipStr)
	}
	return fmt.Sprintf("%d.%d.%d.0/24", ipv4[0], ipv4[1], ipv4[2]), nil
}

// RecordBotHit records a bot hit for the /24 subnet of the given IP.
// If the number of hits within the window reaches the threshold, the subnet is banned.
func (sb *SubnetBanner) RecordBotHit(ctx context.Context, ip string) error {
	subnet, err := extractSubnet24(ip)
	if err != nil {
		return err
	}

	hitsKey := fmt.Sprintf("subnet:hits:%s", subnet)
	now := time.Now()
	nowUnix := float64(now.UnixNano())
	windowStart := float64(now.Add(-sb.window).UnixNano())

	pipe := sb.rdb.Pipeline()

	// Remove expired entries outside the window
	pipe.ZRemRangeByScore(ctx, hitsKey, "-inf", fmt.Sprintf("%f", windowStart))

	// Add current hit with timestamp as score
	pipe.ZAdd(ctx, hitsKey, redis.Z{Score: nowUnix, Member: fmt.Sprintf("%d", now.UnixNano())})

	// Set TTL on the hits key
	pipe.Expire(ctx, hitsKey, sb.window)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("record bot hit pipeline: %w", err)
	}

	// Check count
	count, err := sb.rdb.ZCard(ctx, hitsKey).Result()
	if err != nil {
		return fmt.Errorf("check hit count: %w", err)
	}

	if count >= int64(sb.threshold) {
		bannedKey := fmt.Sprintf("subnet:banned:%s", subnet)
		err = sb.rdb.Set(ctx, bannedKey, "1", sb.window).Err()
		if err != nil {
			return fmt.Errorf("ban subnet: %w", err)
		}
	}

	return nil
}

// IsBanned checks if the /24 subnet of the given IP is currently banned.
func (sb *SubnetBanner) IsBanned(ctx context.Context, ip string) (bool, error) {
	subnet, err := extractSubnet24(ip)
	if err != nil {
		return false, err
	}

	bannedKey := fmt.Sprintf("subnet:banned:%s", subnet)
	exists, err := sb.rdb.Exists(ctx, bannedKey).Result()
	if err != nil {
		return false, fmt.Errorf("check ban status: %w", err)
	}

	return exists > 0, nil
}

// Unban manually removes a subnet ban.
func (sb *SubnetBanner) Unban(ctx context.Context, subnet string) error {
	bannedKey := fmt.Sprintf("subnet:banned:%s", subnet)
	hitsKey := fmt.Sprintf("subnet:hits:%s", subnet)

	pipe := sb.rdb.Pipeline()
	pipe.Del(ctx, bannedKey)
	pipe.Del(ctx, hitsKey)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("unban subnet: %w", err)
	}
	return nil
}
