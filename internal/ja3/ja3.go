package ja3

import (
	"context"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"
)

// TLSFingerprint represents extracted TLS fingerprint data from Nginx headers.
type TLSFingerprint struct {
	JA3Hash    string
	JA3String  string
	JA4Hash    string
	TLSVersion string
	TLSCipher  string
}

// Client provides JA3/JA4 fingerprint analysis with Redis-backed lookups.
type Client struct {
	redis *redis.Client
}

// NewClient creates a new JA3 analysis client.
func NewClient(redisClient *redis.Client) *Client {
	return &Client{redis: redisClient}
}

// ExtractFromHeaders reads TLS fingerprint data injected by Nginx.
func ExtractFromHeaders(r *http.Request) TLSFingerprint {
	return TLSFingerprint{
		JA3Hash:    r.Header.Get("X-JA3-Hash"),
		JA3String:  r.Header.Get("X-JA3-String"),
		JA4Hash:    r.Header.Get("X-JA4-Hash"),
		TLSVersion: r.Header.Get("X-TLS-Version"),
		TLSCipher:  r.Header.Get("X-TLS-Cipher"),
	}
}

// IsKnownBot checks if the JA3 hash matches a known bot fingerprint stored in Redis.
// Falls back to a static list if Redis is unavailable.
func (c *Client) IsKnownBot(ctx context.Context, fp TLSFingerprint) bool {
	if fp.JA3Hash == "" {
		return false
	}

	// Check Redis set first
	if c.redis != nil {
		isMember, err := c.redis.SIsMember(ctx, "ja3:known_bots", fp.JA3Hash).Result()
		if err == nil && isMember {
			return true
		}
	}

	// Fallback to static known bot hashes
	return isStaticKnownBot(fp.JA3Hash)
}

// CheckMismatch detects when JA3 indicates a headless browser but the User-Agent
// claims to be a legitimate browser. Returns true if a mismatch is detected.
func (c *Client) CheckMismatch(fp TLSFingerprint, userAgent string) bool {
	if fp.JA3Hash == "" {
		return false
	}

	// Known headless/automation JA3 hashes
	headlessHashes := map[string]bool{
		"a0e9f5d64349fb13191bc781f81f42e1": true, // HeadlessChrome
		"cd08e31494f9531f560d64c695473da9": true, // PhantomJS
	}

	if !headlessHashes[fp.JA3Hash] {
		return false
	}

	// If JA3 is headless but UA claims to be a regular browser, it is a mismatch
	ua := strings.ToLower(userAgent)
	legitimateBrowserClaims := []string{"firefox", "safari", "chrome", "edge", "opera"}
	for _, browser := range legitimateBrowserClaims {
		if strings.Contains(ua, browser) && !strings.Contains(ua, "headless") {
			return true
		}
	}

	return false
}

// isStaticKnownBot checks against a hardcoded list of known bot JA3 hashes.
func isStaticKnownBot(hash string) bool {
	knownBots := map[string]bool{
		"e4f26f64c47e1fc3f9f5e8c5e38a4f0c": true, // curl/7.x
		"b32309a26951912be7dba376398abc3b": true, // Python-urllib
		"3b5074b1b5d032e5620f69f9f700ff0e": true, // Python-requests
		"a0e9f5d64349fb13191bc781f81f42e1": true, // HeadlessChrome
		"cd08e31494f9531f560d64c695473da9": true, // PhantomJS
		"19e29534fd49dd27d09234e639c4057e": true, // wget
		"5d65ea3fb1d4aa7d826733f2c5e97b05": true, // Go-http-client
		"1d095e36b45b5a9f56e6caf6b08e4002": true, // Googlebot
		"2b3e96781f6c43e8e543652b3adb9240": true, // Bingbot
	}
	return knownBots[hash]
}
