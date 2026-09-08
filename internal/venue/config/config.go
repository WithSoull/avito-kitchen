package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	DatabaseURL            string
	PlatformBaseURL        string
	PlatformToken          string
	PartnerToken           string
	VenueStaffToken        string
	AllowedOrigins         []string
	MaxRequestBytes        int64
	MaxHeaderBytes         int
	DBConnectTimeout       time.Duration
	DBQueryTimeout         time.Duration
	DBMaxConns             int32
	DBMinConns             int32
	DBMaxConnLifetime      time.Duration
	DBMaxConnIdleTime      time.Duration
	PlatformRequestTimeout time.Duration
	MenuPublishTimeout     time.Duration
	CallbackPollInterval   time.Duration
	CallbackLease          time.Duration
	CallbackWorkers        int
	TestDecisionDelay      time.Duration
	TestDecisionStatus     int
	ShutdownTimeout        time.Duration
}

func Load() (Config, error) {
	appEnv := envOrDefault("APP_ENV", "local")
	if appEnv != "local" && appEnv != "test" && appEnv != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be local, test, or production")
	}
	timeout, err := time.ParseDuration(envOrDefault("SHUTDOWN_TIMEOUT", "10s"))
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
	platformRequestTimeout, err := positiveDuration("PLATFORM_REQUEST_TIMEOUT", "3s")
	if err != nil {
		return Config{}, err
	}
	menuPublishTimeout, err := positiveDuration("MENU_PUBLISH_TIMEOUT", "10s")
	if err != nil {
		return Config{}, err
	}
	callbackPollInterval, err := positiveDuration("CALLBACK_POLL_INTERVAL", "500ms")
	if err != nil {
		return Config{}, err
	}
	callbackLease, err := positiveDuration("CALLBACK_LEASE", "10s")
	if err != nil || callbackLease <= platformRequestTimeout {
		return Config{}, fmt.Errorf("CALLBACK_LEASE must be longer than PLATFORM_REQUEST_TIMEOUT")
	}
	callbackWorkers, err := positiveInt("CALLBACK_WORKERS", "2")
	if err != nil {
		return Config{}, err
	}
	platformToken := envOrDefault("PLATFORM_TOKEN", "dev-platform-token")
	partnerToken := envOrDefault("PARTNER_TOKEN", "dev-partner-token")
	staffToken := envOrDefault("VENUE_STAFF_TOKEN", "dev-venue-staff-token")
	testDelay, err := time.ParseDuration(envOrDefault("VENUE_TEST_DECISION_DELAY", "0s"))
	if err != nil || testDelay < 0 {
		return Config{}, fmt.Errorf("VENUE_TEST_DECISION_DELAY must be a non-negative duration")
	}
	testStatus, err := strconv.Atoi(envOrDefault("VENUE_TEST_DECISION_STATUS", "0"))
	if err != nil || (testStatus != 0 && testStatus != 429 && testStatus != 500 && testStatus != 503) {
		return Config{}, fmt.Errorf("VENUE_TEST_DECISION_STATUS must be 0, 429, 500, or 503")
	}
	if appEnv != "test" && (testDelay != 0 || testStatus != 0) {
		return Config{}, fmt.Errorf("venue fault injection is only available in APP_ENV=test")
	}
	if appEnv != "local" && appEnv != "test" {
		if unsafeToken(platformToken) || unsafeToken(partnerToken) || unsafeToken(staffToken) {
			return Config{}, fmt.Errorf("service tokens must be non-development secrets outside local/test")
		}
	}
	return Config{
		AppEnv:                 appEnv,
		HTTPAddr:               envOrDefault("HTTP_ADDR", ":8090"),
		DatabaseURL:            envOrDefault("DATABASE_URL", "postgres://avito:avito@localhost:5435/venue?sslmode=disable"),
		PlatformBaseURL:        envOrDefault("PLATFORM_BASE_URL", "http://localhost:8080"),
		PlatformToken:          platformToken,
		PartnerToken:           partnerToken,
		VenueStaffToken:        staffToken,
		AllowedOrigins:         splitCSV(envOrDefault("CORS_ALLOWED_ORIGINS", "http://localhost:8081")),
		MaxRequestBytes:        maxRequestBytes,
		MaxHeaderBytes:         maxHeaderBytes,
		DBConnectTimeout:       databaseConfig.connectTimeout,
		DBQueryTimeout:         databaseConfig.queryTimeout,
		DBMaxConns:             databaseConfig.maxConns,
		DBMinConns:             databaseConfig.minConns,
		DBMaxConnLifetime:      databaseConfig.maxConnLifetime,
		DBMaxConnIdleTime:      databaseConfig.maxConnIdleTime,
		PlatformRequestTimeout: platformRequestTimeout,
		MenuPublishTimeout:     menuPublishTimeout,
		CallbackPollInterval:   callbackPollInterval,
		CallbackLease:          callbackLease,
		CallbackWorkers:        callbackWorkers,
		TestDecisionDelay:      testDelay,
		TestDecisionStatus:     testStatus,
		ShutdownTimeout:        timeout,
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
