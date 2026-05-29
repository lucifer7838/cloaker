package clicklog

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Visit represents a single visit event to be logged to ClickHouse.
type Visit struct {
	EventID    string
	CampaignID uint32
	EventTime  time.Time
	VisitorIP  string
	UserAgent  string
	Country    string
	DeviceType string
	OS         string
	Browser    string
	Referer    string
	LandingURL string
	IsBot      uint8
	BotScore   float32
	Decision   string
	Reason     string
	ASNNumber  uint32
	ASNName    string
	JA3Hash    string
}

// Logger handles asynchronous batch logging of visits to ClickHouse.
type Logger struct {
	conn driver.Conn
	ch   chan Visit
	done chan struct{}
}

// NewLogger creates a new ClickHouse logger.
func NewLogger(dsn string) (*Logger, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse clickhouse dsn: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	return &Logger{
		conn: conn,
		ch:   make(chan Visit, 1000),
		done: make(chan struct{}),
	}, nil
}

// Log sends a visit to the async logging channel. Non-blocking; drops if full.
func (l *Logger) Log(visit Visit) {
	select {
	case l.ch <- visit:
	default:
		log.Println("WARN: clicklog buffer full, dropping visit")
	}
}

// Start begins the background goroutine that collects and flushes visits.
func (l *Logger) Start(ctx context.Context) {
	go func() {
		defer close(l.done)
		buf := make([]Visit, 0, 100)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case v, ok := <-l.ch:
				if !ok {
					// Channel closed, flush remaining
					if len(buf) > 0 {
						l.flush(ctx, buf)
					}
					return
				}
				buf = append(buf, v)
				if len(buf) >= 100 {
					l.flush(ctx, buf)
					buf = buf[:0]
				}
			case <-ticker.C:
				if len(buf) > 0 {
					l.flush(ctx, buf)
					buf = buf[:0]
				}
			case <-ctx.Done():
				// Drain remaining from channel
				for {
					select {
					case v := <-l.ch:
						buf = append(buf, v)
					default:
						if len(buf) > 0 {
							l.flush(context.Background(), buf)
						}
						return
					}
				}
			}
		}
	}()
}

func (l *Logger) flush(ctx context.Context, visits []Visit) {
	batch, err := l.conn.PrepareBatch(ctx, `
		INSERT INTO ghostroute.visits (
			event_id, campaign_id, event_time, visitor_ip, user_agent,
			country, device_type, os, browser, referer,
			landing_url, is_bot, bot_score, decision, reason,
			asn_number, asn_name, ja3_hash
		)
	`)
	if err != nil {
		log.Printf("ERROR: clicklog prepare batch: %v", err)
		return
	}

	for _, v := range visits {
		err := batch.Append(
			v.EventID,
			v.CampaignID,
			v.EventTime,
			v.VisitorIP,
			v.UserAgent,
			v.Country,
			v.DeviceType,
			v.OS,
			v.Browser,
			v.Referer,
			v.LandingURL,
			v.IsBot,
			v.BotScore,
			v.Decision,
			v.Reason,
			v.ASNNumber,
			v.ASNName,
			v.JA3Hash,
		)
		if err != nil {
			log.Printf("ERROR: clicklog append: %v", err)
			return
		}
	}

	if err := batch.Send(); err != nil {
		log.Printf("ERROR: clicklog send batch: %v", err)
	}
}

// Close flushes remaining visits and closes the connection.
func (l *Logger) Close() {
	close(l.ch)
	<-l.done
	l.conn.Close()
}
