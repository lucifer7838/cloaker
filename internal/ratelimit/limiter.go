package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DefaultLimit is the default rate limit per API key per hour.
	DefaultLimit = 1000
	// WindowSize is the sliding window duration.
	WindowSize = 1 * time.Hour
)

// RateLimiter implements per-API-key sliding window rate limiting via Redis sorted sets.
type RateLimiter struct {
	rdb          *redis.Client
	defaultLimit int
}

// NewRateLimiter creates a new rate limiter with the given Redis client.
func NewRateLimiter(rdb *redis.Client, defaultLimit int) *RateLimiter {
	if defaultLimit <= 0 {
		defaultLimit = DefaultLimit
	}
	return &RateLimiter{
		rdb:          rdb,
		defaultLimit: defaultLimit,
	}
}

// Allow checks if the request is within the rate limit for the given API key.
// Returns true if allowed, false if rate limited. Also returns remaining count.
func (rl *RateLimiter) Allow(ctx context.Context, apiKey string, limit int) (bool, int, error) {
	if limit <= 0 {
		limit = rl.defaultLimit
	}

	key := fmt.Sprintf("ratelimit:%s", apiKey)
	now := time.Now()
	nowUnix := float64(now.UnixNano())
	windowStart := float64(now.Add(-WindowSize).UnixNano())

	pipe := rl.rdb.Pipeline()

	// Remove expired entries outside the window
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%f", windowStart))

	// Count current entries in the window
	countCmd := pipe.ZCard(ctx, key)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("rate limit check pipeline: %w", err)
	}

	count := countCmd.Val()
	remaining := limit - int(count)

	if remaining <= 0 {
		return false, 0, nil
	}

	// Add new entry
	err = rl.rdb.ZAdd(ctx, key, redis.Z{
		Score:  nowUnix,
		Member: fmt.Sprintf("%d", now.UnixNano()),
	}).Err()
	if err != nil {
		return false, 0, fmt.Errorf("rate limit add: %w", err)
	}

	// Set TTL on the key
	rl.rdb.Expire(ctx, key, WindowSize)

	remaining--
	return true, remaining, nil
}

// Middleware returns an HTTP middleware that applies rate limiting.
// The keyExtractor function extracts the API key from the request.
func (rl *RateLimiter) Middleware(keyExtractor func(r *http.Request) string, limitExtractor func(r *http.Request) int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := keyExtractor(r)
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			limit := rl.defaultLimit
			if limitExtractor != nil {
				if l := limitExtractor(r); l > 0 {
					limit = l
				}
			}

			allowed, remaining, err := rl.Allow(r.Context(), apiKey, limit)
			if err != nil {
				// On error, allow the request through
				next.ServeHTTP(w, r)
				return
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			resetTime := time.Now().Add(WindowSize).Unix()
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime, 10))

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprintf(w, `{"error":"rate limit exceeded","retry_after_seconds":%d}`, int(WindowSize.Seconds()))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
