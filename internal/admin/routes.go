package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lucifer7838/ghostroute/internal/auth"
	"github.com/lucifer7838/ghostroute/internal/subnet"
	"github.com/redis/go-redis/v9"
)

// TrafficEvent represents a live traffic event broadcast to WebSocket clients.
type TrafficEvent struct {
	ID        string  `json:"id"`
	Timestamp string  `json:"timestamp"`
	IP        string  `json:"ip"`
	Country   string  `json:"country"`
	UserAgent string  `json:"user_agent"`
	Decision  string  `json:"decision"`
	Reason    string  `json:"reason"`
	Score     float64 `json:"score"`
	Campaign  string  `json:"campaign"`
}

// TrafficBroadcaster manages WebSocket clients for live traffic streaming.
type TrafficBroadcaster struct {
	mu      sync.RWMutex
	clients map[*websocket.Conn]*clientEntry
}

// clientEntry holds per-client write mutex to prevent concurrent writes.
type clientEntry struct {
	writeMu sync.Mutex
}

// NewTrafficBroadcaster creates a new TrafficBroadcaster.
func NewTrafficBroadcaster() *TrafficBroadcaster {
	return &TrafficBroadcaster{
		clients: make(map[*websocket.Conn]*clientEntry),
	}
}

// AddClient registers a WebSocket connection.
func (tb *TrafficBroadcaster) AddClient(conn *websocket.Conn) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.clients[conn] = &clientEntry{}
}

// RemoveClient removes a WebSocket connection.
func (tb *TrafficBroadcaster) RemoveClient(conn *websocket.Conn) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	delete(tb.clients, conn)
}

// Broadcast sends a traffic event to all connected WebSocket clients.
func (tb *TrafficBroadcaster) Broadcast(event TrafficEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	// Collect clients under lock
	tb.mu.RLock()
	type connEntry struct {
		conn  *websocket.Conn
		entry *clientEntry
	}
	snapshot := make([]connEntry, 0, len(tb.clients))
	for conn, entry := range tb.clients {
		snapshot = append(snapshot, connEntry{conn: conn, entry: entry})
	}
	tb.mu.RUnlock()

	// Write outside the lock, using per-client mutex
	for _, ce := range snapshot {
		ce.entry.writeMu.Lock()
		err := ce.conn.WriteMessage(websocket.TextMessage, data)
		ce.entry.writeMu.Unlock()
		if err != nil {
			ce.conn.Close()
			tb.RemoveClient(ce.conn)
		}
	}
}

// Handler holds dependencies for admin API routes.
type Handler struct {
	redisClient   *redis.Client
	pgPool        *pgxpool.Pool
	subnetBanner  *subnet.SubnetBanner
	mlServiceURL  string
	broadcaster   *TrafficBroadcaster
}

// NewHandler creates a new admin API handler.
func NewHandler(rdb *redis.Client, pgPool *pgxpool.Pool, sb *subnet.SubnetBanner, mlServiceURL string, broadcaster *TrafficBroadcaster) *Handler {
	return &Handler{
		redisClient:  rdb,
		pgPool:       pgPool,
		subnetBanner: sb,
		mlServiceURL: mlServiceURL,
		broadcaster:  broadcaster,
	}
}

// GetBroadcaster returns the traffic broadcaster for publishing events.
func (h *Handler) GetBroadcaster() *TrafficBroadcaster {
	return h.broadcaster
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for admin dashboard
	},
}

// RegisterRoutes registers all admin API routes on the given mux.
// If authMw is non-nil, all admin routes are protected by authentication.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, authMw *auth.Middleware) {
	wrap := func(handler http.HandlerFunc) http.Handler {
		if authMw != nil {
			return authMw.AuthMiddleware(http.HandlerFunc(handler))
		}
		return http.HandlerFunc(handler)
	}

	mux.Handle("/api/admin/model/info", wrap(h.handleModelInfo))
	mux.Handle("/api/admin/model/config", wrap(h.handleModelConfig))
	mux.Handle("/api/admin/subnets", wrap(h.handleSubnets))
	mux.Handle("/api/admin/subnets/unban", wrap(h.handleSubnetUnban))
	mux.Handle("/api/admin/keys", wrap(h.handleKeys))
	mux.Handle("/api/admin/keys/", wrap(h.handleKeyDelete))
	mux.Handle("/ws/traffic", wrap(h.handleWebSocket))
}

// handleModelInfo proxies model info from the ML service.
func (h *Handler) handleModelInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Try to get model info from ML service
	if h.mlServiceURL != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(h.mlServiceURL + "/model/info")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				io.Copy(w, resp.Body)
				return
			}
		}
	}

	// Return cached config from Redis if ML service is unavailable
	ctx := r.Context()
	configJSON, err := h.redisClient.Get(ctx, "admin:model:config").Result()
	if err == nil {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"version":"unknown","auc_roc":0,"auc_pr":0,"precision":0,"recall":0,"trained_at":"","config":%s}`, configJSON)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"version":"unknown","auc_roc":0,"auc_pr":0,"precision":0,"recall":0,"trained_at":""}`))
}

// handleModelConfig saves model configuration to Redis.
func (h *Handler) handleModelConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Validate JSON
	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	err = h.redisClient.Set(ctx, "admin:model:config", string(body), 0).Err()
	if err != nil {
		log.Printf("ERROR: save model config to redis: %v", err)
		http.Error(w, `{"error":"failed to save config"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// BannedSubnetInfo represents a banned subnet entry for the API response.
type BannedSubnetInfo struct {
	Subnet    string `json:"subnet"`
	BannedAt  string `json:"banned_at"`
	ExpiresAt string `json:"expires_at"`
	HitCount  int64  `json:"hit_count"`
}

// handleSubnets lists currently banned subnets from Redis.
func (h *Handler) handleSubnets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Scan for banned subnet keys
	var bannedSubnets []BannedSubnetInfo
	iter := h.redisClient.Scan(ctx, 0, "subnet:banned:*", 1000).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		subnetStr := strings.TrimPrefix(key, "subnet:banned:")

		ttl, err := h.redisClient.TTL(ctx, key).Result()
		if err != nil {
			continue
		}

		expiresAt := ""
		if ttl > 0 {
			expiresAt = time.Now().Add(ttl).Format(time.RFC3339)
		}

		// Get hit count from the hits sorted set
		hitsKey := fmt.Sprintf("subnet:hits:%s", subnetStr)
		hitCount, _ := h.redisClient.ZCard(ctx, hitsKey).Result()

		bannedSubnets = append(bannedSubnets, BannedSubnetInfo{
			Subnet:    subnetStr,
			BannedAt:  time.Now().Add(-ttl + time.Hour).Format(time.RFC3339), // Approximate
			ExpiresAt: expiresAt,
			HitCount:  hitCount,
		})
	}

	if bannedSubnets == nil {
		bannedSubnets = []BannedSubnetInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(bannedSubnets)
}

// handleSubnetUnban removes a subnet ban.
func (h *Handler) handleSubnetUnban(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Subnet string `json:"subnet"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Subnet == "" {
		http.Error(w, `{"error":"invalid request, subnet required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := h.subnetBanner.Unban(ctx, req.Subnet); err != nil {
		log.Printf("ERROR: unban subnet %s: %v", req.Subnet, err)
		http.Error(w, `{"error":"failed to unban subnet"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// APIKeyInfo represents an API key entry for the list response.
type APIKeyInfo struct {
	ID         string `json:"id"`
	KeyPrefix  string `json:"key_prefix"`
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	LastUsedAt string `json:"last_used_at"`
	RateLimit  int    `json:"rate_limit"`
	Active     bool   `json:"active"`
}

// handleKeys handles GET (list) and POST (create) for API keys.
func (h *Handler) handleKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listKeys(w, r)
	case http.MethodPost:
		h.createKey(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	if h.pgPool == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
		return
	}

	ctx := r.Context()
	rows, err := h.pgPool.Query(ctx, `
		SELECT id, key_prefix, name, created_at, last_used_at, rate_limit, active
		FROM api_keys
		ORDER BY created_at DESC
	`)
	if err != nil {
		log.Printf("ERROR: list api keys: %v", err)
		http.Error(w, `{"error":"failed to list keys"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var keys []APIKeyInfo
	for rows.Next() {
		var k APIKeyInfo
		var createdAt time.Time
		var lastUsedAt *time.Time
		err := rows.Scan(&k.ID, &k.KeyPrefix, &k.Name, &createdAt, &lastUsedAt, &k.RateLimit, &k.Active)
		if err != nil {
			log.Printf("ERROR: scan api key row: %v", err)
			continue
		}
		k.CreatedAt = createdAt.Format(time.RFC3339)
		if lastUsedAt != nil {
			k.LastUsedAt = lastUsedAt.Format(time.RFC3339)
		}
		keys = append(keys, k)
	}

	if keys == nil {
		keys = []APIKeyInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	if h.pgPool == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var req struct {
		Name      string `json:"name"`
		RateLimit int    `json:"rate_limit"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	if req.RateLimit <= 0 {
		req.RateLimit = 1000
	}

	// Generate new API key
	key, err := auth.GenerateAPIKey()
	if err != nil {
		log.Printf("ERROR: generate api key: %v", err)
		http.Error(w, `{"error":"failed to generate key"}`, http.StatusInternalServerError)
		return
	}

	keyHash := auth.HashAPIKey(key)
	keyPrefix := key[:7] // "gr_" + first 4 hex chars

	// Extract user_id from auth context (admin routes are behind auth middleware)
	ctx := r.Context()
	userID := auth.GetUserID(ctx)
	if userID == "" {
		http.Error(w, `{"error":"user_id not found in auth context"}`, http.StatusUnauthorized)
		return
	}

	_, err = h.pgPool.Exec(ctx, `
		INSERT INTO api_keys (id, key_hash, key_prefix, name, user_id, rate_limit, active, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, true, NOW())
	`, keyHash, keyPrefix, req.Name, userID, req.RateLimit)
	if err != nil {
		log.Printf("ERROR: insert api key: %v", err)
		http.Error(w, `{"error":"failed to create key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"key":    key,
		"status": "created",
	})
}

// handleKeyDelete handles DELETE /api/admin/keys/{id} to deactivate a key.
func (h *Handler) handleKeyDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if h.pgPool == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Extract key ID from path: /api/admin/keys/{id}
	path := r.URL.Path
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 1 {
		http.Error(w, `{"error":"key ID required"}`, http.StatusBadRequest)
		return
	}
	keyID := parts[len(parts)-1]
	if keyID == "" || keyID == "keys" {
		http.Error(w, `{"error":"key ID required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	_, err := h.pgPool.Exec(ctx, `
		UPDATE api_keys SET active = false WHERE id = $1
	`, keyID)
	if err != nil {
		log.Printf("ERROR: deactivate api key: %v", err)
		http.Error(w, `{"error":"failed to revoke key"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"revoked"}`))
}

// handleWebSocket upgrades the connection and streams live traffic events.
func (h *Handler) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ERROR: websocket upgrade: %v", err)
		return
	}

	h.broadcaster.AddClient(conn)

	// Keep connection alive, remove on close
	defer func() {
		h.broadcaster.RemoveClient(conn)
		conn.Close()
	}()

	// Read loop to detect client disconnect
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// BroadcastEvent is a convenience function to publish a traffic event.
func (h *Handler) BroadcastEvent(ctx context.Context, event TrafficEvent) {
	h.broadcaster.Broadcast(event)
}
