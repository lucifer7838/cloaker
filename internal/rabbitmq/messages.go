package rabbitmq

import "time"

// ClickEventMessage represents a click event published to RabbitMQ.
type ClickEventMessage struct {
	EventID         string  `json:"event_id"`
	CampaignID      string  `json:"campaign_id"`
	EventTime       string  `json:"event_time"`
	VisitorIP       string  `json:"visitor_ip"`
	UserAgent       string  `json:"user_agent"`
	FingerprintHash string  `json:"fingerprint_hash"`
	Country         string  `json:"country"`
	Referer         string  `json:"referer"`
	LandingURL      string  `json:"landing_url"`
	BotScore        float32 `json:"bot_score"`
	JA3Hash         string  `json:"ja3_hash"`
}

// ConversionEventMessage represents a conversion event published to RabbitMQ.
type ConversionEventMessage struct {
	ConversionID   string  `json:"conversion_id"`
	ClickID        string  `json:"click_id"`
	CampaignID     string  `json:"campaign_id"`
	EventTime      string  `json:"event_time"`
	ConversionType string  `json:"conversion_type"`
	Payout         float64 `json:"payout"`
}

// FingerprintEventMessage represents a fingerprint event published to RabbitMQ.
type FingerprintEventMessage struct {
	FingerprintHash     string   `json:"fingerprint_hash"`
	CampaignID          string   `json:"campaign_id"`
	EventTime           string   `json:"event_time"`
	CanvasHash          string   `json:"canvas_hash"`
	WebGLRenderer       string   `json:"webgl_renderer"`
	ScreenWidth         int      `json:"screen_width"`
	ScreenHeight        int      `json:"screen_height"`
	TimezoneOffset      int      `json:"timezone_offset"`
	Languages           []string `json:"languages"`
	Plugins             []string `json:"plugins"`
	HardwareConcurrency int      `json:"hardware_concurrency"`
	DeviceMemory        float64  `json:"device_memory"`
	Platform            string   `json:"platform"`
}

// NewClickEventMessage creates a new click event message with the current time.
func NewClickEventMessage(eventID, campaignID, visitorIP, userAgent, fingerprintHash, country, referer, landingURL, ja3Hash string, botScore float32) *ClickEventMessage {
	return &ClickEventMessage{
		EventID:         eventID,
		CampaignID:      campaignID,
		EventTime:       time.Now().UTC().Format(time.RFC3339Nano),
		VisitorIP:       visitorIP,
		UserAgent:       userAgent,
		FingerprintHash: fingerprintHash,
		Country:         country,
		Referer:         referer,
		LandingURL:      landingURL,
		BotScore:        botScore,
		JA3Hash:         ja3Hash,
	}
}
