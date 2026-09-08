package httptransport

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
)

type venueResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Address            string `json:"address,omitempty"`
	Currency           string `json:"currency"`
	MinimumOrderAmount int64  `json:"minimum_order_amount"`
	IsAcceptingOrders  bool   `json:"is_accepting_orders"`
}

type venuePageResponse struct {
	Items      []venueSummaryResponse `json:"items"`
	NextCursor *string                `json:"next_cursor,omitempty"`
}

type venueSummaryResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	Currency           string `json:"currency"`
	MinimumOrderAmount int64  `json:"minimum_order_amount"`
	IsAcceptingOrders  bool   `json:"is_accepting_orders"`
}

type menuResponse struct {
	VenueID    string                 `json:"venue_id"`
	Version    int64                  `json:"version"`
	UpdatedAt  time.Time              `json:"updated_at"`
	Categories []menuCategoryResponse `json:"categories"`
}

type menuCategoryResponse struct {
	ExternalID string             `json:"external_id"`
	Name       string             `json:"name"`
	Items      []menuItemResponse `json:"items"`
}

type menuItemResponse struct {
	ID          string       `json:"id"`
	ExternalID  string       `json:"external_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Price       moneyRequest `json:"price"`
	IsAvailable bool         `json:"is_available"`
}

func venueDTO(venue domain.Venue) venueResponse {
	return venueResponse{ID: venue.ID, Name: venue.Name, Description: venue.Description, Address: venue.Address, Currency: venue.Currency, MinimumOrderAmount: venue.MinimumOrderAmount, IsAcceptingOrders: venue.IsAcceptingOrders}
}

func venuePageDTO(page domain.VenuePage) venuePageResponse {
	response := venuePageResponse{Items: make([]venueSummaryResponse, len(page.Items))}
	for index, venue := range page.Items {
		response.Items[index] = venueSummaryResponse{ID: venue.ID, Name: venue.Name, Description: venue.Description, Currency: venue.Currency, MinimumOrderAmount: venue.MinimumOrderAmount, IsAcceptingOrders: venue.IsAcceptingOrders}
	}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	return response
}

func menuDTO(menu domain.Menu) menuResponse {
	response := menuResponse{VenueID: menu.VenueID, Version: menu.Version, UpdatedAt: menu.UpdatedAt, Categories: make([]menuCategoryResponse, len(menu.Categories))}
	for categoryIndex, category := range menu.Categories {
		categoryResponse := menuCategoryResponse{ExternalID: category.ExternalID, Name: category.Name, Items: make([]menuItemResponse, len(category.Items))}
		for itemIndex, item := range category.Items {
			categoryResponse.Items[itemIndex] = menuItemResponse{ID: item.ID, ExternalID: item.ExternalID, Name: item.Name, Description: item.Description, Price: moneyRequest{Amount: item.Price.Amount, Currency: item.Price.Currency}, IsAvailable: item.IsAvailable}
		}
		response.Categories[categoryIndex] = categoryResponse
	}
	return response
}

type createOrderRequest struct {
	CustomerRef string             `json:"customer_ref"`
	VenueID     string             `json:"venue_id"`
	MenuVersion int64              `json:"menu_version"`
	Items       []orderItemRequest `json:"items"`
	Delivery    deliveryRequest    `json:"delivery"`
}

type orderItemRequest struct {
	MenuItemID string `json:"menu_item_id"`
	Quantity   int    `json:"quantity"`
}

type deliveryRequest struct {
	Address addressRequest `json:"address"`
	Phone   string         `json:"phone"`
	Comment string         `json:"comment"`
}

type addressRequest struct {
	City      string `json:"city"`
	Street    string `json:"street"`
	House     string `json:"house"`
	Apartment string `json:"apartment"`
}

type createOrderResponse struct {
	Order         orderResponse `json:"order"`
	TrackingToken string        `json:"tracking_token"`
}

type orderResponse struct {
	ID                 string              `json:"id"`
	CustomerRef        string              `json:"customer_ref"`
	VenueID            string              `json:"venue_id"`
	ExternalOrderID    *string             `json:"external_order_id"`
	Status             string              `json:"status"`
	CancellationStatus string              `json:"cancellation_status"`
	RejectionReason    *string             `json:"rejection_reason"`
	Items              []orderItemResponse `json:"items"`
	ItemsAmount        int64               `json:"items_amount"`
	DeliveryAmount     int64               `json:"delivery_amount"`
	TotalAmount        int64               `json:"total_amount"`
	Currency           string              `json:"currency"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

type orderItemResponse struct {
	MenuItemID     string `json:"menu_item_id"`
	ExternalItemID string `json:"external_item_id"`
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	UnitPrice      int64  `json:"unit_price"`
	TotalPrice     int64  `json:"total_price"`
}

func createOrderDTO(result domain.CreateOrderResult) createOrderResponse {
	return createOrderResponse{Order: orderDTO(result.Order), TrackingToken: result.TrackingToken}
}

func orderDTO(order domain.Order) orderResponse {
	response := orderResponse{ID: order.ID, CustomerRef: order.CustomerRef, VenueID: order.VenueID,
		Status: order.Status, CancellationStatus: order.CancellationStatus, ItemsAmount: order.ItemsAmount,
		DeliveryAmount: order.DeliveryAmount, TotalAmount: order.TotalAmount, Currency: order.Currency,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt, Items: make([]orderItemResponse, len(order.Items))}
	if order.ExternalOrderID != "" {
		response.ExternalOrderID = &order.ExternalOrderID
	}
	if order.RejectionReason != "" {
		response.RejectionReason = &order.RejectionReason
	}
	for index, item := range order.Items {
		response.Items[index] = orderItemResponse{MenuItemID: item.MenuItemID, ExternalItemID: item.ExternalItemID,
			Name: item.Name, Quantity: item.Quantity, UnitPrice: item.UnitPrice, TotalPrice: item.TotalPrice}
	}
	return response
}

func (request createOrderRequest) domain() (domain.CreateOrderRequest, error) {
	if !validRequiredText(request.CustomerRef, 128) || !httpx.IsUUID(request.VenueID) || request.MenuVersion < 1 || len(request.Items) < 1 || len(request.Items) > 100 {
		return domain.CreateOrderRequest{}, invalidRequest()
	}
	items := make([]domain.OrderItemRequest, len(request.Items))
	for i, item := range request.Items {
		if !httpx.IsUUID(item.MenuItemID) || item.Quantity < 1 || item.Quantity > 100 {
			return domain.CreateOrderRequest{}, invalidRequest()
		}
		items[i] = domain.OrderItemRequest{MenuItemID: item.MenuItemID, Quantity: item.Quantity}
	}
	address := request.Delivery.Address
	if !validRequiredText(address.City, 200) || !validRequiredText(address.Street, 200) || !validRequiredText(address.House, 50) ||
		!validOptionalText(address.Apartment, 50) || strings.TrimSpace(request.Delivery.Phone) == "" || !validOptionalText(request.Delivery.Comment, 500) {
		return domain.CreateOrderRequest{}, invalidRequest()
	}
	return domain.CreateOrderRequest{
		CustomerRef: request.CustomerRef, VenueID: request.VenueID, MenuVersion: request.MenuVersion, Items: items,
		Delivery: domain.Delivery{Address: domain.Address{City: address.City, Street: address.Street, House: address.House, Apartment: address.Apartment}, Phone: request.Delivery.Phone, Comment: request.Delivery.Comment},
	}, nil
}

type replaceMenuRequest struct {
	Version    int64                 `json:"version"`
	Categories []partnerMenuCategory `json:"categories"`
}

type partnerMenuCategory struct {
	ExternalID string            `json:"external_id"`
	Name       string            `json:"name"`
	Items      []partnerMenuItem `json:"items"`
}

type partnerMenuItem struct {
	ExternalID  string       `json:"external_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Price       moneyRequest `json:"price"`
	IsAvailable bool         `json:"is_available"`
}

type moneyRequest struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func (request replaceMenuRequest) domain() (domain.MenuSnapshot, error) {
	if request.Version < 1 || len(request.Categories) < 1 || len(request.Categories) > 100 {
		return domain.MenuSnapshot{}, invalidRequest()
	}
	categories := make([]domain.MenuCategory, len(request.Categories))
	for i, category := range request.Categories {
		if !validRequiredText(category.ExternalID, 128) || !validRequiredText(category.Name, 200) || len(category.Items) > 1000 {
			return domain.MenuSnapshot{}, invalidRequest()
		}
		items := make([]domain.MenuItem, len(category.Items))
		for j, item := range category.Items {
			if !validRequiredText(item.ExternalID, 128) || !validRequiredText(item.Name, 200) || !validOptionalText(item.Description, 2000) || item.Price.Amount < 0 || !validCurrency(item.Price.Currency) {
				return domain.MenuSnapshot{}, invalidRequest()
			}
			items[j] = domain.MenuItem{ExternalID: item.ExternalID, Name: item.Name, Description: item.Description, Price: domain.Money{Amount: item.Price.Amount, Currency: item.Price.Currency}, IsAvailable: item.IsAvailable}
		}
		categories[i] = domain.MenuCategory{ExternalID: category.ExternalID, Name: category.Name, Items: items}
	}
	return domain.MenuSnapshot{Version: request.Version, Categories: categories}, nil
}

type availabilityUpdateRequest struct {
	IsAcceptingOrders bool   `json:"is_accepting_orders"`
	Reason            string `json:"reason"`
}

func (request availabilityUpdateRequest) domain() (domain.AvailabilityUpdate, error) {
	if !validOptionalText(request.Reason, 200) {
		return domain.AvailabilityUpdate{}, invalidRequest()
	}
	return domain.AvailabilityUpdate{IsAcceptingOrders: request.IsAcceptingOrders, Reason: request.Reason}, nil
}

type orderEventRequest struct {
	EventID    string    `json:"event_id"`
	Sequence   int64     `json:"sequence"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	Reason     string    `json:"reason"`
}

func (request orderEventRequest) domain() (domain.OrderEvent, error) {
	validType := request.Type == "order_preparing" || request.Type == "order_ready" || request.Type == "order_completed" || request.Type == "order_cancelled"
	if !httpx.IsUUID(request.EventID) || request.Sequence < 1 || !validType || request.OccurredAt.IsZero() || !validOptionalText(request.Reason, 500) {
		return domain.OrderEvent{}, invalidRequest()
	}
	return domain.OrderEvent{EventID: request.EventID, Sequence: request.Sequence, Type: request.Type, OccurredAt: request.OccurredAt, Reason: request.Reason}, nil
}

func invalidRequest() error {
	return apperror.New(apperror.KindValidation, "VALIDATION_FAILED", "request fields are invalid")
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
