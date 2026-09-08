package venueclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
	shareduuid "github.com/WithSoull/avito-kitchen/internal/shared/uuid"
)

type Error struct {
	StatusCode int
	Retryable  bool
	Cause      error
}

func (err *Error) Error() string {
	if err.StatusCode != 0 {
		return fmt.Sprintf("venue returned HTTP %d", err.StatusCode)
	}
	return fmt.Sprintf("call venue: %v", err.Cause)
}

func IsRetryable(err error) bool {
	var clientErr *Error
	return errors.As(err, &clientErr) && clientErr.Retryable
}

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
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || token == "" || timeout <= 0 {
		return nil, fmt.Errorf("invalid venue client configuration")
	}
	client := &Client{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{Timeout: timeout}}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

func (client *Client) Decide(ctx context.Context, job domain.OutboxJob) (domain.VenueOrderDecision, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/integration/v1/orders", bytes.NewReader(job.Payload))
	if err != nil {
		return domain.VenueOrderDecision{}, &Error{Cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", job.OrderID)
	requestID, err := shareduuid.New()
	if err != nil {
		return domain.VenueOrderDecision{}, err
	}
	request.Header.Set("X-Request-ID", requestID)
	response, err := client.http.Do(request)
	if err != nil {
		return domain.VenueOrderDecision{}, &Error{Retryable: true, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return domain.VenueOrderDecision{}, &Error{StatusCode: response.StatusCode, Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var decision domain.VenueOrderDecision
	if err := decoder.Decode(&decision); err != nil {
		return domain.VenueOrderDecision{}, &Error{Cause: fmt.Errorf("decode venue decision: %w", err)}
	}
	validAccepted := decision.Result == "accepted" && shareduuid.IsValid(decision.ExternalOrderID) && decision.Currency == job.Currency && decision.AcceptedItemsAmount == job.ExpectedItemsAmount
	validRejected := decision.Result == "rejected" && (decision.Reason == "venue_closed" || decision.Reason == "menu_changed" || decision.Reason == "items_unavailable")
	if !validAccepted && !validRejected {
		return domain.VenueOrderDecision{}, &Error{Cause: fmt.Errorf("invalid venue decision")}
	}
	return decision, nil
}

func (client *Client) Cancel(ctx context.Context, job domain.OutboxJob) (domain.VenueCancellationDecision, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL+"/integration/v1/orders/"+job.OrderID+"/cancel", http.NoBody)
	if err != nil {
		return domain.VenueCancellationDecision{}, &Error{Cause: err}
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	request.Header.Set("Idempotency-Key", job.OrderID)
	requestID, err := shareduuid.New()
	if err != nil {
		return domain.VenueCancellationDecision{}, err
	}
	request.Header.Set("X-Request-ID", requestID)
	response, err := client.http.Do(request)
	if err != nil {
		return domain.VenueCancellationDecision{}, &Error{Retryable: true, Cause: err}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return domain.VenueCancellationDecision{}, &Error{StatusCode: response.StatusCode, Retryable: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var decision domain.VenueCancellationDecision
	if err := decoder.Decode(&decision); err != nil {
		return domain.VenueCancellationDecision{}, &Error{Cause: fmt.Errorf("decode venue cancellation: %w", err)}
	}
	validCancelled := decision.Result == "cancelled" && decision.CurrentStatus == "cancelled" && decision.Reason == ""
	validRejected := decision.Result == "rejected" && decision.Reason == "cancellation_not_allowed"
	if !validCancelled && !validRejected {
		return domain.VenueCancellationDecision{}, &Error{Cause: fmt.Errorf("invalid venue cancellation decision")}
	}
	return decision, nil
}
