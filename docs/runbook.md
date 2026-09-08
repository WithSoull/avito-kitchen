# Runbook

## Быстрая диагностика

1. Проверить `docker compose ps`, затем `/healthz` и `/readyz` обоих сервисов.
2. Найти `order_id` в логах platform и venue.
3. Проверить необработанные jobs:

```sql
SELECT event_type, count(*), min(created_at)
FROM outbox
WHERE processed_at IS NULL
GROUP BY event_type;

SELECT event_type, count(*), min(created_at)
FROM venue_outbox
WHERE processed_at IS NULL
GROUP BY event_type;
```

4. Сверить attempts, `last_error`, `available_at`, `locked_until` и
   `processed_at` конкретной job.

## Зависшая outbox job

- `locked_until` в прошлом означает, что job доступна следующему worker.
- Lease в будущем обычно означает выполняющийся HTTP-запрос. Сначала проверить
  request timeout и логи; не снимать lock вручную у работающего worker.
- После исправления временной зависимости retry произойдёт автоматически.
- Ручной replay допустим изменением `available_at`. Нельзя менять payload,
  aggregate/event ID или обнулять attempts без разбора причины.

## Недоступен другой сервис

- Проверить service token, Compose DNS, `/readyz` получателя и timeout клиента.
- Venue callbacks повторяются с тем же `event_id` и `sequence`.
- Cancellation после исчерпания попыток становится `failed`; пользователь может
  повторить её, только пока основной status остаётся `accepted`.
- Confirmation после deadline становится `confirmation_expired` и больше не
  сверяется автоматически.

Если platform показывает `confirmation_expired`, проверить `venue_orders` по
`platform_order_id` до повторного оформления. Venue могло сохранить `accepted`
до потери HTTP-ответа. Автоматической reconciliation этого случая в MVP нет.

## Безопасность

В логи не должны попадать Bearer/tracking tokens, телефон, адрес доставки и
полный payload заказа. При утечке service token заменить secret и перезапустить
оба сервиса. Tracking token восстановить нельзя: в БД хранится только SHA-256
hash.
