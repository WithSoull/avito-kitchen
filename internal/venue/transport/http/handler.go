package httptransport

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
	"github.com/WithSoull/avito-kitchen/internal/venue/service"
)

type Config struct {
	PlatformToken      string
	StaffToken         string
	AllowedOrigins     []string
	MaxRequestBytes    int64
	Readiness          httpx.ReadinessChecker
	ReadinessTimeout   time.Duration
	TestDecisionDelay  time.Duration
	TestDecisionStatus int
	Metrics            *httpx.Metrics
}

type Handler struct {
	orders        service.Orders
	config        Config
	logger        *slog.Logger
	testFaultUsed atomic.Bool
}

func NewHandler(orders service.Orders, config Config, logger *slog.Logger) *Handler {
	return &Handler{orders: orders, config: config, logger: logger}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /readyz", h.ready)
	if h.config.Metrics != nil {
		mux.Handle("GET /metrics", h.config.Metrics)
	}
	mux.HandleFunc("POST /integration/v1/orders", h.authenticate(h.config.PlatformToken, "valid platform token is required", h.decideOrder))
	mux.HandleFunc("POST /integration/v1/orders/{orderID}/cancel", h.authenticate(h.config.PlatformToken, "valid platform token is required", h.cancelOrder))
	mux.HandleFunc("GET /management/v1/orders", h.authenticate(h.config.StaffToken, "valid venue staff token is required", h.listOrders))
	mux.HandleFunc("GET /management/v1/orders/{orderID}", h.authenticate(h.config.StaffToken, "valid venue staff token is required", h.getOrder))
	mux.HandleFunc("POST /management/v1/orders/{orderID}/start-preparing", h.authenticate(h.config.StaffToken, "valid venue staff token is required", h.startPreparing))
	mux.HandleFunc("POST /management/v1/orders/{orderID}/mark-ready", h.authenticate(h.config.StaffToken, "valid venue staff token is required", h.markReady))
	mux.HandleFunc("POST /management/v1/orders/{orderID}/complete", h.authenticate(h.config.StaffToken, "valid venue staff token is required", h.complete))
	return httpx.Middleware(h.logger, h.config.AllowedOrigins)(httpx.Instrument(h.config.Metrics, mux))
}

func (*Handler) health(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	httpx.WriteReadiness(w, r, h.config.Readiness, h.config.ReadinessTimeout)
}

func (h *Handler) decideOrder(w http.ResponseWriter, r *http.Request) {
	injectFault := (h.config.TestDecisionDelay > 0 || h.config.TestDecisionStatus != 0) && h.testFaultUsed.CompareAndSwap(false, true)
	if injectFault && h.config.TestDecisionDelay > 0 {
		timer := time.NewTimer(h.config.TestDecisionDelay)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
	if injectFault && h.config.TestDecisionStatus != 0 {
		w.WriteHeader(h.config.TestDecisionStatus)
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")
	keyLength := utf8.RuneCountInString(idempotencyKey)
	if keyLength < 16 || keyLength > 128 {
		httpx.WriteProblem(w, r, malformedRequest())
		return
	}
	var request orderCommandRequest
	if err := httpx.DecodeJSON(w, r, &request, h.config.MaxRequestBytes); err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	command, err := request.domain()
	if err != nil {
		httpx.WriteProblem(w, r, err)
		return
	}
	decision, err := h.orders.DecideOrder(r.Context(), idempotencyKey, command)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, decisionDTO(decision))
}

func (h *Handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	decision, err := h.orders.CancelOrder(r.Context(), r.PathValue("orderID"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, decision)
}

func (h *Handler) listOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.orders.ListOrders(r.Context())
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	response := managedOrderPageResponse{Items: make([]managedOrderResponse, len(orders))}
	for index, order := range orders {
		response.Items[index] = managedOrderDTO(order)
	}
	httpx.WriteJSON(w, http.StatusOK, response)
}
func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	order, err := h.orders.GetOrder(r.Context(), r.PathValue("orderID"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, managedOrderDTO(order))
}
func (h *Handler) startPreparing(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	order, err := h.orders.StartPreparing(r.Context(), r.PathValue("orderID"))
	h.writeManagedOrder(w, r, order, err)
}
func (h *Handler) markReady(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	order, err := h.orders.MarkReady(r.Context(), r.PathValue("orderID"))
	h.writeManagedOrder(w, r, order, err)
}
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	order, err := h.orders.Complete(r.Context(), r.PathValue("orderID"))
	h.writeManagedOrder(w, r, order, err)
}

func (h *Handler) writeManagedOrder(w http.ResponseWriter, r *http.Request, order domain.ManagedOrder, err error) {
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, managedOrderDTO(order))
}

func (h *Handler) authenticate(expected, detail string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := httpx.BearerToken(r)
		if !ok || !httpx.SecureEqual(token, expected) {
			httpx.WriteProblem(w, r, apperror.New(apperror.KindUnauthorized, "UNAUTHORIZED", detail))
			return
		}
		next(w, r)
	}
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	httpx.WriteProblem(w, r, err)
}

func validPath(w http.ResponseWriter, r *http.Request) bool {
	if httpx.IsUUID(r.PathValue("orderID")) {
		return true
	}
	httpx.WriteProblem(w, r, malformedRequest())
	return false
}

func malformedRequest() error {
	return apperror.New(apperror.KindBadRequest, "INVALID_REQUEST", "request path or headers are invalid")
}
