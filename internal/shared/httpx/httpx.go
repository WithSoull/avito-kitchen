package httpx

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
)

const RequestIDHeader = "X-Request-ID"

var requestIDFallback atomic.Uint64

type contextKey string

const requestIDKey contextKey = "request-id"

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Detail    string `json:"detail"`
	RequestID string `json:"request_id"`
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return apperror.New(apperror.KindBadRequest, "INVALID_CONTENT_TYPE", "Content-Type must be application/json")
	}
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return apperror.New(apperror.KindTooLarge, "REQUEST_TOO_LARGE", "request body is too large")
		}
		if errors.Is(err, io.EOF) {
			return apperror.New(apperror.KindBadRequest, "INVALID_REQUEST", "request body must contain one JSON object")
		}
		return apperror.New(apperror.KindBadRequest, "INVALID_REQUEST", "request body contains invalid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperror.New(apperror.KindBadRequest, "INVALID_REQUEST", "request body must contain one JSON object")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	problem := problemFromError(err)
	problem.RequestID = RequestID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(problem.Status)
	_ = json.NewEncoder(w).Encode(problem)
}

func problemFromError(err error) Problem {
	appErr := &apperror.Error{}
	if !errors.As(err, &appErr) {
		return Problem{Type: "about:blank", Title: http.StatusText(http.StatusInternalServerError), Status: http.StatusInternalServerError, Code: "INTERNAL_ERROR", Detail: "internal server error"}
	}

	status := map[apperror.Kind]int{
		apperror.KindBadRequest: http.StatusBadRequest, apperror.KindUnauthorized: http.StatusUnauthorized,
		apperror.KindForbidden: http.StatusForbidden, apperror.KindNotFound: http.StatusNotFound,
		apperror.KindConflict: http.StatusConflict, apperror.KindValidation: http.StatusUnprocessableEntity,
		apperror.KindTooLarge:        http.StatusRequestEntityTooLarge,
		apperror.KindTooManyRequests: http.StatusTooManyRequests,
		apperror.KindUnavailable:     http.StatusServiceUnavailable,
	}[appErr.Kind]
	if status == 0 {
		status = http.StatusInternalServerError
	}
	detail := appErr.Detail
	if status == http.StatusInternalServerError {
		detail = "internal server error"
	}
	return Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Code: appErr.Code, Detail: detail}
}

func Middleware(logger *slog.Logger, allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return RequestIDMiddleware(RecoverMiddleware(logger, AccessLogMiddleware(logger, CORSMiddleware(allowedOrigins, next))))
	}
}

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		if !validUUID(requestID) {
			requestID = newUUID()
		}
		w.Header().Set(RequestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, requestID)))
	})
}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(data)
}

func AccessLogMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("HTTP request", "method", r.Method, "route", routePattern(r), "status", status, "duration_ms", time.Since(started).Milliseconds(), "request_id", RequestID(r.Context()))
	})
}

func RecoverMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic recovered", "request_id", RequestID(r.Context()))
				WriteProblem(w, r, apperror.New(apperror.KindInternal, "INTERNAL_ERROR", "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func CORSMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin = strings.TrimSpace(origin); origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Order-Token, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func BearerToken(r *http.Request) (string, bool) {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(value, "Bearer ")
	return token, token != ""
}

func SecureEqual(left, right string) bool {
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func routePattern(r *http.Request) string {
	if pattern := r.Pattern; pattern != "" {
		return pattern
	}
	return "unmatched"
}

func newUUID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		binary.BigEndian.PutUint64(value[:8], requestIDFallback.Add(1))
		binary.BigEndian.PutUint64(value[8:], uint64(time.Now().UnixNano()))
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}

func IsUUID(value string) bool { return validUUID(value) }

type ReadinessChecker interface {
	Ping(context.Context) error
}

func WriteReadiness(w http.ResponseWriter, r *http.Request, checker ReadinessChecker, timeout time.Duration) {
	if checker == nil {
		WriteProblem(w, r, apperror.New(apperror.KindUnavailable, "SERVICE_UNAVAILABLE", "database is unavailable"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	if err := checker.Ping(ctx); err != nil {
		WriteProblem(w, r, apperror.Wrap(apperror.KindUnavailable, "SERVICE_UNAVAILABLE", "database is unavailable", err))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
