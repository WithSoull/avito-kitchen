package httptransport_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	httptransport "github.com/WithSoull/avito-kitchen/internal/platform/transport/http"
)

func TestRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       string
		wantStatus int
	}{
		{name: "health", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK},
		{name: "partner requires token", method: http.MethodPut, path: "/partner/v1/menu", wantStatus: http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := httptransport.NewHandler(
				nil,
				nil,
				nil,
				httptransport.Config{PartnerToken: "test-partner-token", PartnerVenueID: "00000000-0000-4000-8000-000000000001", MaxRequestBytes: 1024},
				slog.New(slog.NewTextHandler(io.Discard, nil)),
			).Routes()

			request := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, strings.NewReader(tt.body))
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
		})
	}
}

func TestOrderTokenIsRequired(t *testing.T) {
	handler := httptransport.NewHandler(nil, nil, nil, httptransport.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/orders/00000000-0000-4000-8000-000000000002", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}
