package platformclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
	"github.com/WithSoull/avito-kitchen/internal/venue/domain"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type Option func(*Client)

func WithRoundTripper(transport http.RoundTripper) Option {
	return func(client *Client) { client.http.Transport = transport }
}

func New(baseURL, token string, timeout time.Duration, options ...Option) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("invalid platform base URL")
	}
	if token == "" || timeout <= 0 {
		return nil, fmt.Errorf("invalid platform client configuration")
	}
	client := &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: timeout}}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

func (client *Client) ReplaceMenu(ctx context.Context, snapshot domain.MenuSnapshot) error {
	request := replaceMenuRequest{Version: snapshot.Version, Categories: make([]menuCategoryRequest, len(snapshot.Categories))}
	for categoryIndex, category := range snapshot.Categories {
		mapped := menuCategoryRequest{ExternalID: category.ExternalID, Name: category.Name, Items: make([]menuItemRequest, len(category.Items))}
		for itemIndex, item := range category.Items {
			mapped.Items[itemIndex] = menuItemRequest{ExternalID: item.ExternalID, Name: item.Name, Description: item.Description, Price: moneyRequest{Amount: item.Price.Amount, Currency: item.Price.Currency}, IsAvailable: item.IsAvailable}
		}
		request.Categories[categoryIndex] = mapped
	}
	return client.send(ctx, http.MethodPut, "/partner/v1/menu", request)
}

func (client *Client) UpdateAvailability(ctx context.Context, availability domain.Availability) error {
	return client.send(ctx, http.MethodPatch, "/partner/v1/availability", availabilityRequest{IsAcceptingOrders: availability.IsAcceptingOrders, Reason: availability.Reason})
}

func (client *Client) SendOrderEvent(ctx context.Context, job domain.CallbackJob) error {
	return client.sendBody(ctx, http.MethodPost, "/partner/v1/orders/"+job.PlatformOrderID+"/events", job.Payload)
}

func (client *Client) send(ctx context.Context, method, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode platform request: %w", err)
	}
	return client.sendBody(ctx, method, path, body)
}

func (client *Client) sendBody(ctx context.Context, method, path string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create platform request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	requestID, err := shareduuid.New()
	if err != nil {
		return err
	}
	request.Header.Set("X-Request-ID", requestID)
	response, err := client.http.Do(request)
	if err != nil {
		return fmt.Errorf("call platform: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("platform returned HTTP %d", response.StatusCode)
	}
	return nil
}

type replaceMenuRequest struct {
	Version    int64                 `json:"version"`
	Categories []menuCategoryRequest `json:"categories"`
}
type menuCategoryRequest struct {
	ExternalID string            `json:"external_id"`
	Name       string            `json:"name"`
	Items      []menuItemRequest `json:"items"`
}
type menuItemRequest struct {
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
type availabilityRequest struct {
	IsAcceptingOrders bool   `json:"is_accepting_orders"`
	Reason            string `json:"reason"`
}
