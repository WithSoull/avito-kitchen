# План разработки Avito.Kitchen

Этот файл — сокращённый plan, использованный при работе с AI-агентом. Он
сохраняется как контекст принятия решений, а не как журнал каждого изменённого
файла.

## Цель

Собрать запускаемый MVP из двух Go-сервисов:

- platform предоставляет каталог, checkout, tracking и отмену;
- example venue публикует меню, подтверждает заказы и ведёт lifecycle;
- состояние хранится в двух базах PostgreSQL;
- контракты, миграции и диаграммы находятся в том же репозитории.

## Ограничения для реализации

- Не добавлять web framework, ORM, broker и пользовательскую auth без сценария,
  который их требует.
- Не выполнять сетевые вызовы внутри транзакций БД.
- Деньги хранить целым числом, timestamps — в UTC.
- DTO, domain и persistence models не смешивать.
- Любую доставляемую повторно команду делать идемпотентной.
- Изменения API синхронизировать с OpenAPI, тестами и README.

## Этапы

### 1. Сценарии и контракты

- Описать customer и venue journey.
- Определить владельца меню, остатков и заказа.
- Зафиксировать состояния заказа, причины отказа и правила отмены.
- Спроектировать OpenAPI и физические схемы двух баз.

### 2. Каркас приложений

- Создать два бинарника в одном Go-модуле.
- Добавить typed config, HTTP server, graceful shutdown и PostgreSQL pool.
- Подготовить Dockerfile, Compose, migration services и health/readiness.
- Ввести общий формат ошибок, request ID и отдельные transport DTO.

### 3. Каталог и синхронизация меню

- Хранить на platform локальную read-модель меню.
- Публиковать полный versioned snapshot из venue.
- Обеспечить атомарный upsert, soft-delete и идемпотентный повтор версии.
- Читать version/categories/items из одного snapshot транзакции.

### 4. Checkout

- Валидировать запрос на HTTP- и service-границе.
- Пересчитать стоимость из локального меню.
- Атомарно сохранить заказ, позиции, историю и outbox-команду.
- Защитить повтор по `(customer_ref, Idempotency-Key)`.
- Возвращать tracking token, храня в БД только hash.

### 5. Решение venue и резерв

- Дедуплицировать команду по platform order ID.
- Повторно проверить version, price, availability и stock.
- Сериализовать конкурирующие изменения остатков.
- Сохранить `accepted` или `rejected` вместе с snapshot позиций.

### 6. Фоновая доставка

- Захватывать outbox jobs через lease и `SKIP LOCKED`.
- Делать HTTP после завершения claim-транзакции.
- Классифицировать retryable ошибки, использовать backoff и jitter.
- Защитить применение ответа через lock token.

### 7. Lifecycle и отмена

- Реализовать `accepted → preparing → ready → completed`.
- Создавать callback event в одной транзакции с переходом venue order.
- Дедуплицировать events и проверять `sequence` на platform.
- Сериализовать пользовательскую отмену с началом приготовления.

### 8. Проверка и документация

- Покрыть domain rules unit-тестами, SQL-инварианты integration-тестами.
- Добавить black-box E2E и безопасный fault injection для test environment.
- Проверить миграции вверх, повторный up и down/up.
- Сгенерировать CJM, C4, ER и sequence diagrams из Mermaid.
- Описать только реализованные гарантии и известные ограничения.

## Definition of Done

```bash
go test -race ./...
go vet ./...
make lint
make openapi-lint
make compose-config
make migration-test
make diagrams
make audit
make e2e
```

Команды считаются пройденными только после фактического успешного запуска.
E2E и migration tests должны использовать изолированное окружение и не удалять
рабочий volume разработчика.

## Результат ревью

Основной сценарий реализован, но работа не считается production-ready. Два
оставшихся решения вынесены в начало [README](README.md): reconciliation после
потерянного confirmation response и настоящая pagination management API без
N+1.
