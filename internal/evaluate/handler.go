package evaluate

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lucifer7838/ghostroute/internal/botdetect"
	"github.com/lucifer7838/ghostroute/internal/campaign"
	"github.com/lucifer7838/ghostroute/internal/clicklog"
	"github.com/lucifer7838/ghostroute/internal/ipmatch"
	"github.com/lucifer7838/ghostroute/internal/ja3"
	"github.com/lucifer7838/ghostroute/internal/qdrant"
	"github.com/lucifer7838/ghostroute/internal/rabbitmq"
	"github.com/redis/go-redis/v9"
)

// EvaluateRequest is the JSON request body for POST /evaluate.
type EvaluateRequest struct {
	IP              string            `json:"ip"`
	UserAgent       string            `json:"user_agent"`
	Headers         map[string]string `json:"headers"`
	JA3             string            `json:"ja3"`
	CampaignID      string            `json:"campaign_id"`
	FingerprintHash string            `json:"fingerprint_hash,omitempty"`
}

// EvaluateResponse is the JSON response for POST /evaluate.
type EvaluateResponse struct {
	Decision string   `json:"decision"`
	Score    float64  `json:"score"`
	Reason   string   `json:"reason"`
	ASN      *ASNInfo `json:"asn,omitempty"`
}

// ASNInfo holds ASN details included in the response.
type ASNInfo struct {
	Number uint32 `json:"number"`
	Name   string `json:"name"`
}

// Handler implements the /evaluate endpoint logic.
type Handler struct {
	ipClient     *ipmatch.Client
	campaigns    *campaign.Loader
	clickLogger  *clicklog.Logger
	ja3Client    *ja3.Client
	qdrantClient *qdrant.Client
	producer     *rabbitmq.Producer
	redisClient  *redis.Client
	botThreshold float64
}

// NewHandler creates a new evaluate handler.
func NewHandler(ipClient *ipmatch.Client, campaigns *campaign.Loader, clickLogger *clicklog.Logger, botThreshold float64) *Handler {
	return &Handler{
		ipClient:     ipClient,
		campaigns:    campaigns,
		clickLogger:  clickLogger,
		botThreshold: botThreshold,
	}
}

// SetJA3Client sets the JA3 client for TLS fingerprint analysis.
func (h *Handler) SetJA3Client(c *ja3.Client) {
	h.ja3Client = c
}

// SetQdrantClient sets the Qdrant client for vector similarity checks.
func (h *Handler) SetQdrantClient(c *qdrant.Client) {
	h.qdrantClient = c
}

// SetProducer sets the RabbitMQ producer for event publishing.
func (h *Handler) SetProducer(p *rabbitmq.Producer) {
	h.producer = p
}

// SetRedisClient sets the Redis client for cached ML score lookups.
func (h *Handler) SetRedisClient(rc *redis.Client) {
	h.redisClient = rc
}

// ServeHTTP handles POST /evaluate requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req EvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Lookup ASN
	var asnInfo *ASNInfo
	asnResult, err := h.ipClient.LookupIPv4(ctx, req.IP)
	if err == nil && asnResult.Found {
		asnInfo = &ASNInfo{
			Number: asnResult.ASNumber,
			Name:   asnResult.ASName,
		}
	}

	// Check bot via UA
	isBot := botdetect.MatchesBot(req.UserAgent)

	// JA3 fingerprint analysis
	var ja3Mismatch bool
	var ja3IsBot bool
	tlsFP := ja3.ExtractFromHeaders(r)
	if h.ja3Client != nil {
		ja3IsBot = h.ja3Client.IsKnownBot(ctx, tlsFP)
		ja3Mismatch = h.ja3Client.CheckMismatch(tlsFP, req.UserAgent)
	}

	// Qdrant vector similarity check
	var qdrantBotScore float32
	if h.qdrantClient != nil && req.FingerprintHash != "" {
		results, err := h.qdrantClient.Search(ctx, nil, nil, 0.90, 5)
		if err == nil {
			for _, result := range results {
				if result.Score > qdrantBotScore {
					qdrantBotScore = result.Score
				}
			}
		}
	}

	// Check Redis for cached ML score
	var mlScore float64
	var hasMLScore bool
	if h.redisClient != nil && req.IP != "" && req.CampaignID != "" {
		mlScore, hasMLScore = h.getCachedMLScore(ctx, req.IP, req.CampaignID)
	}

	// Load campaign config if provided
	var camp *campaign.Campaign
	if req.CampaignID != "" && h.campaigns != nil {
		camp, _ = h.campaigns.GetCampaign(ctx, req.CampaignID)
	}

	// Determine decision
	resp := EvaluateResponse{
		ASN: asnInfo,
	}

	// Dual scoring: 0.6 * vector_similarity + 0.4 * xgboost_score
	if hasMLScore && qdrantBotScore > 0 {
		combinedScore := 0.6*float64(qdrantBotScore) + 0.4*mlScore
		if combinedScore > h.botThreshold {
			resp.Decision = "block"
			resp.Score = combinedScore
			resp.Reason = "dual_score_threshold"
		}
	}

	// If dual scoring did not produce a block, fall through to rule-based
	if resp.Decision == "" {
		switch {
		case isBot:
			resp.Decision = "block"
			resp.Score = 1.0
			resp.Reason = "bot_ua_match"
		case ja3IsBot:
			resp.Decision = "block"
			resp.Score = 0.95
			resp.Reason = "ja3_known_bot"
		case ja3Mismatch:
			resp.Decision = "block"
			resp.Score = 0.9
			resp.Reason = "ja3_mismatch"
		case qdrantBotScore > 0.90:
			resp.Decision = "block"
			resp.Score = float64(qdrantBotScore)
			resp.Reason = "fingerprint_similarity"
		case hasMLScore && mlScore > h.botThreshold:
			resp.Decision = "block"
			resp.Score = mlScore
			resp.Reason = "ml_score_threshold"
		case asnResult.Found && isDatacenterASN(asnResult.ASName):
			resp.Decision = "block"
			resp.Score = 0.8
			resp.Reason = "datacenter_asn"
		default:
			resp.Decision = "allow"
			resp.Score = 0.0
			resp.Reason = "clean"
		}
	}

	// Fire-and-forget log to ClickHouse
	var botFlag uint8
	if resp.Decision == "block" {
		botFlag = 1
	}
	campaignID := req.CampaignID
	if camp != nil {
		campaignID = camp.ID
	}

	ja3Hash := req.JA3
	if ja3Hash == "" {
		ja3Hash = tlsFP.JA3Hash
	}

	eventID := uuid.New().String()

	if h.clickLogger != nil {
		h.clickLogger.Log(clicklog.Visit{
			EventID:    eventID,
			CampaignID: campaignID,
			EventTime:  time.Now().UTC(),
			VisitorIP:  req.IP,
			UserAgent:  req.UserAgent,
			Country:    "",
			DeviceType: "",
			OS:         "",
			Browser:    "",
			Referer:    "",
			LandingURL: "",
			IsBot:      botFlag,
			BotScore:   float32(resp.Score),
			Decision:   resp.Decision,
			Reason:     resp.Reason,
			ASNNumber:  asnResult.ASNumber,
			ASNName:    asnResult.ASName,
			JA3Hash:    ja3Hash,
		})
	}

	// Publish click event to RabbitMQ for async ML processing
	if h.producer != nil {
		clickMsg := rabbitmq.NewClickEventMessage(
			eventID,
			campaignID,
			req.IP,
			req.UserAgent,
			req.FingerprintHash,
			"",
			"",
			"",
			ja3Hash,
			float32(resp.Score),
		)
		go func() {
			pubCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.producer.Publish(pubCtx, "ghostroute.clicks", "click.general", clickMsg); err != nil {
				log.Printf("ERROR: publish click event to rabbitmq: %v", err)
			}
		}()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ERROR: encode response: %v", err)
	}
}

// getCachedMLScore retrieves a cached ML score from Redis.
func (h *Handler) getCachedMLScore(ctx context.Context, ip, campaignID string) (float64, bool) {
	key := fmt.Sprintf("ml:score:%s:%s", ip, campaignID)
	val, err := h.redisClient.Get(ctx, key).Result()
	if err != nil {
		return 0, false
	}
	score, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return 0, false
	}
	return score, true
}

// isDatacenterASN checks if the ASN name suggests a datacenter/hosting provider.
func isDatacenterASN(name string) bool {
	datacenterKeywords := []string{
		"AMAZON", "AWS", "GOOGLE-CLOUD", "MICROSOFT-AZURE",
		"DIGITALOCEAN", "LINODE", "VULTR", "OVH", "HETZNER",
		"CONTABO", "HOSTINGER", "SCALEWAY", "ORACLE-CLOUD",
	}
	upper := strings.ToUpper(name)
	for _, kw := range datacenterKeywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}
