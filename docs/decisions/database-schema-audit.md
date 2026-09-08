# Аудит схем PostgreSQL перед реализацией repository

Проверка выполнена после миграций platform `000004` и venue `000003`.

## Владение и удаление данных

- `venues` владеет категориями, позициями меню и заказами платформы.
- `orders` владеет snapshot-позициями, историей статусов и partner events.
- `venue_orders` владеет своими позициями заказа и outbox events.
- Все внешние ключи сохраняют PostgreSQL `NO ACTION`. Это намеренно: в MVP
  заведения и заказы не удаляются физически, меню снимается через `is_active`,
  а случайный hard delete родителя должен завершаться ошибкой, а не каскадно
  уничтожать историю.
- Если позже появится retention job, его порядок удаления и допустимые CASCADE
  правила потребуют отдельного решения и миграции.

## Ограничения

- Идентификаторы агрегатов — UUID; деньги — неотрицательные `bigint`, количество
  — положительный `integer`, версии и sequence — положительный `bigint`.
- Статусы ограничены CHECK-constraints; cancellation state отделён от основного
  состояния заказа.
- Уникальности закрывают external ID в пределах venue, idempotency key в
  пределах customer, tracking-token hash, platform order ID, event ID и
  `(order_id, sequence)`.
- Snapshot заказа продолжает ссылаться на menu item. Поскольку позиции меню
  soft-delete, ссылка сохраняет целостность и не мешает историческим заказам.

## Индексы

- Публичный каталог поддержан partial indexes доступных menu items и активных
  категорий.
- Polling/история заказа поддержаны индексами по customer/time, venue/status и
  `order_id` дочерних таблиц.
- Outbox workers используют partial indexes только для необработанных событий.
- В venue добавлен индекс строк заказа по `venue_order_id`; unique index
  `platform_order_id` обеспечивает идемпотентный поиск решения.

Повторная проверка query plans будет выполнена после появления реальных
repository queries: индекс без подтверждённого access pattern заранее не
добавляется.
