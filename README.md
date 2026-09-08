# Avito.Kitchen

MVP сервиса заказа еды. В репозитории находятся два Go-приложения:

- `avito-kitchen` хранит каталог и заказы, предоставляет публичное API;
- `venue` имитирует отдельное заведение, подтверждает заказы и ведёт остатки.

Сервисы запускаются вместе через Docker Compose, но используют разные базы
PostgreSQL. HTTP-контракты описаны в [OpenAPI](api/openapi.yaml).

## Две известные проблемы

### 1. Потерянный ответ может оставить резерв в venue

Venue атомарно сохраняет решение `accepted` и уменьшает остаток. Если после
commit HTTP-ответ несколько раз потеряется, platform исчерпает retries и
переведёт заказ в `confirmation_expired`. Для platform результат неизвестен,
но venue продолжит считать заказ принятым.

Варианты решения:

1. Продолжать reconciliation после пользовательского deadline. Platform может
   повторно запрашивать сохранённое идемпотентное решение, а venue — отправлять
   `order_decided` через свой outbox.
2. Разделить протокол на временный резерв и подтверждение резерва. Venue
   автоматически освобождает неподтверждённые резервы по TTL.

Рекомендуемый для MVP вариант — первый. Он использует уже существующие
idempotency и outbox, не вводит новый двухфазный протокол и закрывает основной
сценарий сбоя. При позднем `accepted` platform должна отправить компенсирующую
отмену. Второй вариант строже, но требует отдельной машины состояний резерва и
больше гонок.

Сейчас reconciliation после `confirmation_expired` не реализована. Этот статус
означает «platform не получила решение вовремя», а не «venue точно отклонило
заказ».

### 2. Management list не соответствует OpenAPI

`GET /management/v1/orders` сейчас игнорирует `status`, `cursor` и `limit`, хотя
они объявлены в OpenAPI. Repository возвращает последние 100 заказов и отдельно
загружает items каждого заказа. Это N+1 запрос и лишняя зависимость от размера
пула соединений.

Варианты решения:

1. Удалить параметры из OpenAPI и оставить фиксированные последние 100 заказов.
2. Реализовать keyset pagination по `(created_at, id)`, фильтр по status и
   загрузку items одним batch-запросом.

Я бы выбрал второй вариант: management API уже публично обещает пагинацию, а
batch-загрузка одновременно устраняет N+1. Первый вариант проще, но оставляет
API неудобным и потребует возвращаться к нему при первом росте данных.

## Архитектура

```text
Web client
    │
    ▼
avito-kitchen ───── HTTP ─────► venue
    │                             │
    ▼                             ▼
platform DB                   venue DB
    │                             │
    └─ confirmation outbox        └─ lifecycle callback outbox
```

Platform хранит локальную копию меню для быстрых чтений, но последнее слово по
цене и наличию остаётся за venue. Checkout сохраняет заказ и outbox-команду в
одной транзакции. Фоновый worker отправляет команду venue вне транзакции БД.

Внутри каждого приложения зависимости направлены так:

```text
HTTP handler → service → repository → PostgreSQL
                       └→ HTTP client другого сервиса
```

DTO HTTP-слоя, доменные модели и строки БД разделены. Service содержит проверки
и переходы состояний; repository отвечает за SQL и атомарность; `app` собирает
зависимости и управляет остановкой процессов.

Диаграммы:

- [C4 containers](docs/diagrams/c4-containers.svg);
- [platform ER](docs/diagrams/platform-er.svg);
- [venue ER](docs/diagrams/venue-er.svg);
- [checkout sequence](docs/diagrams/checkout-sequence.svg);
- [cancellation race](docs/diagrams/cancellation-race.svg);
- [customer CJM](docs/diagrams/customer-cjm.svg);
- [venue CJM](docs/diagrams/venue-cjm.svg).

Исходники диаграмм лежат рядом в формате Mermaid (`*.mmd`). SVG обновляются
командой `make diagrams`.

## Данные

Platform владеет:

- `venues`, `menu_categories`, `menu_items` — локальная read-модель меню;
- `orders`, `order_items` — заказ и неизменяемый snapshot позиций;
- `order_status_history` — история переходов;
- `partner_order_events` — дедупликация callback-событий;
- `outbox` — команды подтверждения и отмены.

Venue владеет:

- `venue_menu_items`, `venue_menu_versions`, `venue_settings` — меню, версия,
  остатки и доступность;
- `venue_orders`, `venue_order_items` — сохранённое решение и snapshot заказа;
- `venue_outbox` — события `preparing`, `ready`, `completed`, `cancelled`.

Деньги хранятся целым числом в минимальных единицах валюты. Время хранится в
UTC. Физическая схема задаётся миграциями в `migrations/platform` и
`migrations/venue`.

## Основные сценарии

1. Клиент получает заведения и версионированное меню.
2. `POST /api/v1/orders` с `Idempotency-Key` создаёт
   `pending_confirmation` заказ и возвращает tracking token.
3. Venue проверяет версию меню, цены и остатки, затем сохраняет стабильное
   `accepted` или `rejected` решение.
4. Клиент читает заказ с `X-Order-Token`.
5. Оператор проводит заказ через `accepted → preparing → ready → completed`.
6. До dispatch заказ отменяется локально; после `accepted` platform отправляет
   venue отдельную команду отмены.

Подробности: [customer journey](docs/scenarios/customer-journey.md),
[venue journey](docs/scenarios/venue-journey.md) и
[order state machine](docs/decisions/order-state-machine.md).

## Локальный запуск

```bash
cp .env.example .env
docker compose up --build
```

После запуска:

- platform API: <http://localhost:8080>;
- venue API: <http://localhost:8090>;
- Swagger UI: <http://localhost:8081>;
- PostgreSQL: `localhost:5435`.

Проверка:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8090/healthz
curl 'http://localhost:8080/api/v1/venues?limit=20'
curl http://localhost:8080/api/v1/venues/00000000-0000-4000-8000-000000000001/menu
```

Миграции применяются отдельными Compose services перед стартом приложений.
Удалять volume при добавлении новой миграции не требуется.

## Проверки

```bash
make test
make vet
make lint
make openapi-lint
make compose-config
make migration-test
make e2e
make audit
```

`make e2e` изменяет данные запущенного тестового окружения. Migration tests
создают отдельные временные базы.

## Ограничения MVP

- Один пример venue и один набор статических service tokens.
- Нет пользовательской аутентификации, оплаты, курьеров, ETA и геозон.
- Стоимость доставки равна нулю; поддерживается только `RUB`.
- Tracking token нельзя восстановить или перевыпустить.
- Rate limit и метрики локальны для процесса.
- Callback с постоянной ошибкой конфигурации требует вмешательства оператора.
- Две незакрытые технические проблемы описаны в начале README.

Другие принятые решения: [architecture trade-offs](docs/decisions/architecture-tradeoffs.md).
План, использованный при разработке: [ROADMAP.md](ROADMAP.md).

## Структура репозитория

```text
api/                  OpenAPI 3.1
cmd/                  точки входа двух приложений
internal/platform/    HTTP, application logic и SQL platform
internal/venue/       HTTP, application logic и SQL venue
internal/shared/      PostgreSQL, HTTP middleware, ошибки и UUID
migrations/           versioned migrations двух баз
deploy/postgres/      создание локальных баз и legacy baseline
docs/                  сценарии, решения, runbook и диаграммы
scripts/               E2E, migration test, audit и рендер диаграмм
```
