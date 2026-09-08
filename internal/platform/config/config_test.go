package config_test

import (
	"strings"
	"testing"

	"github.com/WithSoull/avito-kitchen/internal/platform/config"
)

func TestProductionRejectsDevelopmentTokens(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PARTNER_TOKEN", "dev-partner-token")
	t.Setenv("PLATFORM_TOKEN", "production-platform-secret")
	_, err := config.Load()
	if err == nil || !strings.Contains(err.Error(), "non-development") {
		t.Fatalf("error = %v", err)
	}
}

func TestLocalDefaultsAreValid(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	if _, err := config.Load(); err != nil {
		t.Fatal(err)
	}
}
