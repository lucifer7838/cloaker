package postback

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/lucifer7838/ghostroute/internal/rabbitmq"
)

// PostbackRequest represents the incoming conversion notification.
type PostbackRequest struct {
	ClickID        string  `json:"click_id"`
	CampaignID     string  `json:"campaign_id"`
	ConversionType string  `json:"conversion_type"`
	Payout         float64 `json:"payout"`
}

// Handler handles POST /api/postback for conversion tracking.
type Handler struct {
	chConn   driver.Conn
	producer *rabbitmq.Producer
}

// NewHandler creates a new postback handler with a pre-initialized ClickHouse connection.
func NewHandler(chConn driver.Conn, producer *rabbitmq.Producer) *Handler {
	return &Handler{
		chConn:   chConn,
		producer: producer,
	}
}

// ServeHTTP handles incoming POST /api/postback requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	var req PostbackRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if req.ClickID == "" || req.CampaignID == "" {
		http.Error(w, `{"error":"click_id and campaign_id are required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Update ClickHouse visit record
	if err := h.markConversion(ctx, req.ClickID); err != nil {
		log.Printf("ERROR: mark conversion in clickhouse: %v", err)
	}

	// Publish conversion event to RabbitMQ
	if h.producer != nil {
		convMsg := &rabbitmq.ConversionEventMessage{
			ConversionID:   uuid.New().String(),
			ClickID:        req.ClickID,
			CampaignID:     req.CampaignID,
			EventTime:      time.Now().UTC().Format(time.RFC3339Nano),
			ConversionType: req.ConversionType,
			Payout:         req.Payout,
		}
		if err := h.producer.Publish(ctx, "ghostroute.clicks", "click.general", convMsg); err != nil {
			log.Printf("ERROR: publish conversion event: %v", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":        "ok",
		"conversion_id": uuid.New().String(),
	})
}

func (h *Handler) markConversion(ctx context.Context, clickID string) error {
	if h.chConn == nil {
		return fmt.Errorf("clickhouse connection not initialized")
	}

	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := h.chConn.Exec(queryCtx,
		"ALTER TABLE visits UPDATE is_conversion = 1 WHERE event_id = ?",
		clickID,
	)
	if err != nil {
		return fmt.Errorf("update visit: %w", err)
	}

	return nil
}
