package httptransport

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/auth"
	"github.com/WithSoull/avito-kitchen/internal/platform/security/ordertoken"
	"github.com/WithSoull/avito-kitchen/internal/platform/service"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
)

type Config struct {
	PartnerToken        string
	PartnerVenueID      string
	AllowedOrigins      []string
	MaxRequestBytes     int64
	Readiness           httpx.ReadinessChecker
	ReadinessTimeout    time.Duration
	Metrics             *httpx.Metrics
	CheckoutConcurrency int
	CheckoutRateLimit   int
}

type Handler struct {
	catalog       service.Catalog
	orders        service.Orders
	partner       service.Partner
	config        Config
	logger        *slog.Logger
	checkoutSlots chan struct{}
	checkoutRate  *fixedWindowLimiter
}

func NewHandler(catalog service.Catalog, orders service.Orders, partner service.Partner, config Config, logger *slog.Logger) *Handler {
	if config.CheckoutConcurrency < 1 {
		config.CheckoutConcurrency = 32
	}
	if config.CheckoutRateLimit < 1 {
		config.CheckoutRateLimit = 100
	}
	return &Handler{catalog: catalog, orders: orders, partner: partner, config: config, logger: logger,
		checkoutSlots: make(chan struct{}, config.CheckoutConcurrency), checkoutRate: &fixedWindowLimiter{limit: config.CheckoutRateLimit}}
}

type fixedWindowLimiter struct {
	mu     sync.Mutex
	window time.Time
	count  int
	limit  int
}

func (limiter *fixedWindowLimiter) allow(now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.window.IsZero() || now.Sub(limiter.window) >= time.Second {
		limiter.window = now
		limiter.count = 0
	}
	if limiter.count >= limiter.limit {
		return false
	}
	limiter.count++
	return true
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	if h.config.Metrics != nil {
		mux.Handle("GET /metrics", h.config.Metrics)
	}
	mux.HandleFunc("GET /api/v1/venues", h.listVenues)
	mux.HandleFunc("GET /api/v1/venues/{venueID}", h.getVenue)
	mux.HandleFunc("GET /api/v1/venues/{venueID}/menu", h.getMenu)
	mux.HandleFunc("POST /api/v1/orders", h.createOrder)
	mux.HandleFunc("GET /api/v1/orders/{orderID}", h.getOrder)
	mux.HandleFunc("POST /api/v1/orders/{orderID}/cancel", h.cancelOrder)
	mux.HandleFunc("PUT /partner/v1/menu", h.authenticatePartner(h.replaceMenu))
	mux.HandleFunc("PATCH /partner/v1/availability", h.authenticatePartner(h.updateAvailability))
	mux.HandleFunc("POST /partner/v1/orders/{orderID}/events", h.authenticatePartner(h.recordOrderEvent))
	return httpx.Middleware(h.logger, h.config.AllowedOrigins)(httpx.Instrument(h.config.Metrics, mux))
}

func (*Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	httpx.WriteReadiness(w, r, h.config.Readiness, h.config.ReadinessTimeout)
}

func (h *Handler) listVenues(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			httpx.WriteProblem(w, r, malformedRequest())
			return
		}
		limit = parsed
	}
	page, err := h.catalog.ListVenues(r.Context(), r.URL.Query().Get("query"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, venuePageDTO(page))
}

func (h *Handler) getVenue(w http.ResponseWriter, r *http.Request) {
	if !httpx.IsUUID(r.PathValue("venueID")) {
		httpx.WriteProblem(w, r, malformedRequest())
		return
	}
	venue, err := h.catalog.GetVenue(r.Context(), r.PathValue("venueID"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, venueDTO(venue))
}

func (h *Handler) getMenu(w http.ResponseWriter, r *http.Request) {
	if !httpx.IsUUID(r.PathValue("venueID")) {
		httpx.WriteProblem(w, r, malformedRequest())
		return
	}
	menu, err := h.catalog.GetMenu(r.Context(), r.PathValue("venueID"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, menuDTO(menu))
}

func (h *Handler) createOrder(w http.ResponseWriter, r *http.Request) {
	if !h.checkoutRate.allow(time.Now()) {
		httpx.WriteProblem(w, r, apperror.New(apperror.KindTooManyRequests, "TOO_MANY_REQUESTS", "checkout rate limit exceeded"))
		return
	}
	select {
	case h.checkoutSlots <- struct{}{}:
		defer func() { <-h.checkoutSlots }()
	default:
		httpx.WriteProblem(w, r, apperror.New(apperror.KindTooManyRequests, "TOO_MANY_REQUESTS", "checkout concurrency limit exceeded"))
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if len(idempotencyKey) < 16 || len(idempotencyKey) > 128 {
		httpx.WriteProblem(w, r, malformedRequest())
		return
	}
	var request createOrderRequest
	if err := httpx.DecodeJSON(w, r, &request, h.config.MaxRequestBytes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	command, err := request.domain()
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	result, err := h.orders.CreateOrder(r.Context(), idempotencyKey, command)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/orders/"+result.Order.ID)
	httpx.WriteJSON(w, http.StatusAccepted, createOrderDTO(result))
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	orderID, tokenHash, err := orderAccess(r)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	order, err := h.orders.GetOrder(r.Context(), orderID, tokenHash)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, orderDTO(order))
}

func (h *Handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	orderID, tokenHash, err := orderAccess(r)
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	order, err := h.orders.CancelOrder(r.Context(), orderID, tokenHash)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, orderDTO(order))
}

func (h *Handler) replaceMenu(w http.ResponseWriter, r *http.Request) {
	var request replaceMenuRequest
	if err := httpx.DecodeJSON(w, r, &request, h.config.MaxRequestBytes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	snapshot, err := request.domain()
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	principal, _ := auth.PartnerPrincipalFromContext(r.Context())
	if err := h.partner.ReplaceMenu(r.Context(), principal.VenueID, snapshot); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) updateAvailability(w http.ResponseWriter, r *http.Request) {
	var request availabilityUpdateRequest
	if err := httpx.DecodeJSON(w, r, &request, h.config.MaxRequestBytes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	update, err := request.domain()
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	principal, _ := auth.PartnerPrincipalFromContext(r.Context())
	if err := h.partner.UpdateAvailability(r.Context(), principal.VenueID, update); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) recordOrderEvent(w http.ResponseWriter, r *http.Request) {
	if !httpx.IsUUID(r.PathValue("orderID")) {
		httpx.WriteProblem(w, r, malformedRequest())
		return
	}
	var request orderEventRequest
	if err := httpx.DecodeJSON(w, r, &request, h.config.MaxRequestBytes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	event, err := request.domain()
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	principal, _ := auth.PartnerPrincipalFromContext(r.Context())
	if err := h.partner.RecordOrderEvent(r.Context(), principal.VenueID, r.PathValue("orderID"), event); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) authenticatePartner(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := httpx.BearerToken(r)
		if !ok || !httpx.SecureEqual(token, h.config.PartnerToken) {
			httpx.WriteProblem(w, r, apperror.New(apperror.KindUnauthorized, "UNAUTHORIZED", "valid partner token is required"))
			return
		}
		ctx := auth.WithPartnerPrincipal(r.Context(), auth.PartnerPrincipal{VenueID: h.config.PartnerVenueID})
		next(w, r.WithContext(ctx))
	}
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httpx.WriteProblem(w, r, err)
}

func orderAccess(r *http.Request) (string, string, error) {
	orderID := r.PathValue("orderID")
	if !httpx.IsUUID(orderID) {
		return "", "", malformedRequest()
	}
	hash, err := ordertoken.Hash(strings.TrimSpace(r.Header.Get("X-Order-Token")))
	if err != nil {
		return "", "", apperror.New(apperror.KindNotFound, "ORDER_NOT_FOUND", "order was not found")
	}
	return orderID, hash, nil
}

func malformedRequest() error {
	return apperror.New(apperror.KindBadRequest, "INVALID_REQUEST", "request path or headers are invalid")
}
