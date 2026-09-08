# CJM заведения Avito.Kitchen

Документ описывает путь интегрированного заведения и оператора тестового venue
service. В MVP это отдельный процесс и контейнер со своей базой данных.

## Акторы и контракты

- **Venue integration worker** вызывает закрытое partner API платформы с
  `PARTNER_TOKEN`.
- **Avito.Kitchen worker** вызывает integration API заведения с отдельным
  `PLATFORM_TOKEN`.
- **Оператор тестового заведения** использует management API venue service с
  `VENUE_STAFF_TOKEN`.
- Пользователь никогда не вызывает venue service напрямую.

Статические tokens допустимы только для MVP. Каждый credential соответствует
конкретному actor, а partner principal — конкретному `venue_id`.

## Основной сценарий

### 1. Опубликовать меню

Venue service формирует полный snapshot и вызывает:

```http
PUT /partner/v1/menu
Authorization: Bearer <partner-token>
```

Snapshot содержит внешние стабильные ID категорий и позиций, монотонную
целочисленную версию `int64`, название, описание, цену и признак доступности.
Новую версию назначает venue service после каждого изменения локального меню.

Полный `PUT`, а не серия `PATCH`, выбран потому, что его легко повторить после
сбоя. Платформа атомарно заменяет read-модель, сохраняя внутренние ID прежних
позиций. Исчезнувшие позиции становятся неактивными, но не удаляются физически.

Результаты синхронизации:

- новая версия → `204 No Content`;
- точный повтор той же версии → идемпотентный `204`;
- та же версия с другим payload → `409 MENU_VERSION_CONFLICT`;
- устаревшая версия → `409 STALE_MENU_VERSION`.

### 2. Открыть или закрыть приём заказов

```http
PATCH /partner/v1/availability
Authorization: Bearer <partner-token>
```

При `is_accepting_orders=false` новые checkout отклоняются до постановки команды
в outbox. Изменение доступности не влияет на уже принятые заказы.

### 3. Получить команду подтверждения

Avito.Kitchen вызывает venue service:

```http
POST /integration/v1/orders
Authorization: Bearer <platform-token>
Idempotency-Key: <platform-order-id>
```

Venue service в одной транзакции:

1. Дедуплицирует команду по `platform_order_id`.
2. Проверяет локальную версию меню, цены и остатки.
3. Атомарно резервирует позиции при успехе.
4. Сохраняет неизменяемое `accepted` или `rejected` решение.
5. Возвращает то же решение при любом сетевом повторе.

### 4. Просмотреть очередь заказов

Оператор использует management API:

```http
GET /management/v1/orders?status=accepted
Authorization: Bearer <venue-staff-token>

GET /management/v1/orders/{order_id}
Authorization: Bearer <venue-staff-token>
```

API показывает только заказы данного venue service и не раскрывает технические
credentials или внутренний payload outbox.

### 5. Провести заказ по этапам

Оператор вызывает команды:

```http
POST /management/v1/orders/{order_id}/start-preparing
POST /management/v1/orders/{order_id}/mark-ready
POST /management/v1/orders/{order_id}/complete
Authorization: Bearer <venue-staff-token>
```

Каждая команда:

1. Проверяет локальный переход.
2. В одной транзакции меняет состояние и создаёт venue outbox event.
3. Callback worker отправляет событие платформе:

```http
POST /partner/v1/orders/{platform_order_id}/events
Authorization: Bearer <partner-token>
```

Событие содержит уникальный `event_id` и следующий монотонный `sequence`.

### 6. Обработать отмену

Платформа отправляет:

```http
POST /integration/v1/orders/{platform_order_id}/cancel
Authorization: Bearer <platform-token>
```

Если заказ ещё `accepted`, venue service освобождает резерв, переводит заказ в
`cancelled` и отвечает `result=cancelled`. Если оператор уже атомарно перевёл
заказ в `preparing`, venue service отвечает `result=rejected` и сообщает
текущий статус. Порядок определяется транзакцией БД, а не временем прихода HTTP.

## Сценарии отказа

| Ситуация | Поведение |
|---|---|
| Platform API недоступно при публикации меню | Venue повторяет полный snapshot с той же версией |
| Команда заказа доставлена повторно | Возвращается первоначальное сохранённое решение |
| Ответ `accepted` потерялся | Повтор не резервирует остаток второй раз |
| Callback потерял ответ | То же событие повторяется с тем же `event_id` и `sequence` |
| Platform получила более поздний sequence раньше | Возвращает `409`, venue повторяет пропущенное событие |
| Остаток забрали конкурентные заказы | БД принимает только допустимое количество, остальные решения — `rejected` |
| Меню на платформе устарело | Venue отклоняет заказ с `menu_changed`, не меняя цену молча |
| Неверный credential | `401`, бизнес-операция не выполняется |
| Credential другого заведения | `403`, чужие ресурсы не раскрываются |
| Оператор повторил management-команду | Возвращается текущее состояние без повторного события |
| Отмена и начало приготовления пришли одновременно | Результат сериализуется транзакцией: `cancelled` или `preparing` |

## Критерии готовности CJM заведения

- Меню публикуется повторяемым snapshot-запросом.
- Venue service принимает окончательное решение по цене и наличию.
- Ни один HTTP retry не создаёт второй заказ, резерв или callback event.
- Оператор не может перескочить или откатить состояние заказа.
- Platform и venue восстанавливают обмен после временной недоступности.
