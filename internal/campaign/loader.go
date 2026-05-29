package campaign

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Campaign represents a routing campaign configuration.
type Campaign struct {
	ID             string
	Name           string
	Route          string
	Active         bool
	BotThreshold   float64
	WhitePageURL   string
	BlackPageURL   string
	GeoCountries   []string
	AllowedDevices []string
}

// Loader manages campaign data from PostgreSQL with in-memory caching.
type Loader struct {
	pool  *pgxpool.Pool
	cache sync.Map
	stop  chan struct{}
}

// NewLoader creates a new campaign loader with a pgxpool connection.
func NewLoader(ctx context.Context, dsn string) (*Loader, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Loader{
		pool: pool,
		stop: make(chan struct{}),
	}, nil
}

// GetCampaign returns a campaign by ID, checking the cache first.
func (l *Loader) GetCampaign(ctx context.Context, campaignID string) (*Campaign, error) {
	if cached, ok := l.cache.Load(campaignID); ok {
		return cached.(*Campaign), nil
	}

	row := l.pool.QueryRow(ctx, `
		SELECT id, name, route, active, bot_threshold, white_page_url, black_page_url, geo_countries, allowed_devices
		FROM campaigns
		WHERE id = $1
	`, campaignID)

	c := &Campaign{}
	err := row.Scan(
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
		return nil, err
	}

	l.cache.Store(campaignID, c)
	return c, nil
}

// StartRefresh starts a background goroutine that refreshes all active campaigns at the given interval.
func (l *Loader) StartRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				l.refreshAll(ctx)
			case <-l.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (l *Loader) refreshAll(ctx context.Context) {
	rows, err := l.pool.Query(ctx, `
		SELECT id, name, route, active, bot_threshold, white_page_url, black_page_url, geo_countries, allowed_devices
		FROM campaigns
		WHERE active = true
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		c := &Campaign{}
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
		l.cache.Store(c.ID, c)
	}
}

// Close shuts down the loader and its connection pool.
func (l *Loader) Close() {
	close(l.stop)
	l.pool.Close()
}
