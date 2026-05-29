package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type contextKey string

const (
	userIDKey   contextKey = "user_id"
	tenantIDKey contextKey = "tenant_id"
)

// GetUserID extracts the user ID from the request context.
func GetUserID(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

// GetTenantID extracts the tenant ID from the request context.
func GetTenantID(ctx context.Context) string {
	if v, ok := ctx.Value(tenantIDKey).(string); ok {
		return v
	}
	return ""
}

// Middleware provides API key authentication middleware.
type Middleware struct {
	keyStore *KeyStore
	rdb      *redis.Client
	cacheTTL time.Duration
}

// NewMiddleware creates a new auth middleware.
func NewMiddleware(keyStore *KeyStore, rdb *redis.Client) *Middleware {
	return &Middleware{
		keyStore: keyStore,
		rdb:      rdb,
		cacheTTL: 5 * time.Minute,
	}
}

// AuthMiddleware returns an HTTP middleware that validates API keys.
func (m *Middleware) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			apiKey = r.URL.Query().Get("api_key")
		}

		if apiKey == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing API key"})
			return
		}

		userInfo, err := m.lookupKey(r.Context(), apiKey)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userInfo.UserID)
		ctx = context.WithValue(ctx, tenantIDKey, userInfo.TenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// lookupKey checks Redis cache first, then validates against the database.
func (m *Middleware) lookupKey(ctx context.Context, key string) (*UserInfo, error) {
	cacheKey := fmt.Sprintf("auth:key:%s", HashAPIKey(key))

	// Check Redis cache
	if m.rdb != nil {
		cached, err := m.rdb.Get(ctx, cacheKey).Result()
		if err == nil {
			var info UserInfo
			if jsonErr := json.Unmarshal([]byte(cached), &info); jsonErr == nil {
				return &info, nil
			}
		}
	}

	// Validate against database
	info, err := m.keyStore.ValidateAPIKey(ctx, key)
	if err != nil {
		return nil, err
	}

	// Cache in Redis
	if m.rdb != nil {
		data, _ := json.Marshal(info)
		m.rdb.Set(ctx, cacheKey, string(data), m.cacheTTL)
	}

	return info, nil
}
