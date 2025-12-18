package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"

	"connectrpc.com/connect"
	c "connectrpc.com/cors"
	"github.com/FACorreiaa/loci-connect-proto/gen/go/loci/auth/authconnect"
	"github.com/FACorreiaa/loci-connect-proto/gen/go/loci/user/userconnect"

	"connectrpc.com/validate"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/cors"
	"go.opentelemetry.io/otel"
	"golang.org/x/time/rate"

	"github.com/FACorreiaa/smart-finance-tracker/pkg/interceptors"
	"github.com/FACorreiaa/smart-finance-tracker/pkg/observability"
)

// SetupRouter configures all routes and returns the HTTP service
func SetupRouter(deps *Dependencies) http.Handler {
	mux := http.NewServeMux()

	jwtSecret := []byte(deps.Config.Auth.JWTSecret)
	if len(jwtSecret) == 0 {
		deps.Logger.Warn("JWT secret is empty; authentication interceptor will reject requests")
	}

	publicProcedures := []string{
		authconnect.AuthServiceRegisterProcedure,
		authconnect.AuthServiceLoginProcedure,
		authconnect.AuthServiceRefreshTokenProcedure,
		authconnect.AuthServiceValidateSessionProcedure,
		authconnect.AuthServiceForgotPasswordProcedure,
		authconnect.AuthServiceResetPasswordProcedure,
		// Public statistics endpoint
	}

	tracer := otel.GetTracerProvider().Tracer("loci/api")

	var rateLimiter connect.Interceptor
	if deps.Config.Server.RateLimitPerSecond > 0 && deps.Config.Server.RateLimitBurst > 0 {
		limiter := rate.NewLimiter(
			rate.Limit(float64(deps.Config.Server.RateLimitPerSecond)),
			deps.Config.Server.RateLimitBurst,
		)
		rateLimiter = interceptors.NewRateLimitInterceptor(limiter)
	}

	requestIDInterceptor := interceptors.NewRequestIDInterceptor("X-Request-ID")
	tracingInterceptor := interceptors.NewTracingInterceptor(tracer)
	validationInterceptor := validate.NewInterceptor()
	// subscriptionInterceptor := subscription.NewRateLimitInterceptor(deps.SubscriptionService)

	// Setup interceptor chain
	interceptorChain := connect.WithInterceptors(
		requestIDInterceptor,
		tracingInterceptor,
		validationInterceptor,
		rateLimiter,
		// subscriptionInterceptor,
		interceptors.NewRecoveryInterceptor(deps.Logger),
		interceptors.NewLoggingInterceptor(deps.Logger),
		interceptors.NewAuthInterceptor(jwtSecret, publicProcedures...),
		observability.NewMetricsInterceptor(),
	)

	// Register Connect RPC routes
	registerConnectRoutes(mux, deps, interceptorChain)

	// Register Webhooks
	//if deps.PaymentService != nil {
	//	mux.Handle("/webhooks/stripe", payment.WebhookHandler(deps.PaymentService, deps.Logger))
	//	deps.Logger.Info("registered webhook", "path", "/webhooks/stripe")
	//}

	// Register health and metrics routes
	registerUtilityRoutes(mux, deps)

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{"*"},                               // For testing ONLY—narrow to specifics like "http://localhost:3000" once working. Avoid in prod.
		AllowedMethods:   c.AllowedMethods(),                          // ["GET", "POST", "OPTIONS"]
		AllowedHeaders:   append(c.AllowedHeaders(), "Authorization"), // Adds "Authorization" for safety; full list: ["Accept-Encoding", "Content-Encoding", "Content-Type", "Connect-Protocol-Version", "Connect-Timeout-Ms", "Grpc-Timeout", "X-Grpc-Web", "X-User-Agent", "Authorization"]
		ExposedHeaders:   c.ExposedHeaders(),                          // ["Grpc-Status", "Grpc-Message", "Grpc-Status-Details-Bin"]
		AllowCredentials: true,
		MaxAge:           7200, // Cache preflights for 2 hours
	})

	// Enable CORS for browser clients (Buf Studio, local frontend)
	//corsHandler := cors.New(cors.Options{
	//	AllowedOrigins: []string{
	//		"https://buf.build",
	//		"https://studio.buf.build",
	//		"http://localhost:3000",
	//	},
	//	AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodOptions},
	//	AllowedHeaders: []string{
	//		"Accept-Encoding",
	//		"Content-Encoding",
	//		"Content-Type",
	//		"Connect-Protocol-Version",
	//		"Connect-Timeout-Ms",
	//		"Grpc-Timeout",
	//		"X-Grpc-Web",
	//		"X-User-Agent",
	//	},
	//	ExposedHeaders: []string{
	//		"Grpc-Status",
	//		"Grpc-Message",
	//		"Grpc-Status-Details-Bin",
	//	},
	//	AllowCredentials: true,
	//})

	return corsHandler.Handler(mux)
}

// registerConnectRoutes registers all Connect RPC service service
func registerConnectRoutes(mux *http.ServeMux, deps *Dependencies, opts connect.HandlerOption) {
	authServicePath, authServiceHandler := authconnect.NewAuthServiceHandler(
		deps.AuthHandler,
		opts,
	)
	mux.Handle(authServicePath, wrapAuthRoute(authServiceHandler))
	deps.Logger.Info("registered Connect RPC service", "path", authServicePath)

	if deps.UserHandler != nil {
		userPath, userHandler := userconnect.NewUserServiceHandler(deps.UserHandler, opts)
		mux.Handle(userPath, userHandler)
		deps.Logger.Info("registered Connect RPC service", "path", userPath)
	}

	deps.Logger.Info("Connect RPC routes configured")
}

func wrapAuthRoute(next http.Handler) http.Handler {
	const maxBodyBytes int64 = 1 << 20 // 1 MiB

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		}

		next.ServeHTTP(w, r)
	})
}

// registerUtilityRoutes registers health check, metrics, and other utility routes
func registerUtilityRoutes(mux *http.ServeMux, deps *Dependencies) {
	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if err := deps.DB.Health(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			if _, writeErr := w.Write([]byte("database unhealthy")); writeErr != nil {
				deps.Logger.Error("failed to write health response", slog.Any("error", writeErr))
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok")); err != nil {
			deps.Logger.Error("failed to write health response", slog.Any("error", err))
		}
	})
	deps.Logger.Info("registered health check", "path", "/health")

	// Extended health with details on dependencies/env
	mux.HandleFunc("/health/details", func(w http.ResponseWriter, _ *http.Request) {
		type status struct {
			Status string `json:"status"`
			Detail string `json:"detail,omitempty"`
		}
		result := map[string]status{
			"db":    {Status: "ok"},
			"env":   {Status: "ok"},
			"ready": {Status: "ok"},
		}

		if err := deps.DB.Health(); err != nil {
			result["db"] = status{Status: "fail", Detail: err.Error()}
			result["ready"] = status{Status: "fail", Detail: "db unavailable"}
		}

		if os.Getenv("GEMINI_API_KEY") == "" {
			result["env"] = status{Status: "warn", Detail: "GEMINI_API_KEY missing"}
		}

		for _, v := range result {
			if v.Status == "fail" {
				w.WriteHeader(http.StatusServiceUnavailable)
				if err := json.NewEncoder(w).Encode(result); err != nil {
					deps.Logger.Error("failed to encode health details", slog.Any("error", err))
				}
				return
			}
		}

		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			deps.Logger.Error("failed to encode health details", slog.Any("error", err))
		}
	})
	deps.Logger.Info("registered health details", "path", "/health/details")

	// Readiness check endpoint
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ready")); err != nil {
			deps.Logger.Error("failed to write readiness response", slog.Any("error", err))
		}
	})
	deps.Logger.Info("registered readiness check", "path", "/ready")

	// Metrics endpoint (Prometheus)
	if deps.Config.Observability.MetricsEnabled {
		mux.Handle("/metrics", promhttp.Handler())
		deps.Logger.Info("registered metrics endpoint", "path", "/metrics")
	}
}
