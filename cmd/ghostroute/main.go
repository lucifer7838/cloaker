package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/lucifer7838/ghostroute/internal/campaign"
	"github.com/lucifer7838/ghostroute/internal/clicklog"
	"github.com/lucifer7838/ghostroute/internal/evaluate"
	"github.com/lucifer7838/ghostroute/internal/fingerprint"
	"github.com/lucifer7838/ghostroute/internal/ipmatch"
	"github.com/lucifer7838/ghostroute/internal/postback"
	"github.com/lucifer7838/ghostroute/internal/rabbitmq"
	"github.com/redis/go-redis/v9"
)

func main() {
	port := getEnv("GHOSTROUTE_PORT", "8080")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	postgresDSN := getEnv("POSTGRES_DSN", "postgres://localhost:5432/ghostroute")
	clickhouseDSN := getEnv("CLICKHOUSE_DSN", "clickhouse://localhost:9000/ghostroute")
	rabbitmqURL := getEnv("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	_ = getEnv("ML_SERVICE_URL", "http://localhost:8000")
	botThreshold := getEnvFloat("BOT_THRESHOLD", 0.7)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis client for ASN lookups
	ipClient := ipmatch.NewClient(redisAddr, redisPassword, 0)

	// Initialize Redis client for ML score cache
	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	})

	// Initialize PostgreSQL campaign loader
	campaignLoader, err := campaign.NewLoader(ctx, postgresDSN)
	if err != nil {
		log.Printf("WARN: campaign loader init failed (will retry on demand): %v", err)
		campaignLoader = nil
	} else {
		campaignLoader.StartRefresh(ctx, 60*time.Second)
	}

	// Initialize ClickHouse logger
	clickLogger, err := clicklog.NewLogger(clickhouseDSN)
	if err != nil {
		log.Printf("WARN: clickhouse logger init failed (visits will be dropped): %v", err)
		clickLogger = nil
	} else {
		clickLogger.Start(ctx)
	}

	// Initialize RabbitMQ producer
	var producer *rabbitmq.Producer
	producer, err = rabbitmq.NewProducer(rabbitmq.ProducerConfig{
		URL:      rabbitmqURL,
		PoolSize: 5,
	})
	if err != nil {
		log.Printf("WARN: rabbitmq producer init failed (events will not be published): %v", err)
		producer = nil
	} else {
		// Declare topology
		conn, dialErr := amqp.Dial(rabbitmqURL)
		if dialErr == nil {
			ch, chErr := conn.Channel()
			if chErr == nil {
				if topErr := rabbitmq.DeclareTopology(ch); topErr != nil {
					log.Printf("WARN: declare rabbitmq topology: %v", topErr)
				}
				ch.Close()
			}
			conn.Close()
		}
	}

	// Build the evaluate handler
	var loader *campaign.Loader
	if campaignLoader != nil {
		loader = campaignLoader
	}
	var logger *clicklog.Logger
	if clickLogger != nil {
		logger = clickLogger
	}

	evalHandler := evaluate.NewHandler(ipClient, loader, logger, botThreshold)
	if producer != nil {
		evalHandler.SetProducer(producer)
	}
	evalHandler.SetRedisClient(redisClient)

	// Initialize fingerprint handler
	fpHandler := fingerprint.NewHandler(redisClient, nil)

	// Initialize postback handler
	var postbackHandler *postback.Handler
	if producer != nil {
		postbackHandler = postback.NewHandler(clickhouseDSN, producer)
	} else {
		postbackHandler = postback.NewHandler(clickhouseDSN, nil)
	}

	// Initialize ClickHouse query proxy for admin dashboard
	queryHandler := newQueryHandler(clickhouseDSN)

	// Register routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/evaluate", evalHandler)
	mux.Handle("/fingerprint", fpHandler)
	mux.Handle("/api/postback", postbackHandler)
	mux.HandleFunc("/api/query", queryHandler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Start server
	go func() {
		log.Printf("INFO: GhostRoute starting on :%s", port)
		log.Printf("INFO: Redis=%s, ClickHouse=%s, RabbitMQ=%s", redisAddr, clickhouseDSN, rabbitmqURL)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: server error: %v", err)
		}
	}()

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	log.Println("INFO: shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("ERROR: server shutdown: %v", err)
	}

	cancel()

	if producer != nil {
		if err := producer.Close(); err != nil {
			log.Printf("ERROR: rabbitmq producer close: %v", err)
		}
	}
	if clickLogger != nil {
		clickLogger.Close()
	}
	if campaignLoader != nil {
		campaignLoader.Close()
	}
	if err := ipClient.Close(); err != nil {
		log.Printf("ERROR: redis close: %v", err)
	}
	if err := redisClient.Close(); err != nil {
		log.Printf("ERROR: redis ml client close: %v", err)
	}

	log.Println("INFO: shutdown complete")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return f
		}
	}
	return defaultVal
}

// queryRequest represents a SQL query request from the admin dashboard.
type queryRequest struct {
	Query string `json:"query"`
}

// newQueryHandler creates an HTTP handler that proxies read-only SQL queries to ClickHouse.
func newQueryHandler(dsn string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
		if err != nil {
			http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req queryRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}

		sql := strings.TrimSpace(req.Query)
		if sql == "" {
			http.Error(w, `{"error":"empty query"}`, http.StatusBadRequest)
			return
		}

		// Only allow SELECT statements
		upper := strings.ToUpper(sql)
		if !strings.HasPrefix(upper, "SELECT") {
			http.Error(w, `{"error":"only SELECT queries are allowed"}`, http.StatusForbidden)
			return
		}

		// Reject dangerous keywords that could modify data even in subqueries
		for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE", "RENAME", "ATTACH", "DETACH"} {
			if strings.Contains(upper, kw) {
				http.Error(w, fmt.Sprintf(`{"error":"query contains forbidden keyword: %s"}`, kw), http.StatusForbidden)
				return
			}
		}

		opts, err := clickhouse.ParseDSN(dsn)
		if err != nil {
			log.Printf("ERROR: query handler parse dsn: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}

		conn, err := clickhouse.Open(opts)
		if err != nil {
			log.Printf("ERROR: query handler open: %v", err)
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		queryCtx, queryCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer queryCancel()

		rows, err := conn.Query(queryCtx, sql)
		if err != nil {
			log.Printf("ERROR: query handler query: %v", err)
			http.Error(w, fmt.Sprintf(`{"error":"query failed: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}
		defer rows.Close()

		columns := rows.Columns()
		columnTypes := rows.ColumnTypes()

		var results []map[string]interface{}
		for rows.Next() {
			values := make([]interface{}, len(columns))
			for i, ct := range columnTypes {
				values[i] = makeZeroValue(ct.DatabaseTypeName())
			}
			if err := rows.Scan(values...); err != nil {
				log.Printf("ERROR: query handler scan: %v", err)
				http.Error(w, `{"error":"failed to scan results"}`, http.StatusInternalServerError)
				return
			}
			row := make(map[string]interface{}, len(columns))
			for i, col := range columns {
				row[col] = values[i]
			}
			results = append(results, row)
		}

		if err := rows.Err(); err != nil {
			log.Printf("ERROR: query handler rows err: %v", err)
			http.Error(w, `{"error":"query execution error"}`, http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if results == nil {
			results = []map[string]interface{}{}
		}
		json.NewEncoder(w).Encode(results)
	}
}

// makeZeroValue returns a pointer to a zero value for scanning ClickHouse column types.
func makeZeroValue(dbType string) interface{} {
	upper := strings.ToUpper(dbType)
	switch {
	case strings.Contains(upper, "INT8"), strings.Contains(upper, "INT16"),
		strings.Contains(upper, "INT32"), strings.Contains(upper, "INT64"),
		strings.Contains(upper, "UINT8"), strings.Contains(upper, "UINT16"),
		strings.Contains(upper, "UINT32"), strings.Contains(upper, "UINT64"):
		var v uint64
		return &v
	case strings.Contains(upper, "FLOAT32"), strings.Contains(upper, "FLOAT64"):
		var v float64
		return &v
	default:
		var v interface{}
		return &v
	}
}
