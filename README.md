# Avito.Kitchen

Готовый MVP API-платформы заказа еды и отдельно запускаемого тестового
заведения. Реализованы каталог, checkout, подтверждение, tracking, отмена,
операционный lifecycle и надёжный обмен через две transactional outbox.

Исходное задание находится в
[`backend-trainee-assignment-autumn-2026.md`](backend-trainee-assignment-autumn-2026.md),
а принятые решения и их компромиссы — в
[`docs/decisions/architecture-tradeoffs.md`](docs/decisions/architecture-tradeoffs.md).
Полный поэтапный план разработки и критерии готовности находятся в
[`ROADMAP.md`](ROADMAP.md).

Зафиксированные продуктовые контракты:

- [CJM пользователя](docs/scenarios/customer-journey.md);
- [CJM заведения](docs/scenarios/venue-journey.md);
- [машина состояний заказа](docs/decisions/order-state-machine.md);
- [каталог ошибок API](docs/api-error-codes.md).

## Архитектурный baseline

- `avito-kitchen` — публичное API, локальная read-модель меню и владелец заказа;
- `venue` — отдельный пример заведения и источник истины по возможности
  приготовить заказ;
- PostgreSQL — отдельные базы `avito_kitchen` и `venue` в одной локальной
  инсталляции;
- REST/JSON — внешние и межсервисные контракты;
- OpenAPI 3.1 и Swagger UI — документация и ручная проверка API.

В обоих Go-сервисах зависимости направлены от HTTP handler к service interface,
а затем к repository interface. Domain/application правила не зависят от HTTP,
Docker и конкретного PostgreSQL driver.

Контейнерная схема: [C4 diagram](docs/diagrams/c4-containers.svg). Модели данных:
[platform ER](docs/diagrams/platform-er.svg) и
[venue ER](docs/diagrams/venue-er.svg). Последовательности:
[checkout](docs/diagrams/checkout-sequence.svg) и
[cancellation race](docs/diagrams/cancellation-race.svg). Mermaid-исходники
лежат рядом с каждым SVG.

HTTP-граница уже использует отдельные DTO, строгий JSON decoder, единый
`application/problem+json`, request ID, безопасный access log и CORS allowlist.
Partner token однозначно связан с `PARTNER_VENUE_ID`; идентификатор заведения не
принимается из partner payload. Чтение и отмена заказа требуют непрозрачный
`X-Order-Token`: сервисный контракт получает только его SHA-256 hash, а не
исходный секрет.

## Локальный запуск

```bash
cp .env.example .env
docker compose up --build
```

После запуска доступны:

- Avito.Kitchen API: <http://localhost:8080>;
- пример venue service: <http://localhost:8090>;
- Swagger UI: <http://localhost:8081>;
- PostgreSQL: `localhost:5435`.

Проверка процессов:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8090/healthz
```

При старте `venue` синхронно публикует тестовое меню и доступность в platform.
После успешного запуска каталог можно проверить так:

```bash
curl 'http://localhost:8080/api/v1/venues?limit=20'
curl http://localhost:8080/api/v1/venues/00000000-0000-4000-8000-000000000001/menu
```

Список заведений использует непрозрачный cursor. Меню отдаётся одним ограниченным
версионированным snapshot, чтобы страницы разных версий нельзя было случайно
смешать на клиенте.

Отдельные `migrate-platform` и `migrate-venue` services применяют недостающие
версионные миграции при каждом запуске. Удалять Docker volume после добавления
миграции не требуется. Init-скрипт PostgreSQL только создаёт вторую базу.

Для проверки полного цикла up, повторного up и down/up на изолированных базах:

```bash
make migration-test
make e2e
make diagrams
```

Тестовые базы имеют фиксированные имена с суффиксом `_migration_test` и
удаляются скриптом после проверки. Основные локальные базы не затрагиваются.

## Проверки

```bash
make test
make vet
make lint
make openapi-lint
make compose-config
make migration-test
```

`make lint` использует закреплённый Docker image `golangci-lint`.
`make openapi-lint` использует зафиксированную версию Redocly CLI через `npx`.
`make e2e` является black-box сценарием поверх запущенного Compose и дополнительно
проверяет состояние обеих БД. `make diagrams` генерирует SVG из Mermaid sources.

## Бизнес-сценарии

- Клиент читает venue/menu snapshot, создаёт заказ с `Idempotency-Key` и получает
  capability-style tracking token.
- Platform сохраняет заказ и confirmation outbox атомарно; venue принимает
  окончательное решение по цене и stock.
- Оператор проводит принятый заказ через `preparing`, `ready`, `completed`.
- Отмена до первого dispatch выполняется локально. После `accepted` venue
  сериализует `cancel` с `start-preparing` и освобождает резерв ровно один раз.
- Сетевые повторы используют стабильные order/event IDs; sequence задаёт порядок,
  а timestamps служат только для аудита.

Исходники CJM: [customer journey](docs/diagrams/customer-cjm.mmd) и
[venue journey](docs/diagrams/venue-cjm.mmd).

## Наблюдаемость и fault injection

Оба сервиса публикуют `/metrics`; `/healthz` проверяет процесс, `/readyz` — БД и
версию миграций. Логи содержат request/order ID, attempt и результат без phone,
address и токенов. Диагностика очередей описана в [runbook](docs/runbook.md).

В `APP_ENV=test` venue поддерживает одноразовые fault-параметры
`VENUE_TEST_DECISION_DELAY` и `VENUE_TEST_DECISION_STATUS` (`429/500/503`). В
local/production запуск с ними запрещён.

## Ограничения MVP

- Один example venue и один partner credential; полноценная ротация/tenant
  registry не реализованы.
- Delivery price равен нулю, геозоны и ETA отсутствуют.
- Management list ограничен 100 строками без cursor navigation.
- Метрики хранятся в памяти процесса; pending queue и oldest age проверяются SQL
  из runbook. OpenTelemetry, broker и distributed rate limiting не добавлялись.
- Callback с постоянной конфигурационной ошибкой остаётся на capped retry до
  вмешательства оператора.
- Tracking token нельзя восстановить или перевыпустить в рамках MVP.

AI-assisted процесс разработки и критерии каждого шага зафиксированы в
[ROADMAP.md](ROADMAP.md), а причины решений — в
[architecture-tradeoffs.md](docs/decisions/architecture-tradeoffs.md).

## Структура

```text
api/                       OpenAPI-контракт
cmd/avito-kitchen/         main основного сервиса
cmd/venue/                 main тестового заведения
internal/platform/         слои основного сервиса
internal/venue/            слои заведения
migrations/platform/       миграции основной БД
migrations/venue/          миграции БД заведения
deploy/postgres/init/      локальная инициализация двух БД
deploy/postgres/baseline/  безопасный переход старого dev volume на versioning
docs/decisions/            журнал архитектурных компромиссов
```

## Текущий статус

HTTP-маршруты, partner/platform/staff token middleware и security foundation
заведены. В `production` конфигурация отклоняет пустые и `dev-*` токены. Platform
атомарно синхронизирует menu snapshot, сохраняет внутренние ID при upsert,
soft-delete отсутствующие позиции и обслуживает каталог независимо от доступности
venue. Venue владеет тестовым меню и публикует его через timeout-bound HTTP-клиент
с Bearer token и request ID. Checkout атомарно сохраняет `pending_confirmation`
заказ, неизменяемый snapshot цен, историю и outbox-команду; цена берётся только
из локального каталога, а повторы защищены scope `(customer_ref, Idempotency-Key)`.
Публичное чтение и отмена защищены отдельным невосстанавливаемым tracking token:
неизвестный заказ и неверный token дают одинаковый ответ. Venue принимает
integration-команду, под
строковыми блокировками проверяет version/price/stock, атомарно резервирует
остатки и возвращает устойчивое `accepted/rejected` решение; staff API позволяет
просматривать полученные заказы. Bounded platform worker забирает confirmation
jobs через `FOR UPDATE SKIP LOCKED`, вызывает venue вне транзакции и применяет
решение атомарно; lease восстанавливаются после падения, временные ошибки
повторяются с backoff/jitter, а исчерпанные попытки завершаются
`confirmation_expired`. Оператор venue может провести заказ через
`accepted → preparing → ready → completed`: transition и callback outbox
создаются атомарно, callback worker сохраняет порядок по `sequence`, а platform
дедуплицирует `event_id` и в одной транзакции обновляет заказ, inbox и history.
До первого dispatch pending-заказ отменяется локально; после `accepted`
платформа доставляет идемпотентную cancel-команду. Venue сериализует её с
`start-preparing`, освобождает stock ровно один раз и возвращает стабильное
решение для сетевых повторов.
