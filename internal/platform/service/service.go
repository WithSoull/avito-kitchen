package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/platform/repository"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

type Catalog interface {
	ListVenues(context.Context, string, string, int) (domain.VenuePage, error)
	GetVenue(context.Context, string) (domain.Venue, error)
	GetMenu(context.Context, string) (domain.Menu, error)
}

type Orders interface {
	CreateOrder(context.Context, string, domain.CreateOrderRequest) (domain.CreateOrderResult, error)
	GetOrder(context.Context, string, string) (domain.Order, error)
	CancelOrder(context.Context, string, string) (domain.Order, error)
}

type Partner interface {
	ReplaceMenu(context.Context, string, domain.MenuSnapshot) error
	UpdateAvailability(context.Context, string, domain.AvailabilityUpdate) error
	RecordOrderEvent(context.Context, string, string, domain.OrderEvent) error
}

type CatalogService struct{ repository repository.Catalog }
type PartnerService struct{ repository repository.Partner }
type OrdersService struct {
	repository          repository.Orders
	tokenSecret         string
	confirmationTimeout time.Duration
}

func NewCatalog(repo repository.Catalog) *CatalogService { return &CatalogService{repository: repo} }
func NewPartner(repo repository.Partner) *PartnerService { return &PartnerService{repository: repo} }
func NewOrders(repo repository.Orders, tokenSecret string, confirmationTimeout time.Duration) *OrdersService {
	return &OrdersService{repository: repo, tokenSecret: tokenSecret, confirmationTimeout: confirmationTimeout}
}

func (service *CatalogService) ListVenues(ctx context.Context, search, cursor string, limit int) (domain.VenuePage, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || utf8.RuneCountInString(search) > 200 {
		return domain.VenuePage{}, invalidRequest("catalog query is invalid")
	}
	afterID, err := decodeCursor(cursor)
	if err != nil {
		return domain.VenuePage{}, invalidRequest("cursor is invalid")
	}
	page, err := service.repository.ListVenues(ctx, domain.VenueListQuery{Search: strings.TrimSpace(search), AfterID: afterID, Limit: limit})
	if err != nil {
		return domain.VenuePage{}, mapRepositoryError(err)
	}
	if page.NextCursor != "" {
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(page.NextCursor))
	}
	return page, nil
}

func (service *CatalogService) GetVenue(ctx context.Context, venueID string) (domain.Venue, error) {
	venue, err := service.repository.GetVenue(ctx, venueID)
	if err != nil {
		return domain.Venue{}, mapRepositoryError(err)
	}
	return venue, nil
}

func (service *CatalogService) GetMenu(ctx context.Context, venueID string) (domain.Menu, error) {
	menu, err := service.repository.GetMenu(ctx, venueID)
	if err != nil {
		return domain.Menu{}, mapRepositoryError(err)
	}
	return menu, nil
}

func (service *PartnerService) ReplaceMenu(ctx context.Context, venueID string, snapshot domain.MenuSnapshot) error {
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	payload, err := json.Marshal(canonicalSnapshot(snapshot))
	if err != nil {
		return apperror.Wrap(apperror.KindInternal, "INTERNAL_ERROR", "internal server error", err)
	}
	hash := sha256.Sum256(payload)
	return mapRepositoryError(service.repository.ReplaceMenu(ctx, venueID, snapshot, hex.EncodeToString(hash[:])))
}

type hashSnapshot struct {
	Version    int64          `json:"version"`
	Categories []hashCategory `json:"categories"`
}
type hashCategory struct {
	ExternalID string     `json:"external_id"`
	Name       string     `json:"name"`
	Items      []hashItem `json:"items"`
}
type hashItem struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	IsAvailable bool   `json:"is_available"`
}

func canonicalSnapshot(snapshot domain.MenuSnapshot) hashSnapshot {
	canonical := hashSnapshot{Version: snapshot.Version, Categories: make([]hashCategory, len(snapshot.Categories))}
	for categoryIndex, category := range snapshot.Categories {
		mapped := hashCategory{ExternalID: category.ExternalID, Name: category.Name, Items: make([]hashItem, len(category.Items))}
		for itemIndex, item := range category.Items {
			mapped.Items[itemIndex] = hashItem{ExternalID: item.ExternalID, Name: item.Name, Description: item.Description, Amount: item.Price.Amount, Currency: item.Price.Currency, IsAvailable: item.IsAvailable}
		}
		canonical.Categories[categoryIndex] = mapped
	}
	return canonical
}

func (service *PartnerService) UpdateAvailability(ctx context.Context, venueID string, update domain.AvailabilityUpdate) error {
	if utf8.RuneCountInString(update.Reason) > 200 {
		return invalidRequest("availability reason is too long")
	}
	return mapRepositoryError(service.repository.UpdateAvailability(ctx, venueID, update))
}

func (service *PartnerService) RecordOrderEvent(ctx context.Context, venueID, orderID string, event domain.OrderEvent) error {
	validType := event.Type == "order_preparing" || event.Type == "order_ready" || event.Type == "order_completed" || event.Type == "order_cancelled"
	if !shareduuid.IsValid(event.EventID) || event.Sequence < 1 || !validType || event.OccurredAt.IsZero() || utf8.RuneCountInString(event.Reason) > 500 {
		return invalidRequest("order event is invalid")
	}
	return mapRepositoryError(service.repository.RecordOrderEvent(ctx, venueID, orderID, event))
}

func validateSnapshot(snapshot domain.MenuSnapshot) error {
	if snapshot.Version < 1 || len(snapshot.Categories) < 1 || len(snapshot.Categories) > 100 {
		return invalidRequest("menu snapshot is invalid")
	}
	categories := make(map[string]struct{}, len(snapshot.Categories))
	items := make(map[string]struct{})
	for _, category := range snapshot.Categories {
		if !validRequiredText(category.ExternalID, 128) || !validRequiredText(category.Name, 200) {
			return invalidRequest("menu category is invalid")
		}
		if _, exists := categories[category.ExternalID]; exists {
			return invalidRequest("menu category external_id must be unique")
		}
		categories[category.ExternalID] = struct{}{}
		if len(category.Items) > 1000 {
			return invalidRequest("menu category has too many items")
		}
		for _, item := range category.Items {
			if !validRequiredText(item.ExternalID, 128) || !validRequiredText(item.Name, 200) || !validOptionalText(item.Description, 2000) ||
				item.Price.Amount < 0 || !validCurrency(item.Price.Currency) {
				return invalidRequest("menu item is invalid")
			}
			if _, exists := items[item.ExternalID]; exists {
				return invalidRequest("menu item external_id must be unique")
			}
			items[item.ExternalID] = struct{}{}
		}
	}
	return nil
}

func validRequiredText(value string, maxRunes int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= maxRunes
}

func validOptionalText(value string, maxRunes int) bool {
	return utf8.RuneCountInString(value) <= maxRunes
}

func validCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, symbol := range []byte(value) {
		if symbol < 'A' || symbol > 'Z' {
			return false
		}
	}
	return true
}

func decodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || !shareduuid.IsValid(string(decoded)) {
		return "", errors.New("invalid cursor")
	}
	return string(decoded), nil
}

func mapRepositoryError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, repository.ErrVenueNotFound):
		return apperror.New(apperror.KindNotFound, "VENUE_NOT_FOUND", "venue was not found")
	case errors.Is(err, repository.ErrMenuNotFound):
		return apperror.New(apperror.KindNotFound, "MENU_NOT_FOUND", "menu was not found")
	case errors.Is(err, repository.ErrStaleMenuVersion):
		return apperror.New(apperror.KindConflict, "STALE_MENU_VERSION", "menu version is older than the current version")
	case errors.Is(err, repository.ErrMenuVersionConflict):
		return apperror.New(apperror.KindConflict, "MENU_VERSION_CONFLICT", "menu version was already used with another snapshot")
	case errors.Is(err, repository.ErrVenueClosed):
		return apperror.New(apperror.KindConflict, "VENUE_CLOSED", "venue is not accepting orders")
	case errors.Is(err, repository.ErrMenuChanged):
		return apperror.New(apperror.KindConflict, "MENU_CHANGED", "menu changed after it was loaded")
	case errors.Is(err, repository.ErrItemsUnavailable):
		return apperror.New(apperror.KindConflict, "ITEMS_UNAVAILABLE", "one or more items are unavailable")
	case errors.Is(err, repository.ErrIdempotencyKeyReused):
		return apperror.New(apperror.KindConflict, "IDEMPOTENCY_KEY_REUSED", "idempotency key was used with another request")
	case errors.Is(err, repository.ErrMinimumOrder):
		return invalidRequest("minimum order amount is not reached")
	case errors.Is(err, repository.ErrAmountOverflow):
		return invalidRequest("order amount is too large")
	case errors.Is(err, repository.ErrOrderNotFound):
		return apperror.New(apperror.KindNotFound, "ORDER_NOT_FOUND", "order was not found")
	case errors.Is(err, repository.ErrOrderVenueMismatch):
		return apperror.New(apperror.KindForbidden, "ORDER_VENUE_MISMATCH", "order belongs to another venue")
	case errors.Is(err, repository.ErrEventConflict):
		return apperror.New(apperror.KindConflict, "EVENT_ID_CONFLICT", "event id was already used for another event")
	case errors.Is(err, repository.ErrEventSequenceConflict):
		return apperror.New(apperror.KindConflict, "EVENT_SEQUENCE_CONFLICT", "event sequence is not the next expected value")
	case errors.Is(err, repository.ErrInvalidOrderTransition):
		return apperror.New(apperror.KindConflict, "INVALID_ORDER_TRANSITION", "event is not allowed from the current order status")
	case errors.Is(err, repository.ErrOrderCancellationNotAllowed):
		return apperror.New(apperror.KindConflict, "ORDER_CANCELLATION_NOT_ALLOWED", "order cannot be cancelled in its current state")
	default:
		return apperror.Wrap(apperror.KindInternal, "INTERNAL_ERROR", "internal server error", err)
	}
}

func invalidRequest(detail string) error {
	return apperror.New(apperror.KindValidation, "VALIDATION_FAILED", detail)
}
