# Машина состояний заказа

Platform хранит основной `status` заказа и отдельный `cancellation_status`.
Разделение нужно потому, что во время удалённой отмены заказ всё ещё остаётся
`accepted` и может одновременно перейти в `preparing`.

## Основной status

| Status | Значение | Terminal |
|---|---|---:|
| `pending_confirmation` | Заказ сохранён, решение venue неизвестно | нет |
| `accepted` | Venue подтвердило цену, наличие и резерв | нет |
| `preparing` | Приготовление началось | нет |
| `ready` | Заказ готов | нет |
| `completed` | Заказ завершён | да |
| `rejected` | Venue явно отклонило заказ | да |
| `confirmation_expired` | Platform не получила решение до deadline | да для текущей реализации |
| `cancelled` | Отмена подтверждена или заказ остановлен venue | да |

Разрешённые переходы:

| From | To | Источник |
|---|---|---|
| — | `pending_confirmation` | checkout platform |
| `pending_confirmation` | `accepted` | решение venue |
| `pending_confirmation` | `rejected` | решение venue |
| `pending_confirmation` | `confirmation_expired` | confirmation worker |
| `accepted` | `preparing` | callback venue |
| `accepted` | `cancelled` | подтверждённая отмена |
| `preparing` | `ready` | callback venue |
| `preparing` | `cancelled` | операционная отмена venue |
| `ready` | `completed` | callback venue |

Поздний HTTP-ответ не меняет terminal-заказ. Из-за этого
`confirmation_expired` может скрывать уже сохранённый `accepted` в venue. До
реализации reconciliation этот случай требует ручной сверки.

## Cancellation status

| Status | Значение |
|---|---|
| `none` | Отмена не запрашивалась |
| `requested` | Команда отмены ожидает решения venue |
| `confirmed` | Venue отменило заказ и освободило резерв |
| `rejected` | Отмена проиграла началу приготовления |
| `failed` | Достоверный результат не получен |

Разрешённые переходы:

| From | To | Условие |
|---|---|---|
| `none` | `requested` | Основной status равен `accepted` |
| `requested` | `confirmed` | Venue отменило `accepted` заказ |
| `requested` | `rejected` | Venue уже начало приготовление |
| `requested` | `failed` | Исчерпана политика retries |
| `failed` | `requested` | Пользователь повторил отмену, заказ ещё `accepted` |

Точный повтор не создаёт новую outbox job. Pending-заказ отменяется локально
только до первого claim confirmation job. В остальных недопустимых состояниях
API возвращает `409 ORDER_CANCELLATION_NOT_ALLOWED`.

## Callback events

- Venue создаёт `order_preparing`, `order_ready`, `order_completed` или
  `order_cancelled` в одной транзакции с локальным переходом.
- `event_id` дедуплицирует доставку.
- `sequence` начинается с 1 и определяет порядок событий одного заказа.
- Platform отклоняет gap и конфликтующий повтор sequence.
- `occurred_at` используется для аудита, но не для определения порядка.

## Причины отказа venue

| Reason | Значение |
|---|---|
| `venue_closed` | Venue не принимает новые заказы |
| `menu_changed` | Версия или цена не совпадает |
| `items_unavailable` | Позиции нет, она выключена или не хватает stock |

Решение сохраняется по `platform_order_id`. Повтор команды возвращает исходный
результат, даже если меню и остатки после него изменились.

## Инварианты

- Один заказ относится к одному customer и одному venue.
- `order_items` и цены не меняются после checkout.
- `total_amount = items_amount + delivery_amount` без переполнения `int64`.
- `accepted` означает сохранённое venue-решение и уменьшенный stock.
- Подтверждённая пользовательская отмена освобождает stock один раз.
- State, history и inbox/outbox меняются атомарно в пределах своей базы.
- HTTP-вызовы не выполняются внутри транзакций БД.
