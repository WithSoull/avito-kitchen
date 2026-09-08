package domain

import (
	"encoding/json"
	"time"
)

type Money struct {
	Amount   int64
	Currency string
}

type Venue struct {
	ID                 string
	Name               string
	Description        string
	Address            string
	Currency           string
	MinimumOrderAmount int64
	IsAcceptingOrders  bool
}

type VenueListQuery struct {
	Search  string
	AfterID string
	Limit   int
}

type VenuePage struct {
	Items      []Venue
	NextCursor string
}

type Menu struct {
	VenueID    string
	Version    int64
	UpdatedAt  time.Time
	Categories []MenuCategory
}

type MenuItem struct {
	ID          string
	ExternalID  string
	Name        string
	Description string
	Price       Money
	IsAvailable bool
}

type CreateOrderRequest struct {
	CustomerRef string
	VenueID     string
	MenuVersion int64
	Items       []OrderItemRequest
	Delivery    Delivery
}

type Delivery struct {
	Address Address `json:"address"`
	Phone   string  `json:"phone"`
	Comment string  `json:"comment"`
}

type Address struct {
	City      string `json:"city"`
	Street    string `json:"street"`
	House     string `json:"house"`
	Apartment string `json:"apartment"`
}

type OrderItemRequest struct {
	MenuItemID string
	Quantity   int
}

type Order struct {
	ID                 string
	CustomerRef        string
	VenueID            string
	ExternalOrderID    string
	Status             string
	CancellationStatus string
	RejectionReason    string
	Items              []OrderItem
	ItemsAmount        int64
	DeliveryAmount     int64
	TotalAmount        int64
	Currency           string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type OrderItem struct {
	MenuItemID     string
	ExternalItemID string
	Name           string
	Quantity       int
	UnitPrice      int64
	TotalPrice     int64
}

type CreateOrderResult struct {
	Order         Order
	TrackingToken string
}

type CreateOrderCommand struct {
	OrderID              string
	IdempotencyKey       string
	RequestHash          string
	TrackingTokenHash    string
	ConfirmationDeadline time.Time
	Request              CreateOrderRequest
}

type MenuSnapshot struct {
	Version    int64
	Categories []MenuCategory
}

type MenuCategory struct {
	ExternalID string
	Name       string
	Items      []MenuItem
}

type AvailabilityUpdate struct {
	IsAcceptingOrders bool
	Reason            string
}

type OrderEvent struct {
	EventID    string
	Sequence   int64
	Type       string
	OccurredAt time.Time
	Reason     string
}

type OutboxJob struct {
	ID                   string
	OrderID              string
	EventType            string
	LockToken            string
	Payload              json.RawMessage
	Attempts             int
	ConfirmationDeadline time.Time
	ExpectedItemsAmount  int64
	Currency             string
}

type VenueCancellationDecision struct {
	Result        string `json:"result"`
	CurrentStatus string `json:"current_status"`
	Reason        string `json:"reason"`
}

type VenueOrderDecision struct {
	Result              string   `json:"result"`
	ExternalOrderID     string   `json:"external_order_id"`
	AcceptedItemsAmount int64    `json:"accepted_items_amount"`
	Currency            string   `json:"currency"`
	Reason              string   `json:"reason"`
	CurrentMenuVersion  int64    `json:"current_menu_version"`
	UnavailableItemIDs  []string `json:"unavailable_item_ids"`
}
