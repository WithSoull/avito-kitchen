package httptransport_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httptransport "github.com/WithSoull/avito-kitchen/internal/venue/transport/http"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		token      string
		body       string
		wantStatus int
	}{
		{name: "platform token required", path: "/integration/v1/orders", wantStatus: http.StatusUnauthorized},
		{name: "staff token required", path: "/management/v1/orders", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := httptransport.NewHandler(
				nil,
				httptransport.Config{PlatformToken: "test-platform-token", StaffToken: "test-staff-token", MaxRequestBytes: 1024},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			).Routes()

			method := http.MethodPost
			if tt.path == "/management/v1/orders" {
				method = http.MethodGet
			}
			request := httptest.NewRequestWithContext(context.Background(), method, tt.path, strings.NewReader(tt.body))
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.body != "" {
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Idempotency-Key", "test-idempotency-key")
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}
