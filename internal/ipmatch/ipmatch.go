package ipmatch

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// RedisKeyIPv4ASN is the sorted set key for IPv4 ASN ranges.
	RedisKeyIPv4ASN = "asn:ipv4:ranges"
)

// ASNResult holds the result of an IP-to-ASN lookup.
type ASNResult struct {
	ASNumber uint32
	ASName   string
	Found    bool
}

// Client wraps a Redis connection for IP-to-ASN lookups.
type Client struct {
	rdb *redis.Client
}

// IPv4ToUint32 converts an IPv4 address to its 32-bit unsigned integer representation.
func IPv4ToUint32(ip net.IP) (uint32, error) {
	ipv4 := ip.To4()
	if ipv4 == nil {
		return 0, fmt.Errorf("not a valid IPv4 address: %s", ip.String())
	}
	return binary.BigEndian.Uint32(ipv4), nil
}

// NewClient creates a new IP match client with connection pooling.
func NewClient(addr, password string, db int) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     100,
		MinIdleConns: 10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	return &Client{rdb: rdb}
}

// Close closes the Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}

// LookupIPv4 performs an O(log n) lookup of an IPv4 address against the ASN sorted set.
func (c *Client) LookupIPv4(ctx context.Context, ipStr string) (ASNResult, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ASNResult{}, fmt.Errorf("invalid IP: %s", ipStr)
	}

	ipInt, err := IPv4ToUint32(ip)
	if err != nil {
		return ASNResult{}, err
	}

	// Find the first range whose end >= our IP
	results, err := c.rdb.ZRangeByScoreWithScores(ctx, RedisKeyIPv4ASN, &redis.ZRangeBy{
		Min:    strconv.FormatUint(uint64(ipInt), 10),
		Max:    "+inf",
		Offset: 0,
		Count:  1,
	}).Result()

	if err != nil {
		return ASNResult{}, fmt.Errorf("redis query failed: %w", err)
	}

	if len(results) == 0 {
		return ASNResult{Found: false}, nil
	}

	// Parse the member: "start_int|as_number|as_name"
	member := results[0].Member.(string)
	parts := strings.SplitN(member, "|", 3)
	if len(parts) != 3 {
		return ASNResult{}, fmt.Errorf("malformed member: %s", member)
	}

	// Verify IP >= range start
	rangeStart, err := strconv.ParseUint(parts[0], 10, 32)
	if err != nil {
		return ASNResult{}, fmt.Errorf("invalid range start: %s", parts[0])
	}

	if uint64(ipInt) < rangeStart {
		// IP is in the gap between ranges
		return ASNResult{Found: false}, nil
	}

	asNum, _ := strconv.ParseUint(parts[1], 10, 32)

	return ASNResult{
		ASNumber: uint32(asNum),
		ASName:   parts[2],
		Found:    true,
	}, nil
}
