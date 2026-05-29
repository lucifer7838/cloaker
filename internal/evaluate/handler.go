package evaluate

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lucifer7838/ghostroute/internal/botdetect"
	"github.com/lucifer7838/ghostroute/internal/campaign"
	"github.com/lucifer7838/ghostroute/internal/clicklog"
	"github.com/lucifer7838/ghostroute/internal/ipmatch"
)

// EvaluateRequest is the JSON request body for POST /evaluate.
type EvaluateRequest struct {
	IP         string            `json:"ip"`
	UserAgent  string            `json:"user_agent"`
	Headers    map[string]string `json:"headers"`
	JA3        string            `json:"ja3"`
	CampaignID string            `json:"campaign_id"`
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

	// Load campaign config if provided
	var camp *campaign.Campaign
	if req.CampaignID != "" && h.campaigns != nil {
		camp, _ = h.campaigns.GetCampaign(ctx, req.CampaignID)
	}

	// Determine decision
	resp := EvaluateResponse{
		ASN: asnInfo,
	}

	switch {
	case isBot:
		resp.Decision = "block"
		resp.Score = 1.0
		resp.Reason = "bot_ua_match"
	case asnResult.Found && isDatacenterASN(asnResult.ASName):
		resp.Decision = "block"
		resp.Score = 0.8
		resp.Reason = "datacenter_asn"
	default:
		resp.Decision = "allow"
		resp.Score = 0.0
		resp.Reason = "clean"
	}

	// Fire-and-forget log to ClickHouse
	var botFlag uint8
	if isBot {
		botFlag = 1
	}
	campaignID := req.CampaignID
	if camp != nil {
		campaignID = camp.ID
	}

	if h.clickLogger != nil {
		h.clickLogger.Log(clicklog.Visit{
			EventID:    uuid.New().String(),
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
			JA3Hash:    req.JA3,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("ERROR: encode response: %v", err)
	}
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
