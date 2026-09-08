package domain

import (
	"encoding/json"
	"time"
)

type Money struct {
	Amount   int64
	Currency string
}

type MenuItem struct {
	ExternalID  string
	Name        string
	Description string
	Price       Money
	IsAvailable bool
}

type MenuCategory struct {
	ExternalID string
	Name       string
	Items      []MenuItem
}

type MenuSnapshot struct {
	Version    int64
	Categories []MenuCategory
}

type Availability struct {
	IsAcceptingOrders bool
	Reason            string
}

type OrderCommand struct {
	PlatformOrderID string
	MenuVersion     int64
	Items           []OrderItem
}

type OrderItem struct {
	ExternalItemID    string
	Name              string
	Quantity          int
	ExpectedUnitPrice int64
}

type OrderDecision struct {
	Result              string
	ExternalOrderID     string
	Reason              string
	AcceptedItemsAmount int64
	Currency            string
	CurrentMenuVersion  int64
	UnavailableItemIDs  []string
}

type CancellationDecision struct {
	Result        string `json:"result"`
	CurrentStatus string `json:"current_status"`
	Reason        string `json:"reason,omitempty"`
}

type ManagedOrder struct {
	ID              string
	PlatformOrderID string
	Status          string
	Items           []ManagedOrderItem
	ItemsAmount     int64
	Currency        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ManagedOrderItem struct {
	ExternalItemID string
	Name           string
	Quantity       int
	UnitPrice      int64
	TotalPrice     int64
}

type OrderEvent struct {
	EventID    string    `json:"event_id"`
	Sequence   int64     `json:"sequence"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	Reason     string    `json:"reason,omitempty"`
}

type CallbackJob struct {
	ID              string
	PlatformOrderID string
	LockToken       string
	Payload         json.RawMessage
	Attempts        int
}
