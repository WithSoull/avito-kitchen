# Runbook

## Быстрая диагностика

1. Проверить `/healthz`, затем `/readyz` обоих сервисов и `docker compose ps`.
2. Найти `order_id` в структурированных логах: confirmation/cancellation и
   callback workers пишут attempt, result и terminal-флаг без PII.
3. Проверить очереди:

```sql
SELECT event_type, count(*), min(created_at)
FROM outbox WHERE processed_at IS NULL GROUP BY event_type;

SELECT event_type, count(*), min(created_at)
FROM venue_outbox WHERE processed_at IS NULL GROUP BY event_type;
```

4. Проверить `/metrics`: HTTP requests/errors/суммарную latency и счётчики
   фоновых jobs. Для возраста очереди источником истины остаются SQL-запросы.

## Stuck outbox

- `locked_until` в прошлом безопасно подхватывается другим worker-ом.
- Lease в будущем означает выполняющийся HTTP-вызов; сначала сверить timeout и
  логи, не менять строку вручную.
- `last_error` показывает класс последней доставки, но не содержит upstream
  body или credentials.
- После исправления зависимости retry произойдёт автоматически. Ручной replay
  допустим только изменением `available_at`, без изменения payload/event ID.

## Недоступное venue/platform API

- Проверить service token, DNS Compose и readiness получателя.
- Confirmation имеет deadline и завершится `confirmation_expired`.
- Cancellation после исчерпания попыток станет `failed`; повтор пользователя
  создаёт новую команду только если основной статус ещё `accepted`.
- Venue callbacks повторяются с capped backoff и тем же `event_id/sequence`.

## Безопасность

Не писать в логи Bearer и tracking tokens, delivery address, phone и полный
payload. При подозрении на утечку заменить service secret и перезапустить оба
сервиса. Tracking token восстановить нельзя: в БД хранится только SHA-256 hash.
