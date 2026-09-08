# Пользовательский сценарий

Диаграмма: [customer-cjm.svg](../diagrams/customer-cjm.svg).

## Границы MVP

- Web-клиент и пользовательская аутентификация не реализованы.
- Корзина хранится на клиенте и содержит позиции одного venue.
- `customer_ref` связывает заказ с внешним пользователем, но не подтверждает его
  личность.
- Чтение и отмена заказа защищены отдельным `tracking_token`.
- Поддерживается одна тестовая зона доставки и валюта `RUB`.

## Основной путь

### 1. Выбор заведения

```http
GET /api/v1/venues?query=пицца&limit=20
```

Platform возвращает страницу заведений и opaque cursor. Доступность показывает,
принимает ли venue новые заказы в момент чтения.

### 2. Получение меню

```http
GET /api/v1/venues/{venue_id}/menu
```

Ответ содержит полный menu snapshot и его версию. Это локальная копия platform;
она может отставать от venue. Клиент отправляет в checkout ID позиций и
количество, но не является источником цены.

### 3. Создание заказа

```http
POST /api/v1/orders
Idempotency-Key: <client-generated-key>
Content-Type: application/json
```

Platform проверяет запрос и текущую локальную версию меню, пересчитывает сумму и
одной транзакцией сохраняет:

- заказ в `pending_confirmation`;
- snapshot позиций и цен;
- запись истории;
- outbox-команду для venue.

Ответ `202 Accepted` означает, что заказ сохранён platform, но ещё не принят
venue. Вместе с `order_id` клиент получает `tracking_token`. Platform хранит
только hash токена; восстановить потерянный токен нельзя.

Повтор с тем же customer, idempotency key и телом возвращает тот же заказ.
Использование ключа с другим телом возвращает `409 IDEMPOTENCY_KEY_REUSED`.

### 4. Ожидание решения

```http
GET /api/v1/orders/{order_id}
X-Order-Token: <tracking_token>
```

Клиент опрашивает заказ и получает одно из состояний:

- `pending_confirmation` — решение ещё не записано platform;
- `accepted` — venue подтвердило цену и резерв;
- `rejected` — venue сохранило отказ;
- `confirmation_expired` — platform не получила достоверный ответ до deadline.

Последнее состояние не доказывает, что venue не выполнило команду. Этот случай
описан как известная проблема в [README](../../README.md#1-потерянный-ответ-может-оставить-резерв-в-venue).

### 5. Приготовление

После принятия venue присылает упорядоченные события:

```text
accepted → preparing → ready → completed
```

Platform применяет событие только для соответствующего venue и ожидаемого
`sequence`.

### 6. Отмена

До первого claim confirmation job platform может отменить заказ локально. После
того как команда отправлена, локальная отмена небезопасна: venue уже могло
зарезервировать остаток.

Для принятого заказа клиент вызывает:

```http
POST /api/v1/orders/{order_id}/cancel
X-Order-Token: <tracking_token>
```

Основной status остаётся `accepted`, пока venue не вернёт результат. Отдельный
`cancellation_status` принимает значения:

- `requested` — команда ожидает доставки;
- `confirmed` — venue освободило резерв, основной status стал `cancelled`;
- `rejected` — приготовление уже началось;
- `failed` — достоверный результат не получен.

## Отказы, которые должен обработать клиент

| Ситуация | Результат |
|---|---|
| Venue закрыто при checkout | `409 VENUE_CLOSED` |
| Локальная версия меню изменилась | `409 MENU_CHANGED` |
| Цена или версия отличаются в venue | заказ становится `rejected: menu_changed` |
| Остатка не хватает | заказ становится `rejected: items_unavailable` |
| Повторён тот же checkout | возвращается первый заказ |
| Tracking token неверен | `404 ORDER_NOT_FOUND`, как для неизвестного ID |
| Отмена проиграла `start-preparing` | `409 ORDER_CANCELLATION_NOT_ALLOWED` или `cancellation_status=rejected` |
