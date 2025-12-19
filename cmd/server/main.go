package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/FACorreiaa/smart-finance-tracker/cmd/api"
	"github.com/FACorreiaa/smart-finance-tracker/pkg/config"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("Error loading .env file")
		log.Fatal(err)
	}

	//apiKey := os.Getenv("GEMINI_API_KEY")
	//
	//// List available models if requested (or just for debug)
	//if os.Getenv("LIST_GEMINI_MODELS") == "true" {
	//	listGeminiModels(context.Background(), apiKey)
	//}

	// Initialize logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("starting echo API")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Initialize dependencies
	deps, err := api.InitDependencies(cfg, logger)
	if err != nil {
		logger.Error("failed to initialize dependencies", "error", err)
		os.Exit(1)
	}
	defer deps.Cleanup()

	// Start pprof server if enabled
	if cfg.Profiling.Enabled {
		go startPprofServer(cfg, logger)
	}

	// Setup router
	handler := api.SetupRouter(deps)

	// Start HTTP server
	if err := runServer(cfg, logger, handler); err != nil {
		logger.Error("server error", "error", err)
		os.Exit(1)
	}
}

// startPprofServer starts the pprof profiling server on a separate port
func startPprofServer(cfg *config.Config, logger *slog.Logger) {
	mux := http.NewServeMux()

	// Register pprof service
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	addr := fmt.Sprintf("localhost:%d", cfg.Profiling.Port)
	logger.Info("pprof server started", "addr", addr, "endpoints", "/debug/pprof/")

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("pprof server error", "error", err)
	}
}

// runServer starts the HTTP server with graceful shutdown
func runServer(cfg *config.Config, logger *slog.Logger, handler http.Handler) error {
	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)

	// Enable HTTP/2 support (h2c - HTTP/2 without TLS)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
		// ReadTimeout: Time to read the entire request including body
		ReadTimeout: 30 * time.Second,
		// WriteTimeout: For streaming endpoints (SSE/gRPC streams), this must be long enough
		// to handle the entire response duration. LLM responses can take 30+ seconds.
		// Setting to 0 disables the timeout - we rely on application-level timeouts
		// (context.WithTimeout in chat_process_stream.go) for proper deadline management.
		WriteTimeout: 0,
		// IdleTimeout: Time to wait for the next request when keep-alives are enabled
		IdleTimeout: 120 * time.Second,
		Protocols:   protocols,
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server started", "addr", addr)
		serverErrors <- srv.ListenAndServe()
	}()

	// Wait for interrupt signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig)

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			srv.Close()
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}

		logger.Info("server stopped gracefully")
	}

	return nil
}
