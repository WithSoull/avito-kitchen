package httptransport

import (
	"strings"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/shared/apperror"
	"github.com/WithSoull/avito-kitchen/internal/shared/httpx"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

type orderCommandRequest struct {
	PlatformOrderID string             `json:"platform_order_id"`
	MenuVersion     int64              `json:"menu_version"`
	Items           []orderItemRequest `json:"items"`
}

type orderDecisionResponse struct {
	Result              string   `json:"result"`
	ExternalOrderID     string   `json:"external_order_id,omitempty"`
	AcceptedItemsAmount *int64   `json:"accepted_items_amount,omitempty"`
	Currency            string   `json:"currency,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	CurrentMenuVersion  *int64   `json:"current_menu_version,omitempty"`
	UnavailableItemIDs  []string `json:"unavailable_item_ids,omitempty"`
}

func decisionDTO(decision domain.OrderDecision) orderDecisionResponse {
	response := orderDecisionResponse{Result: decision.Result, ExternalOrderID: decision.ExternalOrderID,
		Currency: decision.Currency, Reason: decision.Reason, UnavailableItemIDs: decision.UnavailableItemIDs}
	if decision.Result == "accepted" {
		response.AcceptedItemsAmount = &decision.AcceptedItemsAmount
	} else if decision.CurrentMenuVersion > 0 {
		response.CurrentMenuVersion = &decision.CurrentMenuVersion
	}
	return response
}

type managedOrderPageResponse struct {
	Items []managedOrderResponse `json:"items"`
}

type managedOrderResponse struct {
	ID              string                     `json:"id"`
	PlatformOrderID string                     `json:"platform_order_id"`
	Status          string                     `json:"status"`
	Items           []managedOrderItemResponse `json:"items"`
	ItemsAmount     int64                      `json:"items_amount"`
	Currency        string                     `json:"currency"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
}

type managedOrderItemResponse struct {
	ExternalItemID string `json:"external_item_id"`
	Name           string `json:"name"`
	Quantity       int    `json:"quantity"`
	UnitPrice      int64  `json:"unit_price"`
	TotalPrice     int64  `json:"total_price"`
}

func managedOrderDTO(order domain.ManagedOrder) managedOrderResponse {
	response := managedOrderResponse{ID: order.ID, PlatformOrderID: order.PlatformOrderID, Status: order.Status,
		ItemsAmount: order.ItemsAmount, Currency: order.Currency, CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
		Items: make([]managedOrderItemResponse, len(order.Items))}
	for index, item := range order.Items {
		response.Items[index] = managedOrderItemResponse{ExternalItemID: item.ExternalItemID, Name: item.Name,
			Quantity: item.Quantity, UnitPrice: item.UnitPrice, TotalPrice: item.TotalPrice}
	}
	return response
}

type orderItemRequest struct {
	ExternalItemID    string `json:"external_item_id"`
	Name              string `json:"name"`
	Quantity          int    `json:"quantity"`
	ExpectedUnitPrice int64  `json:"expected_unit_price"`
}

func (request orderCommandRequest) domain() (domain.OrderCommand, error) {
	if !httpx.IsUUID(request.PlatformOrderID) || request.MenuVersion < 1 || len(request.Items) < 1 || len(request.Items) > 100 {
		return domain.OrderCommand{}, invalidRequest()
	}
	items := make([]domain.OrderItem, len(request.Items))
	for i, item := range request.Items {
		if strings.TrimSpace(item.ExternalItemID) == "" || strings.TrimSpace(item.Name) == "" || item.Quantity < 1 || item.Quantity > 100 || item.ExpectedUnitPrice < 0 {
			return domain.OrderCommand{}, invalidRequest()
		}
		items[i] = domain.OrderItem{ExternalItemID: item.ExternalItemID, Name: item.Name, Quantity: item.Quantity, ExpectedUnitPrice: item.ExpectedUnitPrice}
	}
	return domain.OrderCommand{PlatformOrderID: request.PlatformOrderID, MenuVersion: request.MenuVersion, Items: items}, nil
}

func invalidRequest() error {
	return apperror.New(apperror.KindValidation, "VALIDATION_FAILED", "request fields are invalid")
}
