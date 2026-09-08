package httpx_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
)

func TestDecodeJSONRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name, contentType, body string
		max                     int64
		wantCode                string
	}{
		{name: "missing content type", body: `{}`, max: 100, wantCode: "INVALID_CONTENT_TYPE"},
		{name: "unknown field", contentType: "application/json", body: `{"extra":true}`, max: 100, wantCode: "INVALID_REQUEST"},
		{name: "multiple values", contentType: "application/json", body: `{} {}`, max: 100, wantCode: "INVALID_REQUEST"},
		{name: "empty", contentType: "application/json", body: ``, max: 100, wantCode: "INVALID_REQUEST"},
		{name: "too large", contentType: "application/json", body: `{"name":"long"}`, max: 5, wantCode: "REQUEST_TOO_LARGE"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(test.body))
			r.Header.Set("Content-Type", test.contentType)
			var target struct {
				Name string `json:"name"`
			}
			err := httpx.DecodeJSON(httptest.NewRecorder(), r, &target, test.max)
			var appErr *apperror.Error
			if !errors.As(err, &appErr) || appErr.Code != test.wantCode {
				t.Fatalf("error = %#v, want code %s", err, test.wantCode)
			}
		})
	}
}

func TestProblemErrorMapping(t *testing.T) {
	tests := []struct {
		kind       apperror.Kind
		wantStatus int
	}{
		{kind: apperror.KindBadRequest, wantStatus: http.StatusBadRequest},
		{kind: apperror.KindUnauthorized, wantStatus: http.StatusUnauthorized},
		{kind: apperror.KindForbidden, wantStatus: http.StatusForbidden},
		{kind: apperror.KindNotFound, wantStatus: http.StatusNotFound},
		{kind: apperror.KindConflict, wantStatus: http.StatusConflict},
		{kind: apperror.KindValidation, wantStatus: http.StatusUnprocessableEntity},
		{kind: apperror.KindTooLarge, wantStatus: http.StatusRequestEntityTooLarge},
		{kind: apperror.KindInternal, wantStatus: http.StatusInternalServerError},
		{kind: apperror.KindUnavailable, wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			handler := httpx.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				httpx.WriteProblem(w, r, apperror.New(test.kind, "TEST_ERROR", "safe detail"))
			}))
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

type readinessChecker struct{ err error }

func (checker readinessChecker) Ping(context.Context) error { return checker.err }

func TestReadiness(t *testing.T) {
	tests := []struct {
		name       string
		checker    httpx.ReadinessChecker
		wantStatus int
	}{
		{name: "available", checker: readinessChecker{}, wantStatus: http.StatusOK},
		{name: "unavailable", checker: readinessChecker{err: errors.New("connection refused")}, wantStatus: http.StatusServiceUnavailable},
		{name: "missing checker", wantStatus: http.StatusServiceUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := httpx.RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpx.WriteReadiness(w, r, test.checker, time.Second) }))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestMiddlewareAddsRequestIDCorsAndSafeAccessLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteProblem(w, r, apperror.New(apperror.KindForbidden, "FORBIDDEN", "forbidden"))
	})
	handler := httpx.Middleware(logger, []string{"http://localhost:8081"})(mux)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/orders/secret-address", strings.NewReader(`{"phone":"+79990000000"}`))
	r.Header.Set("Origin", "http://localhost:8081")
	r.Header.Set("Authorization", "Bearer super-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || w.Header().Get(httpx.RequestIDHeader) == "" || w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("unexpected response: %d headers=%v", w.Code, w.Header())
	}
	var problem httpx.Problem
	if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || problem.RequestID == "" {
		t.Fatalf("problem = %#v, err=%v", problem, err)
	}
	logText := logs.String()
	for _, forbidden := range []string{"super-secret", "+79990000000", "secret-address"} {
		if strings.Contains(logText, forbidden) {
			t.Fatalf("log contains sensitive value %q: %s", forbidden, logText)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := httpx.Middleware(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), []string{"http://localhost:8081"})(http.NotFoundHandler())
	r := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/orders", nil)
	r.Header.Set("Origin", "http://localhost:8081")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestCORSDoesNotAllowUnknownOrigin(t *testing.T) {
	handler := httpx.Middleware(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), []string{"http://localhost:8081"})(http.NotFoundHandler())
	r := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/v1/orders", nil)
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected allowed origin: %s", w.Header().Get("Access-Control-Allow-Origin"))
	}
}
