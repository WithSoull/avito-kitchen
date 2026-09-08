# Сценарий заведения

Диаграмма: [venue-cjm.svg](../diagrams/venue-cjm.svg).

## Участники

- Venue publisher вызывает partner API platform с `PARTNER_TOKEN`.
- Platform worker вызывает integration API venue с `PLATFORM_TOKEN`.
- Оператор вызывает management API venue с `VENUE_STAFF_TOKEN`.
- Пользователь не обращается к venue напрямую.

Статические tokens подходят только для одного демонстрационного заведения.

## Основной путь

### 1. Публикация меню

```http
PUT /partner/v1/menu
Authorization: Bearer <partner-token>
```

Venue отправляет полный snapshot с возрастающей числовой версией. Platform
атомарно обновляет read-модель, сохраняет внутренние ID существующих позиций и
помечает отсутствующие позиции неактивными.

Результаты:

- новая версия — `204`;
- точный повтор — `204`;
- другой payload с той же версией — `409 MENU_VERSION_CONFLICT`;
- меньшая версия — `409 STALE_MENU_VERSION`.

При запуске venue сначала публикует меню и availability, затем начинает слушать
HTTP. Если platform недоступна, venue не стартует.

### 2. Изменение доступности

```http
PATCH /partner/v1/availability
Authorization: Bearer <partner-token>
```

Закрытое venue не принимает новые checkout. Уже принятые заказы продолжают
жизненный цикл.

### 3. Решение по заказу

```http
POST /integration/v1/orders
Authorization: Bearer <platform-token>
Idempotency-Key: <platform-order-id>
```

Venue одной транзакцией:

1. Проверяет, нет ли сохранённого решения по `platform_order_id`.
2. Сверяет версию меню, цены, availability и stock.
3. Блокирует строки позиций и уменьшает конечный остаток.
4. Сохраняет `accepted` или `rejected` и snapshot позиций.

Повтор команды возвращает сохранённое решение и не резервирует stock второй
раз. Возможные причины отказа: `venue_closed`, `menu_changed`,
`items_unavailable`.

### 4. Работа оператора

```http
GET  /management/v1/orders
GET  /management/v1/orders/{order_id}
POST /management/v1/orders/{order_id}/start-preparing
POST /management/v1/orders/{order_id}/mark-ready
POST /management/v1/orders/{order_id}/complete
```

Переход состояния и callback event создаются одной транзакцией. Callback worker
доставляет события platform по `sequence`; событие с большим sequence не
обгоняет необработанное предыдущее событие того же заказа.

Текущий list endpoint имеет известное ограничение: параметры пагинации из
OpenAPI пока игнорируются, а items загружаются N+1 запросами. Решение описано в
[README](../../README.md#2-management-list-не-соответствует-openapi).

### 5. Отмена

Platform отправляет venue идемпотентную команду отмены. Venue блокирует строку
заказа, поэтому отмена и `start-preparing` не могут успешно примениться
одновременно.

- Для `accepted` заказ становится `cancelled`, stock возвращается один раз.
- Для `preparing`, `ready` или `completed` отмена отклоняется.
- Повтор возвращает сохранённое решение без повторного изменения stock.

## Сетевые сбои

- Platform confirmation worker повторяет временные ошибки с backoff до deadline
  или лимита попыток.
- Venue callback worker использует lease и повторяет доставку с тем же
  `event_id/sequence`.
- Потеря HTTP-ответа безопасна для повторного исполнения команды, но после
  `confirmation_expired` автоматической reconciliation пока нет.
