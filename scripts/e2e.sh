#!/bin/sh
set -eu

platform=${PLATFORM_URL:-http://localhost:8080}
venue=${VENUE_URL:-http://localhost:8090}
partner_token=${PARTNER_TOKEN:-dev-partner-token}
staff_token=${VENUE_STAFF_TOKEN:-dev-venue-staff-token}
venue_id=00000000-0000-4000-8000-000000000001
run_id=$(date +%s)

field() {
  printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p"
}

status_code() {
  curl -sS -o /tmp/avito-kitchen-e2e-response -w '%{http_code}' "$@"
}

expect_status() {
  expected=$1
  shift
  actual=$(status_code "$@")
  if [ "$actual" != "$expected" ]; then
    echo "expected HTTP $expected, got $actual: $(sed -n '1,8p' /tmp/avito-kitchen-e2e-response)" >&2
    return 1
  fi
}

create_order() {
  customer=$1
  key=$2
  curl -sS -X POST "$platform/api/v1/orders" \
    -H 'Content-Type: application/json' -H "Idempotency-Key: $key" \
    --data "{\"customer_ref\":\"$customer\",\"venue_id\":\"$venue_id\",\"menu_version\":1,\"items\":[{\"menu_item_id\":\"$item_id\",\"quantity\":1}],\"delivery\":{\"address\":{\"city\":\"Moscow\",\"street\":\"Test\",\"house\":\"1\"},\"phone\":\"+79990000000\"}}"
}

wait_order() {
  wait_order_id=$1
  wait_token=$2
  wait_expected=$3
  count=0
  while [ "$count" -lt 40 ]; do
    body=$(curl -sS "$platform/api/v1/orders/$wait_order_id" -H "X-Order-Token: $wait_token")
    [ "$(field "$body" status)" = "$wait_expected" ] && return 0
    count=$((count + 1))
    sleep 0.25
  done
  echo "order $wait_order_id did not reach $wait_expected" >&2
  return 1
}

wait_ready() {
  count=0
  while [ "$count" -lt 40 ]; do
    curl -fsS "$platform/readyz" >/dev/null 2>&1 && return 0
    count=$((count + 1))
    sleep 0.25
  done
  return 1
}

curl -fsS "$platform/readyz" >/dev/null
curl -fsS "$venue/readyz" >/dev/null
menu=$(curl -fsS "$platform/api/v1/venues/$venue_id/menu")
item_id=$(field "$menu" id)
[ -n "$item_id" ] || { echo "seed menu has no item id" >&2; exit 1; }

created=$(create_order "e2e-$run_id" "e2e-idempotency-$run_id")
order_id=$(field "$created" id)
token=$(field "$created" tracking_token)
[ -n "$order_id" ] && [ -n "$token" ] || { echo "checkout failed: $created" >&2; exit 1; }
repeated=$(create_order "e2e-$run_id" "e2e-idempotency-$run_id")
[ "$(field "$repeated" id)" = "$order_id" ]
[ "$(field "$repeated" tracking_token)" = "$token" ]
expect_status 404 "$platform/api/v1/orders/$order_id" -H 'X-Order-Token: invalid'
wait_order "$order_id" "$token" accepted

expect_status 202 -X POST "$platform/api/v1/orders/$order_id/cancel" -H "X-Order-Token: $token"
wait_order "$order_id" "$token" cancelled
expect_status 202 -X POST "$platform/api/v1/orders/$order_id/cancel" -H "X-Order-Token: $token"

expect_status 409 -X POST "$platform/api/v1/orders" -H 'Content-Type: application/json' -H "Idempotency-Key: e2e-unavailable-$run_id" --data "{\"customer_ref\":\"unavailable-$run_id\",\"venue_id\":\"$venue_id\",\"menu_version\":1,\"items\":[{\"menu_item_id\":\"00000000-0000-4000-8000-000000000099\",\"quantity\":1}],\"delivery\":{\"address\":{\"city\":\"Moscow\",\"street\":\"Test\",\"house\":\"1\"},\"phone\":\"+79990000000\"}}"
expect_status 409 -X POST "$platform/api/v1/orders" -H 'Content-Type: application/json' -H "Idempotency-Key: e2e-stale-$run_id" --data "{\"customer_ref\":\"stale-$run_id\",\"venue_id\":\"$venue_id\",\"menu_version\":999,\"items\":[{\"menu_item_id\":\"$item_id\",\"quantity\":1}],\"delivery\":{\"address\":{\"city\":\"Moscow\",\"street\":\"Test\",\"house\":\"1\"},\"phone\":\"+79990000000\"}}"

curl -fsS -X PATCH "$platform/partner/v1/availability" -H "Authorization: Bearer $partner_token" -H 'Content-Type: application/json' --data '{"is_accepting_orders":false,"reason":"e2e"}' >/dev/null
expect_status 409 -X POST "$platform/api/v1/orders" -H 'Content-Type: application/json' -H "Idempotency-Key: e2e-closed-$run_id" --data "{\"customer_ref\":\"closed-$run_id\",\"venue_id\":\"$venue_id\",\"menu_version\":1,\"items\":[{\"menu_item_id\":\"$item_id\",\"quantity\":1}],\"delivery\":{\"address\":{\"city\":\"Moscow\",\"street\":\"Test\",\"house\":\"1\"},\"phone\":\"+79990000000\"}}"
curl -fsS -X PATCH "$platform/partner/v1/availability" -H "Authorization: Bearer $partner_token" -H 'Content-Type: application/json' --data '{"is_accepting_orders":true}' >/dev/null

restart=$(create_order "e2e-restart-$run_id" "e2e-restart-key-$run_id")
restart_id=$(field "$restart" id)
restart_token=$(field "$restart" tracking_token)
docker compose restart avito-kitchen >/dev/null
wait_ready
wait_order "$restart_id" "$restart_token" accepted
restart_body=$(curl -sS "$platform/api/v1/orders/$restart_id" -H "X-Order-Token: $restart_token")
restart_external_id=$(field "$restart_body" external_order_id)
expect_status 200 -X POST "$venue/management/v1/orders/$restart_external_id/start-preparing" -H "Authorization: Bearer $staff_token"
wait_order "$restart_id" "$restart_token" preparing
expect_status 200 -X POST "$venue/management/v1/orders/$restart_external_id/mark-ready" -H "Authorization: Bearer $staff_token"
wait_order "$restart_id" "$restart_token" ready
expect_status 200 -X POST "$venue/management/v1/orders/$restart_external_id/complete" -H "Authorization: Bearer $staff_token"
wait_order "$restart_id" "$restart_token" completed

late=$(create_order "e2e-late-$run_id" "e2e-late-key-$run_id")
late_id=$(field "$late" id)
late_token=$(field "$late" tracking_token)
wait_order "$late_id" "$late_token" accepted
event_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
expect_status 409 -X POST "$platform/partner/v1/orders/$late_id/events" -H "Authorization: Bearer $partner_token" -H 'Content-Type: application/json' --data "{\"event_id\":\"$event_id\",\"sequence\":2,\"type\":\"order_ready\",\"occurred_at\":\"2026-09-08T00:00:00Z\"}"
late_body=$(curl -sS "$platform/api/v1/orders/$late_id" -H "X-Order-Token: $late_token")
external_id=$(field "$late_body" external_order_id)
expect_status 200 -X POST "$venue/management/v1/orders/$external_id/start-preparing" -H "Authorization: Bearer $staff_token"
wait_order "$late_id" "$late_token" preparing
expect_status 409 -X POST "$platform/api/v1/orders/$late_id/cancel" -H "X-Order-Token: $late_token"

order_count=$(docker compose exec -T postgres psql -U avito -d avito_kitchen -Atc "SELECT count(*) FROM orders WHERE customer_ref='e2e-$run_id'")
cancel_events=$(docker compose exec -T postgres psql -U avito -d venue -Atc "SELECT count(*) FROM venue_outbox vo JOIN venue_orders o ON o.id=vo.venue_order_id WHERE o.platform_order_id='$order_id' AND vo.event_type='order_cancelled'")
[ "$order_count" = 1 ]
[ "$cancel_events" = 1 ]

echo "E2E passed: full lifecycle, idempotency, tracking security, unavailable/stale/closed, worker restart, sequence conflict, successful and late cancellation"
