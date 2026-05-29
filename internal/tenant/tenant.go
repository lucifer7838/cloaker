package tenant

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lucifer7838/ghostroute/internal/campaign"
)

// TenantService provides per-user campaign isolation.
type TenantService struct {
	pool *pgxpool.Pool
}

// NewTenantService creates a new tenant service with the given database pool.
func NewTenantService(pool *pgxpool.Pool) *TenantService {
	return &TenantService{pool: pool}
}

// GetCampaignsForTenant returns only campaigns owned by the specified tenant.
func (ts *TenantService) GetCampaignsForTenant(ctx context.Context, tenantID string) ([]*campaign.Campaign, error) {
	rows, err := ts.pool.Query(ctx, `
		SELECT id, name, route, active, bot_threshold, white_page_url, black_page_url, geo_countries, allowed_devices
		FROM campaigns
		WHERE user_id = $1 AND active = true
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant campaigns: %w", err)
	}
	defer rows.Close()

	var campaigns []*campaign.Campaign
	for rows.Next() {
		c := &campaign.Campaign{}
		err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.Route,
			&c.Active,
			&c.BotThreshold,
			&c.WhitePageURL,
			&c.BlackPageURL,
			&c.GeoCountries,
			&c.AllowedDevices,
		)
		if err != nil {
			continue
		}
		campaigns = append(campaigns, c)
	}

	return campaigns, nil
}

// ValidateCampaignAccess checks if a campaign is owned by the specified tenant.
func (ts *TenantService) ValidateCampaignAccess(ctx context.Context, tenantID, campaignID string) error {
	var exists bool
	err := ts.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM campaigns WHERE id = $1 AND user_id = $2)
	`, campaignID, tenantID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("validate campaign access: %w", err)
	}
	if !exists {
		return fmt.Errorf("campaign %s not accessible by tenant %s", campaignID, tenantID)
	}
	return nil
}
