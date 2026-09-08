package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultShutdownTimeout = 10 * time.Second

type Config struct {
	AppEnv              string
	HTTPAddr            string
	DatabaseURL         string
	PartnerToken        string
	VenueBaseURL        string
	PlatformToken       string
	OrderTokenSecret    string
	PartnerVenueID      string
	AllowedOrigins      []string
	MaxRequestBytes     int64
	MaxHeaderBytes      int
	DBConnectTimeout    time.Duration
	DBQueryTimeout      time.Duration
	DBMaxConns          int32
	DBMinConns          int32
	DBMaxConnLifetime   time.Duration
	DBMaxConnIdleTime   time.Duration
	ShutdownTimeout     time.Duration
	ConfirmationTimeout time.Duration
	VenueRequestTimeout time.Duration
	OutboxPollInterval  time.Duration
	OutboxLease         time.Duration
	OutboxWorkers       int
	OutboxMaxAttempts   int
	CheckoutConcurrency int
	CheckoutRateLimit   int
}

func Load() (Config, error) {
	appEnv := envOrDefault("APP_ENV", "local")
	if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be local, test, or production")
	}
	timeout, err := time.ParseDuration(envOrDefault("SHUTDOWN_TIMEOUT", defaultShutdownTimeout.String()))
	if err != nil {
		return Config{}, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
	}

	maxRequestBytes, err := strconv.ParseInt(envOrDefault("MAX_REQUEST_BODY_BYTES", "1048576"), 10, 64)
	if err != nil || maxRequestBytes < 1 {
		return Config{}, fmt.Errorf("MAX_REQUEST_BODY_BYTES must be a positive integer")
	}
	maxHeaderBytes, err := strconv.Atoi(envOrDefault("MAX_HEADER_BYTES", "1048576"))
	if err != nil || maxHeaderBytes < 1 {
		return Config{}, fmt.Errorf("MAX_HEADER_BYTES must be a positive integer")
	}
	databaseConfig, err := loadDatabaseConfig()
	if err != nil {
		return Config{}, err
	}
	partnerToken := envOrDefault("PARTNER_TOKEN", "dev-partner-token")
	platformToken := envOrDefault("PLATFORM_TOKEN", "dev-platform-token")
	orderTokenSecret := envOrDefault("ORDER_TOKEN_SECRET", "dev-order-token-secret-change-me-32-bytes")
	if len(orderTokenSecret) < 32 {
		return Config{}, fmt.Errorf("ORDER_TOKEN_SECRET must contain at least 32 characters")
	}
	confirmationTimeout, err := positiveDuration("ORDER_CONFIRMATION_TIMEOUT", "2m")
	if err != nil {
		return Config{}, err
	}
	venueRequestTimeout, err := positiveDuration("VENUE_REQUEST_TIMEOUT", "3s")
	if err != nil {
		return Config{}, err
	}
	outboxPollInterval, err := positiveDuration("OUTBOX_POLL_INTERVAL", "500ms")
	if err != nil {
		return Config{}, err
	}
	outboxLease, err := positiveDuration("OUTBOX_LEASE", "10s")
	if err != nil || outboxLease <= venueRequestTimeout {
		return Config{}, fmt.Errorf("OUTBOX_LEASE must be longer than VENUE_REQUEST_TIMEOUT")
	}
	outboxWorkers, err := positiveInt("OUTBOX_WORKERS", "2")
	if err != nil {
		return Config{}, err
	}
	outboxMaxAttempts, err := positiveInt("OUTBOX_MAX_ATTEMPTS", "5")
	if err != nil {
		return Config{}, err
	}
	checkoutConcurrency, err := positiveInt("CHECKOUT_CONCURRENCY", "32")
	if err != nil {
		return Config{}, err
	}
	checkoutRateLimit, err := positiveInt("CHECKOUT_RATE_LIMIT", "100")
	if err != nil {
		return Config{}, err
	}
	if appEnv != "local" && appEnv != "test" {
		if unsafeToken(partnerToken) || unsafeToken(platformToken) || unsafeToken(orderTokenSecret) {
			return Config{}, fmt.Errorf("service tokens must be non-development secrets outside local/test")
		}
	}

	return Config{
		AppEnv:              appEnv,
		HTTPAddr:            envOrDefault("HTTP_ADDR", ":8080"),
		DatabaseURL:         envOrDefault("DATABASE_URL", "postgres://avito:avito@localhost:5435/avito_kitchen?sslmode=disable"),
		PartnerToken:        partnerToken,
		VenueBaseURL:        envOrDefault("VENUE_BASE_URL", "http://localhost:8090"),
		PlatformToken:       platformToken,
		OrderTokenSecret:    orderTokenSecret,
		PartnerVenueID:      envOrDefault("PARTNER_VENUE_ID", "00000000-0000-4000-8000-000000000001"),
		AllowedOrigins:      splitCSV(envOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:8081")),
		MaxRequestBytes:     maxRequestBytes,
		MaxHeaderBytes:      maxHeaderBytes,
		DBConnectTimeout:    databaseConfig.connectTimeout,
		DBQueryTimeout:      databaseConfig.queryTimeout,
		DBMaxConns:          databaseConfig.maxConns,
		DBMinConns:          databaseConfig.minConns,
		DBMaxConnLifetime:   databaseConfig.maxConnLifetime,
		DBMaxConnIdleTime:   databaseConfig.maxConnIdleTime,
		ShutdownTimeout:     timeout,
		ConfirmationTimeout: confirmationTimeout,
		VenueRequestTimeout: venueRequestTimeout,
		OutboxPollInterval:  outboxPollInterval,
		OutboxLease:         outboxLease,
		OutboxWorkers:       outboxWorkers,
		OutboxMaxAttempts:   outboxMaxAttempts,
		CheckoutConcurrency: checkoutConcurrency,
		CheckoutRateLimit:   checkoutRateLimit,
	}, nil
}

type databaseConfig struct {
	connectTimeout, queryTimeout, maxConnLifetime, maxConnIdleTime time.Duration
	maxConns, minConns                                             int32
}

func loadDatabaseConfig() (databaseConfig, error) {
	connectTimeout, err := positiveDuration("DB_CONNECT_TIMEOUT", "5s")
	if err != nil {
		return databaseConfig{}, err
	}
	queryTimeout, err := positiveDuration("DB_QUERY_TIMEOUT", "3s")
	if err != nil {
		return databaseConfig{}, err
	}
	maxConnLifetime, err := positiveDuration("DB_MAX_CONN_LIFETIME", "30m")
	if err != nil {
		return databaseConfig{}, err
	}
	maxConnIdleTime, err := positiveDuration("DB_MAX_CONN_IDLE_TIME", "5m")
	if err != nil {
		return databaseConfig{}, err
	}
	maxConns64, err := strconv.ParseInt(envOrDefault("DB_MAX_CONNS", "10"), 10, 32)
	if err != nil || maxConns64 < 1 {
		return databaseConfig{}, fmt.Errorf("DB_MAX_CONNS must be a positive integer")
	}
	minConns64, err := strconv.ParseInt(envOrDefault("DB_MIN_CONNS", "1"), 10, 32)
	if err != nil || minConns64 < 0 || minConns64 > maxConns64 {
		return databaseConfig{}, fmt.Errorf("DB_MIN_CONNS must be between zero and DB_MAX_CONNS")
	}
	return databaseConfig{connectTimeout: connectTimeout, queryTimeout: queryTimeout, maxConnLifetime: maxConnLifetime, maxConnIdleTime: maxConnIdleTime, maxConns: int32(maxConns64), minConns: int32(minConns64)}, nil
}

func positiveDuration(key, fallback string) (time.Duration, error) {
	value, err := time.ParseDuration(envOrDefault(key, fallback))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return value, nil
}

func positiveInt(key, fallback string) (int, error) {
	value, err := strconv.Atoi(envOrDefault(key, fallback))
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return value, nil
}

func unsafeToken(value string) bool {
	return strings.TrimSpace(value) == "" || strings.HasPrefix(value, "dev-")
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
