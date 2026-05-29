package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// UserInfo holds information about an authenticated user.
type UserInfo struct {
	UserID    string
	TenantID  string
	RateLimit int
}

// KeyStore handles API key generation and validation against PostgreSQL.
type KeyStore struct {
	pool *pgxpool.Pool
}

// NewKeyStore creates a new KeyStore with the given database pool.
func NewKeyStore(pool *pgxpool.Pool) *KeyStore {
	return &KeyStore{pool: pool}
}

// GenerateAPIKey produces a new API key with the format: gr_ + 32 random hex characters.
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return "gr_" + hex.EncodeToString(bytes), nil
}

// HashAPIKey returns the SHA256 hash of a key for storage.
func HashAPIKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// ValidateAPIKey validates an API key against the database and returns user info.
func (ks *KeyStore) ValidateAPIKey(ctx context.Context, key string) (*UserInfo, error) {
	keyHash := HashAPIKey(key)

	var userID, tenantID string
	var rateLimit int
	var active bool
	err := ks.pool.QueryRow(ctx, `
		SELECT ak.active, u.id, u.id, ak.rate_limit
		FROM api_keys ak
		JOIN users u ON u.id = ak.user_id
		WHERE ak.key_hash = $1
	`, keyHash).Scan(&active, &userID, &tenantID, &rateLimit)
	if err != nil {
		return nil, fmt.Errorf("invalid API key")
	}

	if !active {
		return nil, fmt.Errorf("API key is inactive")
	}

	// Update last_used_at asynchronously
	go func() {
		updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = ks.pool.Exec(updateCtx, `
			UPDATE api_keys SET last_used_at = NOW() WHERE key_hash = $1
		`, keyHash)
	}()

	return &UserInfo{
		UserID:    userID,
		TenantID:  tenantID,
		RateLimit: rateLimit,
	}, nil
}
