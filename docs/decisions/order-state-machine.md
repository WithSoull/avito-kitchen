# Машина состояний заказа

## Решение

Основной `status` описывает только подтверждение и выполнение заказа. Состояние
запроса пользовательской отмены хранится отдельно в `cancellation_status`.

Это исключает неоднозначность состояния `cancellation_requested`: один такой
status не позволяет понять, был заказ принят, началось ли приготовление и что
показывать оператору заведения.

## Основные состояния

| Status | Значение | Terminal |
|---|---|---:|
| `pending_confirmation` | Заказ сохранён, ожидается решение заведения | нет |
| `accepted` | Заведение подтвердило цену, наличие и резерв | нет |
| `preparing` | Заведение начало приготовление | нет |
| `ready` | Заказ приготовлен | нет |
| `completed` | Заказ передан/завершён в рамках MVP | да |
| `rejected` | Заведение явно отклонило заказ | да |
| `confirmation_expired` | Platform не получила достоверное решение до deadline | да |
| `cancelled` | Отмена подтверждена либо заведение остановило заказ | да |

## Состояния пользовательской отмены

| Cancellation status | Значение |
|---|---|
| `none` | Отмена не запрашивалась |
| `requested` | Команда сохранена и ожидает решения заведения |
| `confirmed` | Заведение подтвердило отмену и освободило резерв |
| `rejected` | Отмена проиграла гонку началу приготовления |
| `failed` | Достоверный результат не получен после ограниченных retries |

`failed` консервативно сохраняет основной status. Платформа не заявляет, что
заказ отменён, пока venue service этого не подтвердил. Повторный запрос отмены
может перевести `failed → requested`, если основной status всё ещё `accepted`.

## Разрешённые переходы основного status

| From | To | Actor/source | Условие |
|---|---|---|---|
| — | `pending_confirmation` | platform checkout | Заказ, snapshot и outbox созданы одной транзакцией |
| `pending_confirmation` | `accepted` | platform confirmation worker | Venue вернул сохранённое решение `accepted` до deadline |
| `pending_confirmation` | `rejected` | platform confirmation worker | Venue вернул `rejected` |
| `pending_confirmation` | `confirmation_expired` | platform confirmation worker | Исчерпан confirmation deadline |
| `accepted` | `preparing` | venue event | Оператор выполнил `start-preparing` |
| `accepted` | `cancelled` | venue/platform | Подтверждена пользовательская или операционная отмена |
| `preparing` | `ready` | venue event | Оператор выполнил `mark-ready` |
| `preparing` | `cancelled` | venue event | Только операционная отмена заведением с причиной |
| `ready` | `completed` | venue event | Оператор выполнил `complete` |

Любой переход из `completed`, `rejected`, `confirmation_expired` или `cancelled`
запрещён. Поздний ответ не может воскресить terminal-заказ.

`confirmation_expired` является terminal только для текущей публичной машины
состояний. При потере HTTP-ответа удалённое решение venue может уже существовать;
автоматическая reconciliation и компенсация такого решения остаются известным
ограничением MVP.

## Разрешённые переходы cancellation status

| From | To | Условие |
|---|---|---|
| `none` | `requested` | Основной status равен `accepted` |
| `requested` | `confirmed` | Venue атомарно отменило `accepted` заказ; основной status становится `cancelled` |
| `requested` | `rejected` | Venue уже начало приготовление; основной status становится/остаётся `preparing` |
| `requested` | `failed` | Результат неизвестен после ограниченной политики повторов |
| `failed` | `requested` | Пользователь повторил отмену, основной status всё ещё `accepted` |

Повтор той же команды в `requested` не создаёт новую outbox-запись. Отмена в
`pending_confirmation`, `preparing`, `ready` или terminal status возвращает
`409 ORDER_CANCELLATION_NOT_ALLOWED`.

## Правила применения событий

- Platform является владельцем публичного status и единственным сервисом,
  который записывает его в своей БД.
- Venue является владельцем локального статуса приготовления и источником
  событий `order_preparing`, `order_ready`, `order_completed`,
  `order_cancelled`.
- Platform применяет venue event только к заказу этого authenticated venue.
- `event_id` глобально уникален и обеспечивает дедупликацию.
- `sequence` монотонен внутри одного заказа и начинается с 1.
- Точное повторное событие возвращает идемпотентный успех.
- Новый event с уже использованным sequence или gap возвращает
  `409 EVENT_SEQUENCE_CONFLICT`.
- `occurred_at` сохраняется для аудита, но не определяет порядок событий.

## Причины отклонения подтверждения

| Reason | Значение |
|---|---|
| `venue_closed` | Заведение не принимает новые заказы |
| `menu_changed` | Версия или цена не совпадает с локальным меню заведения |
| `items_unavailable` | Одной или нескольких позиций/количества нет |

Отклонение является сохранённым решением venue service. Повтор команды для того
же `platform_order_id` всегда возвращает первоначальное решение, даже если меню
или остатки позднее изменились.

## Инварианты

- Один заказ принадлежит ровно одному пользователю и одному заведению.
- `order_items` являются snapshot и не изменяются после создания.
- `total_amount = items_amount + delivery_amount` без переполнения `int64`.
- `accepted` означает, что venue service сохранило решение и резерв.
- `cancelled` после пользовательской отмены означает, что резерв освобождён.
- Текущее состояние, history и inbox/outbox record меняются атомарно.
- Сетевой вызов никогда не выполняется внутри транзакции БД.
