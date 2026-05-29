package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lucifer7838/ghostroute/internal/campaign"
	"github.com/lucifer7838/ghostroute/internal/clicklog"
	"github.com/lucifer7838/ghostroute/internal/evaluate"
	"github.com/lucifer7838/ghostroute/internal/ipmatch"
)

func main() {
	port := getEnv("GHOSTROUTE_PORT", "8080")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	postgresDSN := getEnv("POSTGRES_DSN", "postgres://localhost:5432/ghostroute")
	clickhouseDSN := getEnv("CLICKHOUSE_DSN", "clickhouse://localhost:9000/ghostroute")
	botThreshold := getEnvFloat("BOT_THRESHOLD", 0.7)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Redis client for ASN lookups
	ipClient := ipmatch.NewClient(redisAddr, "", 0)

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

	// Register routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/evaluate", evalHandler)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	// Start server
	go func() {
		log.Printf("INFO: GhostRoute starting on :%s", port)
		log.Printf("INFO: Redis=%s, ClickHouse=%s", redisAddr, clickhouseDSN)
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

	if clickLogger != nil {
		clickLogger.Close()
	}
	if campaignLoader != nil {
		campaignLoader.Close()
	}
	if err := ipClient.Close(); err != nil {
		log.Printf("ERROR: redis close: %v", err)
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
