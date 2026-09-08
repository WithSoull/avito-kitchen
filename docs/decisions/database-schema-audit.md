# Физическая схема PostgreSQL

Актуальная схема складывается из миграций platform `000001`–`000009` и venue
`000001`–`000007`. Диаграммы показывают только физические внешние ключи:
[platform ER](../diagrams/platform-er.svg) и [venue ER](../diagrams/venue-er.svg).

## Владение и удаление

- `venues` связано с категориями, позициями меню и заказами platform.
- `orders` связано с позициями заказа, историей и partner events.
- `venue_orders` связано с позициями venue order и `venue_outbox`.
- Внешние ключи используют `NO ACTION`: заказы и venues в MVP физически не
  удаляются, а menu items выключаются через `is_active`.

`outbox.aggregate_id` в platform — логическая ссылка без FK. Поэтому outbox не
изображён дочерней таблицей `orders` на физической ER-диаграмме.
`venue_menu_versions` также не имеет FK к `venue_menu_items`: таблица хранит факт
публикации версии, а items представляют текущее состояние.

## Ограничения целостности

- Основные идентификаторы — UUID.
- Деньги — неотрицательный `bigint`, количество — положительный `integer`.
- Version и event sequence — положительный `bigint`.
- Статусы и типы outbox events ограничены `CHECK`.
- Уникальные ограничения защищают external IDs, checkout idempotency scope,
  tracking-token hash, platform order ID, event ID и `(order_id, sequence)`.
- `order_items` сохраняет FK на menu item и собственный snapshot цены и названия.

## Индексы

- Partial indexes обслуживают активное меню и необработанные outbox rows.
- Дочерние строки заказа индексированы по `order_id`/`venue_order_id`.
- Platform orders индексированы по customer/time и venue/status.
- Unique index `venue_orders.platform_order_id` используется для возврата
  сохранённого идемпотентного решения.

Management list пока не имеет полноценного keyset access pattern: он ограничен
100 строками и загружает items отдельными запросами. Это отмечено в README.
