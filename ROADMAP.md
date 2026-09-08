# Roadmap разработки Avito.Kitchen

Этот документ — единый исполняемый план проекта для разработчика и AI-агента.
Он задаёт порядок работ, границы этапов и проверяемые критерии готовности.

Исходные требования находятся в
[`backend-trainee-assignment-autumn-2026.md`](backend-trainee-assignment-autumn-2026.md),
архитектурные компромиссы — в
[`docs/decisions/architecture-tradeoffs.md`](docs/decisions/architecture-tradeoffs.md),
правила работы с репозиторием — в [`AGENTS.md`](AGENTS.md).

## Как агент должен работать по roadmap

1. Выполнять только один этап одновременно. Не начинать следующий, пока не
   выполнены критерии выхода текущего.
2. Перед этапом перечитать относящиеся к нему OpenAPI, миграции, решения и
   существующий код. Не заменять уже принятые решения молчаливыми допущениями.
3. Реализовывать вертикальными срезами: контракт → handler → service →
   repository → миграция → тесты → документация.
4. Не добавлять функции из следующих этапов «заодно». Если без изменения
   архитектуры продолжить нельзя, сначала дополнить журнал решений.
5. Не менять OpenAPI, state machine или схему БД только в коде. Все связанные
   артефакты обновляются в одном этапе.
6. После каждого этапа запускать его quality gate и записывать результат в
   секцию «Журнал выполнения» в конце этого файла.
7. Менять статус этапа на `DONE` только после фактического прохождения всех
   проверок. Допустимые статусы: `TODO`, `IN PROGRESS`, `BLOCKED`, `DONE`.
8. Не коммитить, не публиковать и не удалять пользовательские данные без
   отдельного запроса пользователя.

## Приоритеты

- **MUST** — необходимо для выполнения задания и демонстрации основных CJM.
- **SHOULD** — заметно повышает качество решения, выполняется после рабочего
  end-to-end сценария.
- **STRETCH** — только после полного выполнения MUST и SHOULD.

## Итоговый Definition of Done

Проект завершён, когда одновременно выполняются условия:

- `docker compose up --build` поднимает основной сервис, пример заведения,
  PostgreSQL, миграции и Swagger UI одной командой;
- пользователь может получить каталог, открыть меню, создать заказ и проверить
  его итоговый статус;
- заведение синхронизирует меню, подтверждает или отклоняет заказ и отправляет
  дальнейшие статусы;
- повторные HTTP-запросы не создают дубли заказов или событий;
- недоступное заведение, устаревшее меню, отсутствующий товар и потерянный ответ
  имеют определённый и протестированный результат;
- OpenAPI совпадает с фактическими запросами, ответами и кодами ошибок;
- миграции применяются к пустым базам и обновляют уже созданные базы;
- unit, integration и end-to-end тесты проходят;
- линтер и статический анализ проходят без ошибок;
- CJM, C4 и ER-диаграммы генерируются из хранящегося в репозитории кода;
- README объясняет запуск, архитектуру, модель данных, сценарии, ограничения и
  компромиссы MVP.

---

## Этап 0. Инфраструктурный каркас

**Приоритет:** MUST
**Статус:** DONE

Уже реализовано:

- два Go-бинарника `avito-kitchen` и `venue`;
- слои handler/service/repository с заглушками;
- OpenAPI 3.1 и Swagger UI;
- Docker Compose с двумя сервисами и PostgreSQL;
- отдельные базы и начальные схемы платформы и заведения;
- partner/platform Bearer middleware;
- graceful shutdown, health-checks, Makefile и golangci-lint;
- базовые HTTP-тесты;
- документ с архитектурными trade-offs.

**Quality gate:**

```bash
go test ./...
go vet ./...
golangci-lint run
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

---

## Этап 1. Зафиксировать продуктовые сценарии и контракт

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 0

### Работа

- Описать основной CJM пользователя:
  поиск заведения → просмотр меню → корзина → checkout → ожидание подтверждения
  → отслеживание заказа.
- Описать негативные пользовательские сценарии:
  заведение закрыто, позиция закончилась, меню изменилось, заведение не ответило,
  повторный checkout, отмена одновременно с подтверждением.
- Описать CJM заведения:
  публикация меню → изменение доступности → получение заказа → решение →
  приготовление → завершение.
- Зафиксировать границы MVP: одна тестовая зона доставки, одна валюта `RUB`, одна
  корзина/заказ на одно заведение, без оплаты и курьеров.
- Зафиксировать полную таблицу разрешённых переходов заказа и акторов перехода.
- Составить каталог стабильных machine-readable кодов ошибок.
- Проверить все request/response schema в OpenAPI, включая обязательность полей,
  ограничения длины, количества, форматы UUID/времени/телефона и nullable-поля.
- Добавить в OpenAPI management API тестового заведения для просмотра заказов и
  команд `start-preparing`, `mark-ready`, `complete`.
- Определить контракт отмены и разрешённые состояния. Рекомендуемая политика:
  отмена до `preparing`, с подтверждением заведением.
- Зафиксировать HTTP-коды: `201/202`, `204`, `400`, `401`, `403`, `404`, `409`,
  `422`, `429`, `500`, `502/503` и случаи их применения.

### Артефакты

- `docs/scenarios/customer-journey.md`;
- `docs/scenarios/venue-journey.md`;
- `docs/decisions/order-state-machine.md`;
- обновлённый `api/openapi.yaml`;
- `docs/api-error-codes.md`.

### Критерий выхода

- Для каждого MUST-сценария существует последовательность API-вызовов.
- У каждого endpoint определены успешные и бизнес-ошибочные ответы.
- Не осталось произвольного `PATCH status`; переходы выражены командами или
  событиями.
- OpenAPI проходит синтаксическую и семантическую валидацию.

---

## Этап 2. Общий HTTP и security foundation

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 1

### Работа

- Ввести отдельные transport DTO и явный mapping DTO ↔ domain. Не использовать
  структуры БД или domain напрямую в JSON API.
- Реализовать строгий JSON decoder: лимит тела, проверка `Content-Type`, запрет
  неизвестных полей, запрет нескольких JSON-значений.
- Добавить единый typed error contract и mapping domain/application errors в
  `application/problem+json`.
- Добавить request ID/correlation ID и включать его в логи и problem response.
- Добавить access log с методом, route, status, duration и request ID без
  телефона, адреса, токенов и полного request body.
- Реализовать CORS для явно заданного списка origins, включая локальный Swagger.
- Обработать `OPTIONS` preflight.
- Заменить строковую аутентификацию партнёра на `PartnerPrincipal{VenueID}` в
  context. Заведение не может выбирать `venue_id` в теле запроса.
- Разделить `401` — credential отсутствует/невалиден и `403` — credential валиден,
  но ресурс принадлежит другому заведению.
- Реализовать генерацию и hash tracking token через `crypto/rand`; фактическое
  хранение hash будет добавлено вместе с repository заказа на этапе 5.
- Добавить middleware/guard contract для `X-Order-Token` на чтение и отмену
  заказа; подключить его к данным на этапе 9.
- Ввести `APP_ENV`; запретить пустые и development-токены вне `local/test`.
- Настроить timeout и максимальные размеры заголовков HTTP server.

### Тесты

- table-driven тесты JSON decoder и error mapping;
- missing/malformed/wrong Bearer token → `401`;
- чужое заведение → `403`;
- отсутствующий/невалидный order token не открывает заказ;
- CORS разрешает только настроенные origins;
- логи не содержат secrets и PII.

### Критерий выхода

- Все handlers используют единый транспортный foundation.
- Swagger `Try it out` работает с локально разрешённого origin.
- Неавторизованный запрос не доходит до service/repository.

---

## Этап 3. PostgreSQL, транзакции и нормальные миграции

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 2

### Работа

- Подключить `pgx/v5` и настроить отдельный `pgxpool` в каждом сервисе.
- Ввести небольшие DB/transaction interfaces и transaction manager; не
  прокидывать `pgxpool.Pool` в business service.
- Добавить обязательные connection/query timeouts и ограничение pool size.
- Зарегистрировать закрытие pools в graceful shutdown.
- Сделать `/readyz` реальной проверкой готовности БД; `/healthz` оставить
  проверкой процесса.
- Заменить выполнение миграций только при создании Docker volume на отдельные
  одноразовые Compose migration services. Init-скрипт должен только создавать
  базы, миграции должны применяться при каждом запуске безопасно и идемпотентно.
- Проверить ограничения, индексы, внешние ключи и правила `ON DELETE` текущей
  схемы до написания repository.
- Добавлять последующие изменения только новыми миграциями, не редактируя уже
  принятую `000001` после фиксации этапа.
- Подготовить test helpers для PostgreSQL integration tests.

### Тесты

- применение всех up-миграций к пустой БД;
- повторный запуск migration service;
- применение down/up в изолированной тестовой БД;
- readiness при доступной и недоступной БД;
- корректное завершение сервиса при отмене context.

### Критерий выхода

- Приложения реально подключаются к своим базам.
- Compose не требует удаления volume после добавления новой миграции.
- Ошибка миграции не позволяет приложению стартовать как ready.

---

## Этап 4. Вертикальный срез каталога и меню

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 3

### Работа платформы

- Реализовать repository для `venues`, `menu_categories`, `menu_items`.
- Реализовать атомарную полную синхронизацию menu snapshot:
  upsert категорий/позиций, сохранение внутренних ID, soft-delete отсутствующих
  позиций и обновление `menu_version` в одной транзакции.
- Реализовать политику версий:
  новый version принимается; точный повтор идемпотентен; та же версия с другим
  payload и устаревшая версия возвращают `409`.
- Для сравнения одинаковой версии хранить детерминированный hash snapshot.
- Реализовать изменение `is_accepting_orders` только для authenticated venue.
- Реализовать `GET venues`, `GET venue`, `GET menu` с cursor pagination.
- Не показывать неактивные позиции, но сохранять их для исторических заказов.
- Добавить seed карточки тестового заведения отдельной миграцией или явной
  bootstrap-командой.

### Работа venue service

- Добавить собственное меню/остатки тестового заведения.
- Реализовать HTTP-клиент partner API с timeout, Bearer token и request ID.
- Реализовать команду или startup-job публикации полного menu snapshot.

### Тесты

- unit-тесты правил версий и mapping;
- repository integration tests для upsert и soft-delete;
- чужой partner token не меняет меню;
- повтор snapshot не создаёт дубликаты;
- каталог читается при остановленном venue service;
- контрактные тесты ответов против OpenAPI.

### Критерий выхода

После `docker compose up` тестовое заведение публикует меню, а пользователь
видит его через публичный API платформы.

---

## Этап 5. Создание заказа внутри платформы

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 4

### Работа

- Реализовать validation checkout:
  непустой `customer_ref`, одно заведение, уникальные позиции, положительное
  количество, активное заведение, минимальная сумма, валидный адрес и телефон.
- Не принимать цену от web-клиента. Получать цену из локальной read-модели.
- Проверять переданный `menu_version`, но считать это ранней проверкой, а не
  окончательной гарантией наличия.
- Рассчитать суммы с overflow-check; деньги остаются `int64` в копейках.
- В одной транзакции создать:
  `orders(pending_confirmation)`, snapshot `order_items`, начальную запись
  истории и outbox-команду подтверждения.
- Реализовать клиентскую идемпотентность:
  scope `(customer_ref, Idempotency-Key)`, canonical request hash, возврат
  прежнего результата при точном повторе и `409` при повторе ключа с другим
  телом.
- Сгенерировать order ID и tracking token до транзакции, сохранить только hash
  токена, вернуть открытый токен ровно в ответе создания.
- Возвращать `202 Accepted` и `Location`.

### Тесты

- unit-тесты всех бизнес-инвариантов и расчёта денег;
- repository integration test атомарного создания заказа;
- rollback не оставляет частичный заказ или outbox;
- двойной запрос создаёт один заказ;
- повтор ключа с другим payload → `409`;
- изменение позиции после заказа не меняет snapshot заказа.

### Критерий выхода

Пользователь может создать устойчиво сохранённый `pending_confirmation` заказ;
сетевого вызова заведения в транзакции нет.

---

## Этап 6. Приём заказа тестовым заведением

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 5

### Работа

- Реализовать входной `POST /integration/v1/orders`.
- Дедуплицировать по `platform_order_id`; повтор всегда возвращает первое
  сохранённое решение.
- В одной транзакции проверить:
  venue открыт, menu version допустим, позиции существуют, цены совпадают,
  количества доступны.
- При успехе атомарно зарезервировать остатки и создать `accepted` заказ.
- При отказе сохранить стабильное решение и reason:
  `venue_closed`, `menu_changed`, `items_unavailable`.
- Не создавать повторную резервацию при сетевом retry.
- Реализовать management API заведения: список и получение заказов. Изменение
  статусов будет добавлено на этапе 8.

### Тесты

- конкурентные заказы за последнюю позицию: принимается только допустимое число;
- повтор одинаковой команды возвращает то же решение;
- потеря ответа и retry не уменьшают остаток дважды;
- неверный platform token → `401`;
- price/menu conflict возвращает документированный reason.

### Критерий выхода

Venue service выдаёт устойчивое `accepted/rejected` решение и является
источником истины для резерва остатков.

---

## Этап 7. Надёжная доставка подтверждения через outbox worker

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 6

### Работа

- Реализовать repository выдачи outbox jobs с конкурентно-безопасным claim:
  `FOR UPDATE SKIP LOCKED` плюс lease/`locked_until`, без удержания транзакции во
  время HTTP-вызова.
- Реализовать bounded worker pool и graceful shutdown worker-ов.
- Реализовать HTTP-клиент venue service с connection/request timeout,
  `PLATFORM_TOKEN`, request ID и `Idempotency-Key = order_id`.
- Разделить ошибки на retryable и terminal.
- Реализовать exponential backoff с jitter и максимум попыток/общий confirmation
  deadline.
- При `accepted/rejected` атомарно обновить заказ и history, затем завершить job.
- При исчерпании deadline перевести заказ в `confirmation_expired`.
- Не позволять позднему ответу воскресить terminal-заказ.
- Добавить восстановление зависших lease после падения worker-а.

### Тесты

- unit-тесты retry classification и backoff;
- несколько worker-ов не теряют jobs;
- дубли доставки безопасны;
- падение после HTTP-ответа до отметки `processed` безопасно;
- временный `503` заканчивается успешным retry;
- постоянная недоступность заканчивается `confirmation_expired`;
- graceful shutdown не начинает новые jobs и корректно завершает текущие.

### Критерий выхода

Созданный заказ автоматически и надёжно переходит в `accepted`, `rejected` или
`confirmation_expired`, включая сетевые сбои и рестарт платформы.

---

## Этап 8. Жизненный цикл заказа и callback-события

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 7

### Работа venue service

- Реализовать management-команды `start-preparing`, `mark-ready`, `complete`.
- Проверять локальную state machine и сохранять transition + venue outbox event
  в одной транзакции.
- Реализовать callback worker к platform partner API.
- Каждому событию назначать стабильный UUID и монотонный `sequence` на заказ.

### Работа платформы

- Реализовать inbox/deduplication по `event_id`.
- Проверять, что authenticated venue владеет заказом.
- Проверять `sequence` и разрешённость перехода.
- Дубликат события считать успешным; gap/out-of-order обрабатывать по
  зафиксированной политике, не сортировать по клиентскому времени.
- Сохранять текущее состояние, event и status history атомарно.

### Тесты

- полный путь `accepted → preparing → ready → completed`;
- duplicate callback;
- callback чужого заведения;
- out-of-order sequence;
- недопустимый переход назад;
- потеря callback-ответа и повторная доставка.

### Критерий выхода

Оператор тестового заведения может провести заказ до `completed`, а платформа
имеет ту же последовательную историю.

---

## Этап 9. Получение заказа и безопасная отмена

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 8

### Работа

- Реализовать `GET /api/v1/orders/{id}` по паре `order_id + X-Order-Token`.
- Возвращать snapshot позиций и текущий статус без внутренних outbox/inbox
  полей.
- Использовать одинаковый внешний ответ для неизвестного заказа и неверного
  tracking token, чтобы не облегчать перебор ID.
- Реализовать идемпотентный запрос отмены.
- Если заказ ещё не отправлен заведению, завершить отмену локально и пометить
  confirmation job terminal.
- Если заказ уже принят, оставить основной status `accepted`, выставить
  `cancellation_status=requested` и создать outbox-команду заведению.
- Venue service атомарно отменяет допустимый заказ и освобождает резерв либо
  возвращает конфликт, если приготовление уже началось.
- Разрешить гонку accept/cancel через сериализацию на стороне venue, а не через
  предположение о порядке сетевых запросов.
- Отразить окончательный результат callback-событием.

### Тесты

- неверный tracking token не раскрывает заказ;
- повтор отмены безопасен;
- cancel до dispatch;
- cancel одновременно с accept;
- cancel после `preparing` получает документированный конфликт;
- освобождение остатка выполняется ровно один раз.

### Критерий выхода

Пользователь безопасно читает и отменяет только свой конкретный заказ без
системы пользовательской аутентификации.

---

## Этап 10. End-to-end сценарии и fault injection

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 9

### Работа

- Добавить black-box тесты, работающие только через HTTP и Compose.
- Подготовить детерминированные seed-данные и `make demo`/`make e2e`.
- Покрыть сценарии:
  happy path, item unavailable, changed price/menu, venue closed, double click,
  venue timeout, restart platform worker, duplicate/out-of-order callback,
  successful cancellation, rejected late cancellation.
- Добавить управляемый test mode venue service для задержки/ошибки ответа. Он
  должен включаться только в `APP_ENV=test`.
- Проверять не только status code, но и состояние обеих БД и отсутствие дублей.

### Критерий выхода

Все MUST-сценарии воспроизводятся одной командой и подтверждают заявленные
гарантии системы.

---

## Этап 11. Наблюдаемость и эксплуатационная устойчивость

**Приоритет:** SHOULD
**Статус:** DONE
**Зависит от:** этапа 10

### Работа

- Структурированные логи с `request_id`, `order_id`, `venue_id`, attempt и
  transition без PII.
- Метрики HTTP latency/error rate, pending outbox, oldest outbox age,
  confirmation result/duration, retries и invalid transitions.
- Отдельные readiness checks БД и критических background workers.
- Ограничение concurrency, body size и rate limit для публичного checkout.
- Настраиваемые timeout/backoff/deadline через typed config с валидацией.
- Runbook для stuck outbox, недоступного venue и повторной обработки.
- Опционально подключить OpenTelemetry tracing только после метрик и логов.

### Критерий выхода

По логам и метрикам можно ответить: где заказ, сколько раз его отправляли, почему
он отклонён и есть ли накопившаяся очередь.

---

## Этап 12. Документация и code-generated диаграммы

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапа 10; может выполняться параллельно с этапом 11

### Работа

- Создать CJM пользователя и заведения в PlantUML/Mermaid source.
- Создать C4 Context/Container и при необходимости Component diagram.
- Создать ER-диаграммы обеих баз.
- Добавить sequence diagrams для checkout/confirmation и cancellation race.
- Добавить Make-цель генерации SVG/PNG из diagram sources.
- Дополнить README:
  бизнес-сценарии, архитектура, API, модель данных, state machine, безопасность,
  идемпотентность, eventual consistency, запуск, тесты и ограничения.
- Перенести полезные формулировки из trade-offs в README, сохранив отдельный
  журнал решений.
- Приложить AI plan: ссылку на этот roadmap и краткое описание процесса.

### Критерий выхода

Человек, не видевший код, может по README поднять проект, пройти demo и объяснить
главные архитектурные решения.

---

## Этап 13. Финальная приёмка

**Приоритет:** MUST
**Статус:** DONE
**Зависит от:** этапов 10 и 12; этап 11 проверяется, если был выполнен

### Проверки

- Запустить все quality gates на чистом checkout.
- В отдельном acceptance Compose project проверить миграции с нуля, не удаляя
  рабочий volume разработчика.
- Проверить upgrade уже существующей БД без удаления volume.
- Сверить каждый OpenAPI endpoint с router и реальными кодами ответов.
- Проверить Swagger `Try it out` для public, partner и integration API.
- Просканировать репозиторий на secrets, `.env`, дампы БД и лишние артефакты.
- Проверить Docker images: non-root user, graceful shutdown, health/readiness,
  фиксированные версии базовых images.
- Провести ручной smoke-test всех CJM.
- Проверить `git diff` на незавершённые заглушки, случайные TODO и ручные правки
  сгенерированного кода.
- Обновить раздел ограничений: заявлять только фактически реализованные
  гарантии.

### Финальные команды

```bash
go test -race ./...
go vet ./...
golangci-lint run
docker compose config --quiet
docker compose down
docker compose -p avito-kitchen-acceptance up -d --build
make e2e
docker compose -p avito-kitchen-acceptance ps
```

Удаление acceptance volume выполняется только после явного подтверждения
пользователя либо автоматически в изолированной CI-среде.

### Критерий выхода

Выполнен общий Definition of Done, отсутствуют незадокументированные падения
проверок, а проект готов к передаче проверяющему.

---

## STRETCH после готового MVP

Эти задачи не должны задерживать выполнение MUST:

- генерация transport DTO/server/client из OpenAPI;
- SSE/WebSocket вместо polling статуса;
- геополигоны доставки и расчёт ETA;
- модификаторы блюд;
- несколько валют;
- отдельная PostgreSQL-инсталляция на сервис;
- mTLS или OAuth2 client credentials для партнёров;
- Kafka/CDC для масштабирования outbox;
- полноценная пользовательская аутентификация;
- нагрузочное тестирование и профилирование;
- deployment manifests и CI/CD.

## Журнал выполнения

После каждого этапа агент добавляет запись:

```text
YYYY-MM-DD — Этап N — DONE
Изменено: <ключевые артефакты>
Проверено: <точные команды и результат>
Ограничения/следующий шаг: <если есть>
```

Текущая запись:

```text
2026-09-08 — Этап 0 — DONE
Изменено: каркас двух сервисов, OpenAPI, Docker Compose, Swagger UI, миграции,
линтер, тесты и архитектурные trade-offs.
Проверено: go test ./..., go vet ./..., golangci-lint run,
docker compose config --quiet, docker compose up -d --build; все контейнеры
healthy, обе схемы БД созданы.
Ограничения/следующий шаг: начать этап 1 и окончательно зафиксировать CJM,
state machine, management API заведения и каталог ошибок.

2026-09-08 — Этап 1 — DONE
Изменено: CJM пользователя и заведения, state machine, каталог ошибок,
management API заведения, отдельный staff token, числовая версия меню и
отдельное состояние пользовательской отмены.
Проверено: go test ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid without warnings), docker compose config --quiet, сборка и
health Compose; новые management-маршруты дают 401 без token и 501 после
успешной аутентификации; up/down миграции проверены в изолированных базах.
Ограничения/следующий шаг: бизнес-операции остаются заглушками; начать этап 2 —
общий HTTP и security foundation.

2026-09-08 — Этап 2 — DONE
Изменено: отдельные transport DTO и DTO→domain mapping, strict JSON decoder,
typed application errors и problem details, request ID, безопасный access log,
CORS/preflight, PartnerPrincipal с привязанным venue ID, guard для
X-Order-Token, crypto/rand token generation и SHA-256 hashing, APP_ENV и
ограничения HTTP server.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), docker compose config --quiet, docker compose up -d
--build (все контейнеры healthy); smoke-проверки подтвердили CORS 204, strict
JSON 400, missing order token 404, wrong Bearer 401 и authenticated stubs 501.
Ограничения/следующий шаг: hash order token пока передаётся только через
service/repository contract, фактическое хранение появится с order repository;
начать этап 3 — PostgreSQL pools, транзакции, readiness и migration services.

2026-09-08 — Этап 3 — DONE
Изменено: pgxpool 5.10 для каждого сервиса, DBTX/Transactor interfaces,
connection pool limits и PostgreSQL statement timeout, migration-aware
readiness, lifecycle pools, versioned migration services, безопасный baseline
старого dev volume, query indexes, schema audit и PostgreSQL test helpers.
Go toolchain обновлён до 1.25; lint закреплён в Docker image
golangci-lint 2.12.2.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), docker compose config --quiet и docker compose up -d
--build; migration services завершились с exit 0, повторный up дал no change,
down/up проверен в изолированных базах; integration tests подтвердили
commit/rollback, statement timeout и graceful shutdown обоих сервисов;
readiness даёт 503 без PostgreSQL и восстанавливается до 200 после запуска.
Ограничения/следующий шаг: SQL repositories пока остаются заглушками; начать
этап 4 — вертикальный срез каталога, menu sync и публикация тестового меню.

2026-09-08 — Этап 4 — DONE
Изменено: SQL repositories каталога и собственного меню venue, cursor списка
заведений, атомарный upsert/soft-delete menu snapshot с сохранением внутренних
ID, монотонная version/hash-политика, authenticated availability, seed меню,
HTTP-клиент venue с timeout/Bearer/request ID и startup-публикация. OpenAPI и
README обновлены, trade-offs цельного bounded menu snapshot и синхронного старта
зафиксированы отдельно.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), docker compose config --quiet, migration-test и docker
compose up -d --build; repository integration покрывает idempotent repeat,
version conflicts, soft-delete и zero stock. Все контейнеры healthy, повторная
публикация дала PUT/PATCH 204 и оставила 2 категории/3 позиции без дублей;
публичные GET venues/menu работают, каталог проверен при остановленном venue,
чужой partner token возвращает 401.
Ограничения/следующий шаг: меню намеренно отдаётся цельным версионированным
snapshot вместо cursor pagination; фоновые публикация/retry появятся при росте
интеграции. Начать этап 5 — атомарное создание заказа внутри платформы.

2026-09-08 — Этап 5 — DONE
Изменено: checkout validation, расчёт сумм из локального меню с overflow-check,
атомарное создание pending_confirmation заказа/snapshot/history/outbox,
идемпотентность по `(customer_ref, Idempotency-Key)` с advisory lock и canonical
request hash, воспроизводимый HMAC tracking token с хранением только hash,
`202 Accepted` и `Location`. Добавлена миграция позиции order item для
стабильного повторного ответа.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), migration up/repeat/down-up и docker compose up -d --build.
Integration tests покрывают точный повтор, конфликт payload, rollback после
частичной записи, закрытое venue, смену меню, недоступную позицию, minimum order
и неизменность price snapshot. HTTP smoke-test вернул один и тот же order/token
на повторный checkout, `202` и корректный `Location`.
Ограничения/следующий шаг: outbox пока только устойчиво записывается и не
доставляется; начать этап 6 — идемпотентный приём и резервирование заказа venue.

2026-09-08 — Этап 6 — DONE
Изменено: authenticated `POST /integration/v1/orders`, устойчивые решения по
`platform_order_id`, атомарная проверка version/price/availability/stock,
пессимистическое резервирование с детерминированным `FOR UPDATE`, сохранение
accepted/rejected и snapshot позиций, read-only management list/get API.
Добавлена venue migration с availability state, decision details и position.
Проверено: go test -race ./..., golangci-lint run (0 issues), Redocly lint
(valid), migration up/repeat/down-up и docker compose up -d --build.
Конкурентный integration test подтвердил, что последняя позиция достаётся одному
заказу; retry возвращает прежнее решение и не уменьшает stock повторно; price
conflict стабильно даёт `menu_changed`, неверный platform token — 401. HTTP
smoke-test дважды вернул один `external_order_id`, management API видит заказ.
Ограничения/следующий шаг: management list пока ограничен первыми 100 заказами
без cursor navigation; начать этап 7 — доставка platform outbox worker-ом.

2026-09-08 — Этап 7 — DONE
Изменено: конкурентно-безопасный claim platform outbox через
`FOR UPDATE SKIP LOCKED`, восстанавливаемые lease, bounded worker pool и
graceful shutdown, venue HTTP client с timeout/request ID/idempotency key,
retry classification, exponential backoff с jitter и confirmation deadline.
Решение venue, история и завершение outbox теперь фиксируются атомарно; поздний
ответ не может изменить terminal-заказ.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), migration up/repeat/down-up и docker compose up -d
--build. Integration tests покрывают конкурентный claim, восстановление lease,
защиту от старого worker-а и атомарный resolve; unit tests — HTTP-классификацию,
backoff, expiry и остановку до нового claim. Compose E2E автоматически провёл
заказ `pending_confirmation → accepted`, сохранил его в venue и завершил outbox
с первой попытки.
Ограничения/следующий шаг: все terminal-сетевые ошибки и исчерпание retry сейчас
сводятся к `confirmation_expired`; начать этап 8 — lifecycle-команды venue и
надёжная доставка callback-событий на платформу.

2026-09-08 — Этап 8 — DONE
Изменено: management-команды `start-preparing`, `mark-ready`, `complete`,
локальная venue state machine и атомарное создание callback event при переходе.
Venue callback outbox получил конкурентный claim, lease recovery, строгую
доставку по sequence, bounded worker pool, backoff и graceful shutdown. Platform
проверяет владельца заказа, дедуплицирует event ID, запрещает gap/повтор sequence
с другим событием и атомарно обновляет order, inbox и status history.
Проверено: go test -race ./..., go vet ./..., golangci-lint run (0 issues),
Redocly lint (valid), migration up/repeat/down-up и docker compose up -d
--build. Integration tests покрывают полный lifecycle, повтор management-команды,
запрет перехода назад, чужое venue, gap, duplicate/conflicting event и повтор
callback после потери ответа. Compose E2E провёл заказ
`accepted → preparing → ready → completed`; обе БД получили sequence 1/2/3,
все callback jobs завершены, повтор события вернул `204`.
Ограничения/следующий шаг: callback с устойчивой конфигурационной ошибкой
повторяется с capped backoff до вмешательства оператора; начать этап 9 —
защищённое чтение заказа и безопасная отмена.

2026-09-08 — Этап 9 — DONE
Изменено: защищённое чтение заказа по tracking token, локальная и удалённая
отмена, cancellation outbox, идемпотентное решение venue, однократный возврат
остатка и согласование callback с HTTP-ответом при гонках.
Проверено: integration/unit tests покрывают неверный token, повтор отмены,
гонку с preparing, однократный stock release и callback после потери ответа.
Ограничения/следующий шаг: отмена после начала приготовления отклоняется;
перейти к системным и отказоустойчивым сценариям этапа 10.

2026-09-08 — Этап 10 — DONE
Изменено: black-box E2E-сценарий, test-only fault injection задержки и HTTP
ошибок venue, проверки double-click, stale menu, закрытия venue, рестарта
worker, out-of-order callback и поздней отмены.
Проверено: сценарий включён в `make e2e`; итоговый результат фиксируется в
записи этапа 13 после единого acceptance run.
Ограничения/следующий шаг: fault injection запрещён вне APP_ENV=test.

2026-09-08 — Этап 11 — DONE
Изменено: `/metrics`, request/error/latency counters, bounded checkout
concurrency, fixed-window rate limit, структурированные worker logs и runbook
для диагностики очередей и восстановления интеграции.
Проверено: конфигурация, middleware и маршруты покрыты общим набором тестов;
операционные SQL-запросы и ограничения описаны в runbook.
Ограничения/следующий шаг: метрики process-local и сбрасываются при рестарте;
для MVP состояние очереди диагностируется напрямую из PostgreSQL.

2026-09-08 — Этап 12 — DONE
Изменено: Mermaid-источники customer/venue CJM, C4 Container, ER-моделей,
checkout sequence и cancellation race; README синхронизирован со сценариями,
архитектурой, эксплуатацией, ограничениями и post-MVP направлениями.
Проверено: рендер всех диаграмм включён в единый acceptance run этапа 13.
Ограничения/следующий шаг: диаграммы поддерживаются как код и должны
перегенерироваться командой `make diagrams` при изменении контрактов.

2026-09-08 — Этап 13 — DONE
Изменено: удалён устаревший NotImplemented scaffolding, добавлен repository
audit, сгенерированы семь SVG, hardened E2E получает seed ID из API и сообщает
точный failed HTTP response. Отдельный acceptance project поднят с нуля.
Проверено: `go test -race ./...`, `go vet ./...`, `make lint` (0 issues),
`make openapi-lint` (valid), `make compose-config`, `make migration-test`,
`make diagrams`, `make audit` и `COMPOSE_PROJECT_NAME=avito-kitchen-acceptance
make e2e` прошли. E2E подтвердил timeout→retry со второй попытки, полный
lifecycle, идемпотентность, конфликты, restart и обе отмены. Все runtime
контейнеры healthy/non-root, Swagger отдаёт спецификацию, обе очереди пусты.
Ограничения/следующий шаг: acceptance project и его volume намеренно оставлены;
рабочий project остановлен без удаления данных. Дальнейшие идеи вынесены в
post-MVP раздел и не входят в тестовое задание.
```
