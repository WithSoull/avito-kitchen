package venueclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/WithSoull/avito-kitchen/internal/platform/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestDecideHeadersResponseAndRetryClassification(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer platform-token" || request.Header.Get("Idempotency-Key") != "00000000-0000-4000-8000-000000000001" || request.Header.Get("X-Request-ID") == "" {
			t.Fatalf("headers = %#v", request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"result":"accepted","external_order_id":"00000000-0000-4000-8000-000000000002","accepted_items_amount":100,"currency":"RUB"}`)), Header: make(http.Header)}, nil
	})
	client, err := New("http://venue", "platform-token", time.Second, WithRoundTripper(transport))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Decide(context.Background(), domain.OutboxJob{OrderID: "00000000-0000-4000-8000-000000000001", Payload: []byte(`{}`), ExpectedItemsAmount: 100, Currency: "RUB"})
	if err != nil || decision.Result != "accepted" {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}

	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("unavailable")), Header: make(http.Header)}, nil
	})
	_, err = client.Decide(context.Background(), domain.OutboxJob{OrderID: "00000000-0000-4000-8000-000000000001", Payload: []byte(`{}`)})
	if !IsRetryable(err) {
		t.Fatalf("503 must be retryable: %v", err)
	}
	client.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("bad request")), Header: make(http.Header)}, nil
	})
	_, err = client.Decide(context.Background(), domain.OutboxJob{OrderID: "00000000-0000-4000-8000-000000000001", Payload: []byte(`{}`)})
	if IsRetryable(err) {
		t.Fatalf("400 must be terminal: %v", err)
	}
}

func TestCancelIsIdempotentlyAddressedByPlatformOrderID(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/integration/v1/orders/00000000-0000-4000-8000-000000000001/cancel" ||
			request.Header.Get("Authorization") != "Bearer platform-token" ||
			request.Header.Get("Idempotency-Key") != "00000000-0000-4000-8000-000000000001" {
			t.Fatalf("request path=%s headers=%#v", request.URL.Path, request.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"result":"cancelled","current_status":"cancelled"}`)), Header: make(http.Header)}, nil
	})
	client, err := New("http://venue", "platform-token", time.Second, WithRoundTripper(transport))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := client.Cancel(context.Background(), domain.OutboxJob{OrderID: "00000000-0000-4000-8000-000000000001"})
	if err != nil || decision.Result != "cancelled" {
		t.Fatalf("decision=%#v err=%v", decision, err)
	}
}
