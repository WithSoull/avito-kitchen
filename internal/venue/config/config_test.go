package config_test

import (
	"testing"

	"github.com/WithSoull/avito-kitchen/internal/venue/config"
)

func TestProductionRejectsDevelopmentTokens(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("PLATFORM_TOKEN", "production-platform-secret")
	t.Setenv("PARTNER_TOKEN", "production-partner-secret")
	t.Setenv("VENUE_STAFF_TOKEN", "dev-staff-token")
	if _, err := config.Load(); err == nil {
		t.Fatal("expected unsafe token error")
	}
}
