package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a REST-based Qdrant client for fingerprint vector operations.
type Client struct {
	baseURL    string
	httpClient *http.Client
	collection string
}

// NewClient creates a new Qdrant REST client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		collection: "fingerprints",
	}
}

// CreateCollection creates the fingerprints collection with 128-dim cosine distance.
func (c *Client) CreateCollection(ctx context.Context) error {
	body := map[string]interface{}{
		"vectors": map[string]interface{}{
			"size":     128,
			"distance": "Cosine",
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal create collection: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create collection failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Point represents a vector point with payload for upsert.
type Point struct {
	ID      string                 `json:"id"`
	Vector  []float32              `json:"vector"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// Upsert inserts or updates a single point in the collection.
func (c *Client) Upsert(ctx context.Context, id string, vector []float32, campaignID string, createdAt time.Time, isBot bool) error {
	body := map[string]interface{}{
		"points": []map[string]interface{}{
			{
				"id":     id,
				"vector": vector,
				"payload": map[string]interface{}{
					"campaign_id": campaignID,
					"created_at":  createdAt.Format(time.RFC3339),
					"is_bot":      isBot,
				},
			},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal upsert: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upsert failed (%d): %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SearchResult holds a single search match from Qdrant.
type SearchResult struct {
	ID    string  `json:"id"`
	Score float32 `json:"score"`
}

// searchResponse represents the Qdrant search API response.
type searchResponse struct {
	Result []struct {
		ID      interface{}            `json:"id"`
		Score   float32                `json:"score"`
		Payload map[string]interface{} `json:"payload,omitempty"`
	} `json:"result"`
}

// Search finds the nearest neighbors to a query vector, optionally filtered by campaign_id.
func (c *Client) Search(ctx context.Context, queryVector []float32, campaignID *string, scoreThreshold float32, limit int) ([]SearchResult, error) {
	body := map[string]interface{}{
		"vector":          queryVector,
		"limit":           limit,
		"score_threshold": scoreThreshold,
		"with_payload":    true,
	}

	if campaignID != nil && *campaignID != "" {
		body["filter"] = map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key": "campaign_id",
					"match": map[string]interface{}{
						"value": *campaignID,
					},
				},
			},
		}
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal search: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points/search", c.baseURL, c.collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var searchResp searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}

	results := make([]SearchResult, 0, len(searchResp.Result))
	for _, r := range searchResp.Result {
		idStr := ""
		switch v := r.ID.(type) {
		case string:
			idStr = v
		case float64:
			idStr = fmt.Sprintf("%d", int64(v))
		}
		results = append(results, SearchResult{
			ID:    idStr,
			Score: r.Score,
		})
	}

	return results, nil
}
