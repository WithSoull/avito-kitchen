# Ошибки API

HTTP-ошибки возвращаются как `application/problem+json`:

```json
{
  "type": "about:blank",
  "title": "Conflict",
  "status": 409,
  "code": "MENU_CHANGED",
  "detail": "menu changed after it was loaded",
  "request_id": "8e203958-bc88-4dc7-9605-c7d28b6d643d"
}
```

Клиент может ветвить логику по `status` и `code`. `detail` предназначен для
человека. Сырые SQL и сетевые ошибки наружу не возвращаются. Структурированного
поля `fields` реализация сейчас не формирует.

## Общие коды

| HTTP | Code | Когда возвращается |
|---:|---|---|
| 400 | `INVALID_REQUEST` | Некорректный JSON, path или обязательный header |
| 400 | `INVALID_CONTENT_TYPE` | Body передан не как `application/json` |
| 401 | `UNAUTHORIZED` | Service или staff token отсутствует либо неверен |
| 404 | `VENUE_NOT_FOUND` | Venue не найдено |
| 404 | `MENU_NOT_FOUND` | Меню ещё не опубликовано |
| 404 | `ORDER_NOT_FOUND` | Заказ не найден или неверен `X-Order-Token` |
| 413 | `REQUEST_TOO_LARGE` | Body превышает настроенный предел |
| 422 | `VALIDATION_FAILED` | JSON разобран, но значения полей недопустимы |
| 429 | `TOO_MANY_REQUESTS` | Сработал checkout rate/concurrency limit |
| 500 | `INTERNAL_ERROR` | Неожиданная внутренняя ошибка |
| 503 | `SERVICE_UNAVAILABLE` | Readiness check не может обратиться к БД |

Checkout limiter сейчас не вычисляет время следующей попытки и не устанавливает
`Retry-After`.

## Каталог и checkout

| HTTP | Code | Когда возвращается |
|---:|---|---|
| 409 | `VENUE_CLOSED` | Venue не принимает новые заказы |
| 409 | `MENU_VERSION_CONFLICT` | Та же версия меню пришла с другим payload |
| 409 | `STALE_MENU_VERSION` | Получена версия старее сохранённой |
| 409 | `MENU_CHANGED` | Версия или цена изменилась после чтения меню |
| 409 | `ITEMS_UNAVAILABLE` | Позиция неактивна или недоступна |
| 409 | `IDEMPOTENCY_KEY_REUSED` | Idempotency key повторён с другим запросом |

`menu_changed` и `items_unavailable`, полученные от venue после `202 Accepted`,
записываются как причина `rejected`, а не возвращаются отдельной HTTP-ошибкой
первичного checkout.

## Состояния заказа и callback

| HTTP | Code | Когда возвращается |
|---:|---|---|
| 403 | `ORDER_VENUE_MISMATCH` | Partner пытается изменить заказ другого venue |
| 409 | `INVALID_ORDER_TRANSITION` | Переход запрещён текущим состоянием |
| 409 | `ORDER_CANCELLATION_NOT_ALLOWED` | Заказ нельзя отменить в текущей фазе |
| 409 | `EVENT_ID_CONFLICT` | Event ID уже использован для другого события |
| 409 | `EVENT_SEQUENCE_CONFLICT` | Sequence повторён с другим событием или содержит gap |

Точный повтор checkout, venue decision, cancellation или callback возвращает
первоначальный результат и не считается ошибкой.

Сетевые ошибки между platform и venue обрабатываются workers и напрямую
пользователю не возвращаются. Для confirmation они приводят к retry, а после
исчерпания текущей политики — к `confirmation_expired`.
