# Каталог ошибок API

Все ошибки API используют `Content-Type: application/problem+json` и стабильный
machine-readable `code`. Поле `detail` предназначено для человека и не должно
использоваться клиентом для ветвления логики.

## Формат

```json
{
  "type": "about:blank",
  "title": "Conflict",
  "status": 409,
  "code": "MENU_CHANGED",
  "detail": "Menu changed after it was loaded",
  "request_id": "8e203958-bc88-4dc7-9605-c7d28b6d643d"
}
```

Для ошибок полей дополнительно возвращается объект `fields`. В нём нет сырых
значений телефона, адреса, tokens и других чувствительных данных.

## Общие коды

| HTTP | Code | Когда используется | Retry клиента |
|---:|---|---|---|
| 400 | `INVALID_REQUEST` | Невалидный JSON, Content-Type, path/query/header format | После исправления |
| 400 | `INVALID_CONTENT_TYPE` | `Content-Type` отсутствует или отличается от `application/json` | После исправления |
| 413 | `REQUEST_TOO_LARGE` | JSON body превышает настроенный лимит | После уменьшения body |
| 401 | `UNAUTHORIZED` | Credential отсутствует или невалиден | После получения credential |
| 403 | `FORBIDDEN` | Actor аутентифицирован, но ресурс ему не принадлежит | Нет |
| 404 | `VENUE_NOT_FOUND` | Заведение не существует или недоступно для публичного просмотра | Нет |
| 404 | `MENU_NOT_FOUND` | У заведения ещё нет опубликованного меню | Позднее |
| 404 | `ORDER_NOT_FOUND` | Заказ отсутствует или неверен `X-Order-Token` | Нет |
| 422 | `VALIDATION_FAILED` | JSON корректен, но поля не проходят ограничения | После исправления |
| 429 | `TOO_MANY_REQUESTS` | Превышен rate limit | По `Retry-After` |
| 500 | `INTERNAL_ERROR` | Неожиданная внутренняя ошибка без раскрытия деталей | Ограниченно |
| 503 | `SERVICE_UNAVAILABLE` | Сервис не ready или критическая зависимость недоступна | Да |

## Каталог и меню

| HTTP | Code | Когда используется |
|---:|---|---|
| 409 | `VENUE_CLOSED` | Checkout выполняется для заведения, не принимающего заказы |
| 409 | `MENU_VERSION_CONFLICT` | Та же partner menu version пришла с другим payload |
| 409 | `STALE_MENU_VERSION` | Partner прислал версию старее сохранённой |
| 409 | `MENU_CHANGED` | Checkout или venue confirmation использует устаревшее меню/цену |
| 409 | `ITEMS_UNAVAILABLE` | Локальная ранняя проверка обнаружила недоступные позиции |

`MENU_CHANGED` и `ITEMS_UNAVAILABLE` также могут быть бизнес-результатом
асинхронного подтверждения. Тогда `POST /orders` уже вернул `202`, HTTP-ошибка не
возвращается, а заказ становится `rejected` с соответствующим reason.

## Заказы и идемпотентность

| HTTP | Code | Когда используется |
|---:|---|---|
| 409 | `IDEMPOTENCY_KEY_REUSED` | Ключ уже применялся с другим canonical request hash |
| 409 | `INVALID_ORDER_TRANSITION` | Команда или событие нарушает state machine |
| 409 | `ORDER_CANCELLATION_NOT_ALLOWED` | Пользователь отменяет заказ не в `accepted` |
| 409 | `EVENT_SEQUENCE_CONFLICT` | Callback имеет gap, повтор sequence с другим event или старый sequence |
| 409 | `ORDER_ALREADY_TERMINAL` | Команда пытается изменить terminal-заказ |

Точный повтор запроса с тем же idempotency/event ID не является ошибкой и
возвращает первоначальный успешный результат.

## Интеграционные ошибки

| HTTP | Code | Когда используется | Повторяет |
|---:|---|---|---|
| 400 | `INVALID_INTEGRATION_REQUEST` | Контрактный дефект исходящего запроса | Не автоматически |
| 401 | `UNAUTHORIZED` | Неверный service credential | После исправления конфигурации |
| 403 | `FORBIDDEN` | Credential не владеет venue/order | Нет |
| 409 | `INTEGRATION_STATE_CONFLICT` | Venue не может применить команду в текущем состоянии | После reconciliation |
| 502 | `UPSTREAM_BAD_RESPONSE` | Зависимость вернула невалидный/неожиданный ответ | Ограниченно |
| 503 | `VENUE_UNAVAILABLE` | Venue временно недоступно | Confirmation worker |

Для первичного подтверждения `502/503/timeout` не возвращаются пользователю из
`POST /orders`: заказ уже принят платформой и остаётся `pending_confirmation` до
успеха или `confirmation_expired`.

## Политика HTTP-кодов

- `200 OK` — чтение, повтор ранее выполненной команды или команда с телом
  результата.
- `202 Accepted` — платформа устойчиво сохранила асинхронную операцию.
- `204 No Content` — идемпотентное событие/snapshot принято без response body.
- `400` — запрос нельзя декодировать или его envelope некорректен.
- `401` — не установлена идентичность actor.
- `403` — actor известен, но действие запрещено для ресурса.
- `404` — ресурс не раскрывается клиенту. Неверный order token намеренно даёт
  тот же ответ.
- `409` — запрос корректен, но конфликтует с версией, идемпотентностью или
  текущим состоянием.
- `422` — поля корректного JSON нарушают декларативные ограничения.
- `429` — применяется только вместе с `Retry-After`.
- `500` — неизвестная ошибка; сырые SQL/network details не возвращаются.
- `502/503` — только синхронные межсервисные/ready ошибки, когда такой ответ
  действительно наблюдаем вызывающим клиентом.
