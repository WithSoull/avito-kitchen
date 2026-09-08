package httpx

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

type Metrics struct {
	requests       atomic.Uint64
	errors         atomic.Uint64
	durationMicros atomic.Uint64
	jobsProcessed  atomic.Uint64
	jobRetries     atomic.Uint64
}

func Instrument(metrics *Metrics, next http.Handler) http.Handler {
	if metrics == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		metrics.requests.Add(1)
		metrics.durationMicros.Add(uint64(time.Since(started).Microseconds()))
		if recorder.status >= http.StatusBadRequest {
			metrics.errors.Add(1)
		}
	})
}

func (metrics *Metrics) JobProcessed() { metrics.jobsProcessed.Add(1) }
func (metrics *Metrics) JobRetried()   { metrics.jobRetries.Add(1) }

func (metrics *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = fmt.Fprintf(w, "avito_kitchen_http_requests_total %d\n", metrics.requests.Load())
	_, _ = fmt.Fprintf(w, "avito_kitchen_http_errors_total %d\n", metrics.errors.Load())
	_, _ = fmt.Fprintf(w, "avito_kitchen_http_duration_microseconds_total %d\n", metrics.durationMicros.Load())
	_, _ = fmt.Fprintf(w, "avito_kitchen_outbox_jobs_processed_total %d\n", metrics.jobsProcessed.Load())
	_, _ = fmt.Fprintf(w, "avito_kitchen_outbox_retries_total %d\n", metrics.jobRetries.Load())
}
