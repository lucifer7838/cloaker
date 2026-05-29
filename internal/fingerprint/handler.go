package fingerprint

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lucifer7838/ghostroute/internal/qdrant"
	"github.com/redis/go-redis/v9"
)

// FingerprintData represents the collected browser fingerprint.
type FingerprintData struct {
	Canvas    CanvasResult    `json:"canvas"`
	WebGL     WebGLResult     `json:"webgl"`
	Audio     AudioResult     `json:"audio"`
	Fonts     FontResult      `json:"fonts"`
	Screen    ScreenResult    `json:"screen"`
	Navigator NavigatorResult `json:"navigator"`
	Timezone  TimezoneResult  `json:"timezone"`
	WebRTC    WebRTCResult    `json:"webrtc"`
	Battery   BatteryResult   `json:"battery"`
}

// CanvasResult holds canvas fingerprint data.
type CanvasResult struct {
	Hash          string `json:"hash"`
	NoisePoisoned bool   `json:"noisePoisoned"`
	Length        int    `json:"length"`
}

// WebGLResult holds WebGL fingerprint data.
type WebGLResult struct {
	Supported              bool   `json:"supported"`
	Vendor                 string `json:"vendor"`
	Renderer               string `json:"renderer"`
	Version                string `json:"version"`
	ShadingLanguageVersion string `json:"shadingLanguageVersion"`
	MaxTextureSize         int    `json:"maxTextureSize"`
}

// AudioResult holds AudioContext fingerprint data.
type AudioResult struct {
	Supported    bool    `json:"supported"`
	Hash         string  `json:"hash"`
	SampleRate   float64 `json:"sampleRate"`
	ChannelCount int     `json:"channelCount"`
}

// FontResult holds font enumeration data.
type FontResult struct {
	Detected []string `json:"detected"`
	Count    int      `json:"count"`
}

// ScreenResult holds screen property data.
type ScreenResult struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	ColorDepth        int     `json:"colorDepth"`
	PixelDepth        int     `json:"pixelDepth"`
	PixelRatio        float64 `json:"pixelRatio"`
	WidthDiscrepancy  int     `json:"widthDiscrepancy"`
	HeightDiscrepancy int     `json:"heightDiscrepancy"`
}

// NavigatorResult holds navigator property data.
type NavigatorResult struct {
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	DeviceMemory        float64  `json:"deviceMemory"`
	Platform            string   `json:"platform"`
	MaxTouchPoints      int      `json:"maxTouchPoints"`
	Languages           []string `json:"languages"`
	Vendor              string   `json:"vendor"`
}

// TimezoneResult holds timezone data.
type TimezoneResult struct {
	Timezone      string `json:"timezone"`
	Locale        string `json:"locale"`
	OffsetMinutes int    `json:"offsetMinutes"`
}

// WebRTCResult holds WebRTC leak detection data.
type WebRTCResult struct {
	Supported bool     `json:"supported"`
	LocalIPs  []string `json:"localIPs"`
}

// BatteryResult holds battery API data.
type BatteryResult struct {
	Supported bool    `json:"supported"`
	Charging  bool    `json:"charging"`
	Level     float64 `json:"level"`
}

// Handler handles POST /fingerprint requests.
type Handler struct {
	redis       *redis.Client
	qdrantClient *qdrant.Client
}

// NewHandler creates a new fingerprint handler.
func NewHandler(redisClient *redis.Client, qdrantClient *qdrant.Client) *Handler {
	return &Handler{
		redis:        redisClient,
		qdrantClient: qdrantClient,
	}
}

// ServeHTTP handles incoming fingerprint submissions.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	clientIP := extractClientIP(r)

	// Rate limiting: 10 requests per minute per IP
	allowed, err := h.checkRateLimit(ctx, clientIP)
	if err != nil {
		log.Printf("ERROR: rate limit check: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
		return
	}

	// Read body (max 64KB)
	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var fp FingerprintData
	if err := json.Unmarshal(body, &fp); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Validate required fields
	if !validateFingerprint(&fp) {
		http.Error(w, `{"error":"invalid fingerprint data"}`, http.StatusUnprocessableEntity)
		return
	}

	// Compute fingerprint hash from stable signals
	fpHash := computeFingerprintHash(&fp)

	// Store in Redis with 30-day TTL
	fpJSON, _ := json.Marshal(fp)
	redisKey := fmt.Sprintf("fp:%s", fpHash)
	h.redis.Set(ctx, redisKey, fpJSON, 30*24*time.Hour)

	// Index by IP for correlation
	ipKey := fmt.Sprintf("fp:ip:%s", clientIP)
	h.redis.SAdd(ctx, ipKey, fpHash)
	h.redis.Expire(ctx, ipKey, 30*24*time.Hour)

	// Generate embedding and upsert to Qdrant (best effort)
	if h.qdrantClient != nil {
		features := toFingerprintFeatures(&fp)
		vector := qdrant.GenerateEmbedding(features)
		pointID := uuid.New().String()
		if err := h.qdrantClient.Upsert(ctx, pointID, vector, "", time.Now().UTC(), false); err != nil {
			log.Printf("WARN: qdrant upsert: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"hash":   fpHash,
	})
}

func (h *Handler) checkRateLimit(ctx context.Context, ip string) (bool, error) {
	key := fmt.Sprintf("ratelimit:fp:%s", ip)
	pipe := h.redis.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 60*time.Second)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}
	return incr.Val() <= 10, nil
}

func validateFingerprint(fp *FingerprintData) bool {
	// Must have at least canvas or WebGL data
	if fp.Canvas.Length == 0 && !fp.WebGL.Supported {
		return false
	}
	// Screen dimensions must be positive
	if fp.Screen.Width <= 0 || fp.Screen.Height <= 0 {
		return false
	}
	// Must have timezone
	if fp.Timezone.Timezone == "" {
		return false
	}
	return true
}

func computeFingerprintHash(fp *FingerprintData) string {
	var parts []string
	parts = append(parts, fp.Canvas.Hash)
	parts = append(parts, fp.WebGL.Vendor, fp.WebGL.Renderer)
	parts = append(parts, fp.Audio.Hash)

	sortedFonts := make([]string, len(fp.Fonts.Detected))
	copy(sortedFonts, fp.Fonts.Detected)
	sort.Strings(sortedFonts)
	parts = append(parts, strings.Join(sortedFonts, ","))

	parts = append(parts, fmt.Sprintf("%dx%d@%d",
		fp.Screen.Width, fp.Screen.Height, fp.Screen.ColorDepth))
	parts = append(parts, fmt.Sprintf("%d", fp.Navigator.HardwareConcurrency))
	parts = append(parts, fp.Navigator.Platform)
	parts = append(parts, fp.Timezone.Timezone)

	normalized := strings.Join(parts, "|")
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		return host[:idx]
	}
	return host
}

func toFingerprintFeatures(fp *FingerprintData) *qdrant.FingerprintFeatures {
	return &qdrant.FingerprintFeatures{
		CanvasHash:          fp.Canvas.Hash,
		WebGLRenderer:       fp.WebGL.Renderer,
		AudioFingerprint:    fp.Audio.SampleRate,
		ScreenWidth:         fp.Screen.Width,
		ScreenHeight:        fp.Screen.Height,
		ScreenDepth:         fp.Screen.ColorDepth,
		TimezoneOffset:      fp.Timezone.OffsetMinutes,
		Languages:           fp.Navigator.Languages,
		InstalledFontsCount: fp.Fonts.Count,
		Plugins:             nil,
		HardwareConcurrency: fp.Navigator.HardwareConcurrency,
		DeviceMemory:        fp.Navigator.DeviceMemory,
		Platform:            fp.Navigator.Platform,
	}
}
