# StageFlow

StageFlow — это сервис оркестрации для повторяемого, наблюдаемого и более безопасного выполнения HTTP-ориентированных сценариев. Он предназначен для команд, которым нужно моделировать многошаговые интеграционные последовательности, запускать их по требованию, инспектировать каждый шаг и делать это с продакшен-подобными защитными ограничениями вместо ad-hoc shell-скриптов или разрозненных коллекций Postman.

## Какую проблему решает StageFlow

Командам регулярно требуется выполнять последовательность зависимых HTTP-вызовов:

- создать ресурс в одном сервисе;
- извлечь идентификатор из ответа;
- передать этот идентификатор в следующий запрос;
- проверить, что downstream-системы пришли в ожидаемое состояние;
- повторно запустить сценарий позже с тем же или переопределённым входом;
- отладить сбои, имея достаточно телеметрии и артефактов, чтобы понять, что произошло.

На практике такой процесс обычно размазан по скриптам, CI-задачам, ноутбукам, curl-сниппетам и «устным договорённостям» внутри команды. В результате получаются хрупкие проверки staging-окружений, слабая аудируемость, непоследовательная обработка ошибок и слабая наблюдаемость.

StageFlow централизует этот процесс в одном backend-сервисе.

## Идея StageFlow

**Flow** — это переиспользуемое определение упорядоченных HTTP-шагов.
**Run** — это выполнение такого flow с конкретным входным payload.
Каждый **step** может:

- рендерить шаблоны запросов, используя вход запуска и ранее извлечённые значения;
- выполнять исходящий HTTP-вызов;
- извлекать значения из ответа;
- проверять ожидаемые инварианты перед продолжением;
- сохранять пошаговое состояние выполнения для последующего анализа.

Сервис предоставляет HTTP API для управления определениями flow, асинхронного запуска run, просмотра результатов и импорта одной команды curl в черновик request spec.

## Ключевые возможности

- управление определениями flow в стиле CRUD с валидацией до выполнения;
- асинхронный запуск run и поддержка повторных запусков;
- последовательный движок выполнения с рендерингом шаблонов, извлечением значений, проверками и ретраями;
- модель обработки через очередь/воркер на базе Redis;
- enforcement allowlist-а для исходящих HTTP-запросов;
- редактирование чувствительных заголовков в execution snapshots и логах;
- SSRF-ориентированные защиты: allowlist хостов, валидация URL и запрет редиректов;
- метрики Prometheus, структурированные логи и интеграционные точки для OpenTelemetry tracing;
- in-memory репозитории для локальной разработки и детерминированных тестов;
- Postgres-адаптеры репозиториев и SQL-миграции, подготовленные для постоянного хранилища.

## Архитектура

```text
                   +---------------------------+
                   |      API-клиенты          |
                   | curl / CI / tests / UI    |
                   +-------------+-------------+
                                 |
                                 v
+-------------------+   +--------+---------+   +----------------------+
| Prometheus/Grafana|<--|   HTTP API слой  |-->| Use case управления  |
| панели метрик     |   | handlers/mw      |   | flow: create/update/ |
+-------------------+   +--------+---------+   | validate             |
                                 |             +----------------------+
                                 v
                       +---------+----------+
                       | Координатор run    |
                       | create run + queue |
                       +---------+----------+
                                 |
                                 v
                        +--------+--------+
                        | Redis-очередь   |
                        | запусков        |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Worker-сервис    |
                        | lease + execute  |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Движок выполнения|
                        | render/extract   |
                        | assert/retry     |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Безопасный HTTP  |
                        | executor:        |
                        | allowlist + mask |
                        +-----------------+
```

## Структура проекта

```text
cmd/stageflow/               # основная точка входа процесса StageFlow
cmd/stageflow-sandbox/       # лёгкий локальный mock/sandbox HTTP-сервис
internal/app/                # bootstrap приложения и связывание жизненного цикла
internal/config/             # загрузка и валидация env/flag-конфигурации
internal/delivery/http/      # HTTP DTO, handlers, middleware, server
internal/domain/             # доменные модели, value objects, валидация, ошибки
internal/execution/          # движок, рендеринг шаблонов, extraction, assertions, HTTP executor
internal/observability/      # логирование, tracing, metrics, инструментирование репозиториев
internal/queue/redisqueue/   # реализация очереди на базе Redis
internal/redisclient/        # минимальная абстракция Redis-клиента
internal/repository/         # контракты репозиториев + in-memory адаптеры
internal/repository/postgres/# Postgres-адаптеры и интеграционные тесты
internal/sandbox/            # handlers sandbox-сервиса, модели и in-memory состояние
internal/usecase/            # управление flow, координация run, импорт curl
internal/worker/             # фоновый worker и координация диспетчеризации
migrations/                  # SQL-миграции для схемы Postgres
pkg/clock/                   # небольшие переиспользуемые помощники без тяжёлых зависимостей
third_party/                 # локальные стабы, сохраняющие каркас самодостаточным
```

## Текущая форма runtime

Сейчас bootstrap по умолчанию подключает:

- **in-memory репозитории** для flow и run;
- **Redis** для асинхронной очереди;
- **Prometheus metrics** на `/metrics`;
- **OpenTelemetry tracing** с stdout-exporter по умолчанию.

В репозитории уже есть Postgres-адаптеры и миграции, но runtime по умолчанию по-прежнему использует in-memory persistence. Это сделано осознанно ради удобной локальной итерации и тестируемости; README специально подчёркивает это, чтобы не создавать ложного ощущения production-ready состояния.

## Локальная разработка

### Требования

- Go 1.23+
- Docker и Docker Compose

### Запуск локального стека

```bash
make compose-up
```

Команда поднимает:

- StageFlow API на `http://localhost:8080`;
- встроенный UI StageFlow на `http://localhost:8080/ui`;
- sandbox-сервис StageFlow на `http://localhost:8091`;
- Redis на `localhost:6379`;
- Postgres на `localhost:5432`;
- Prometheus на `http://localhost:9090`;
- Grafana на `http://localhost:3000` (`admin` / `admin`).

### Запуск только приложения из исходников

Если Redis уже доступен локально:

```bash
make run
```

Встроенный server-rendered UI включён по умолчанию и обслуживается тем же Go-процессом, что и API. Откройте `http://localhost:8080/ui`, чтобы просматривать рабочие пространства, запросы, flow, переменные, секреты, политики и историю запусков без отдельной frontend-сборки.

### Запуск sandbox-сервиса

Для локального проектирования flow и e2e-проверок без обращения к реальным интеграциям StageFlow включает отдельный лёгкий sandbox-сервис:

```bash
make run-sandbox
```

По умолчанию он слушает `http://localhost:8091`. Внутри Docker Compose он доступен приложению StageFlow по имени хоста `sandbox:8091`, а allowlist по умолчанию в compose уже содержит `sandbox`, поэтому flow могут безопасно обращаться к нему.

Sandbox специально заточен под сценарии StageFlow:

- stateful многошаговые сценарии (`users` -> `accounts` -> `orders`);
- детерминированное извлечение из JSON-тел и заголовков ответа;
- сценарии успешных и провальных проверок assertions;
- тестирование retry с управляемым нестабильным endpoint;
- тестирование таймаутов с задержанными ответами;
- выполнение saved requests против предсказуемых mock API.

#### Endpoint’ы sandbox

| Endpoint | Назначение |
|---|---|
| `POST /api/users` | создаёт пользователя со стабильными вложенными полями для extraction |
| `GET /api/users/{id}` | получает пользователя по id |
| `POST /api/accounts` | создаёт аккаунт, связанный с `user_id` |
| `GET /api/accounts/{id}` | получает аккаунт по id |
| `POST /api/orders` | создаёт заказ, связанный с `user_id` и `account_id` |
| `GET /api/orders/{id}` | получает заказ по id |
| `POST /api/orders/{id}/complete` | завершает заказ, чтобы воспроизводить многошаговые изменения состояния |
| `GET /api/unstable` | дважды возвращает `500`, затем `200` для проверки retry |
| `GET /api/always-500` | детерминированный endpoint с ошибкой |
| `GET /api/delay/{ms}` | ответ с задержкой для проверки таймаутов |
| `GET /api/headers` | возвращает JSON и заголовки вроде `X-Trace-ID` для extraction из header |
| `/api/echo` | эхо-ответ с методом, заголовками, query и кратким summary body |
| `GET /api/assert/ok` | возвращает payload, удобный для успешных assertions |
| `GET /api/assert/wrong-value` | возвращает payload с заведомо неверными значениями |
| `GET /api/assert/missing-field` | возвращает payload без ожидаемых полей |
| `POST /api/reset` | очищает всё in-memory состояние |
| `POST /api/seed` | загружает детерминированные demo-сущности |
| `GET /api/debug/state` | выгружает текущее in-memory состояние |
| `GET /api/health` | endpoint проверки живости sandbox |

### Встроенный UI

StageFlow включает лёгкий HTML UI для внутренних операторских и разработческих сценариев:

- серверный рендеринг на Go `html/template`;
- обслуживается тем же backend-процессом и на том же порту, что и API;
- без React/Vite/NPM pipeline;
- практичные CRUD-страницы для рабочих пространств, saved requests, flow, шагов, переменных, секретов, политик и run, включая архивирование/восстановление рабочих пространств;
- страницы run details с форматированными execution snapshots и auto-refresh для активных запусков.

Настройки UI:

| Параметр | Назначение | По умолчанию |
|---|---|---|
| `STAGEFLOW_UI_ENABLED` | включает встроенный UI | `true` |
| `STAGEFLOW_UI_PREFIX` | префикс маршрутов встроенного UI | `/ui` |

### Сборка и тестирование

```bash
make build
make test
make lint
```

## Конфигурация

StageFlow настраивается через переменные окружения и флаги командной строки.

Важные параметры:

| Параметр | Назначение | По умолчанию |
|---|---|---|
| `STAGEFLOW_HTTP_ADDR` | адрес прослушивания API | `:8080` |
| `STAGEFLOW_ALLOWED_HOSTS` | allowlist исходящих HTTP-хостов через запятую | `example.internal,host.docker.internal` |
| `STAGEFLOW_DEFAULT_RUN_QUEUE` | очередь для асинхронных запусков | `default` |
| `STAGEFLOW_REDIS_ADDR` | адрес Redis | `127.0.0.1:6379` |
| `STAGEFLOW_REDIS_PREFIX` | префикс ключей Redis | `stageflow` |
| `STAGEFLOW_TRACING_ENABLED` | включает tracing | `true` |
| `STAGEFLOW_TRACING_EXPORTER` | exporter tracing (`stdout` или `none`) | `stdout` |
| `STAGEFLOW_TRACING_SAMPLE_RATIO` | коэффициент семплирования trace | `1` |
| `STAGEFLOW_LOG_LEVEL` | уровень логирования | `info` |

## Применение миграций

В репозитории уже есть миграции для реализации репозиториев на базе Postgres.

Чтобы применить их к локальному экземпляру Postgres из compose:

```bash
make migrate-up
```

Чтобы откатить миграции:

```bash
make migrate-down
```

> Примечание: bootstrap приложения по умолчанию всё ещё использует in-memory репозитории. Сейчас миграции наиболее полезны для разработки репозиториев, интеграционных тестов и следующего шага по интеграции постоянного хранилища.

## Обзор API

### Создание flow

```bash
curl -X POST http://localhost:8080/api/v1/flows \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "order-happy-path",
    "name": "Успешный сценарий заказа",
    "description": "Создаёт заказ и проверяет его статус",
    "status": "active",
    "steps": [
      {
        "id": "create-order",
        "order_index": 0,
        "name": "create-order",
        "request": {
          "method": "POST",
          "url": "http://host.docker.internal:8081/orders",
          "headers": {
            "Content-Type": "application/json",
            "Authorization": "Bearer secret-token"
          },
          "body": "{\"customer_id\":\"{{ input.customer_id }}\"}"
        },
        "extract": {
          "rules": [
            {"name": "order_id", "json_path": "$.id"}
          ]
        },
        "assert": {
          "rules": [
            {"kind": "status_code", "expected": 201}
          ]
        }
      },
      {
        "id": "get-order",
        "order_index": 1,
        "name": "get-order",
        "request": {
          "method": "GET",
          "url": "http://host.docker.internal:8081/orders/{{ order_id }}"
        },
        "assert": {
          "rules": [
            {"kind": "status_code", "expected": 200},
            {"kind": "json_path_equals", "json_path": "$.status", "expected": "created"}
          ]
        }
      }
    ]
  }'
```

### Импорт команды curl

```bash
curl -X POST http://localhost:8080/api/v1/import/curl \
  -H 'Content-Type: application/json' \
  -d '{
    "command": "curl -X POST https://api.example.internal/orders -H \"Authorization: Bearer secret\" -H \"Content-Type: application/json\" -d \"{\\\"customer_id\\\":\\\"123\\\"}\""
  }'
```

Этот endpoint возвращает нормализованный черновик request spec, который можно скопировать в шаг flow.

### Запуск run

```bash
curl -X POST http://localhost:8080/api/v1/flows/order-happy-path/runs \
  -H 'Content-Type: application/json' \
  -d '{
    "initiated_by": "local-dev",
    "input": {
      "customer_id": "cust-123"
    },
    "idempotency_key": "order-happy-path-cust-123"
  }'
```

### Просмотр run

```bash
curl http://localhost:8080/api/v1/runs/run-000000000000001
curl http://localhost:8080/api/v1/runs/run-000000000000001/steps
```

## Операционные endpoint’ы

- `GET /healthz` — лёгкая liveness-проверка;
- `GET /readyz` — readiness-проверка;
- `GET /version` — информация о сервисе, окружении и версии;
- `GET /metrics` — метрики Prometheus.

## Модель безопасности

StageFlow рассчитан на контролируемые внутренние окружения, но executor исходящих HTTP-запросов всё равно применяет значимые защитные меры.

### Allowlist

Исходящие запросы разрешены только к хостам, указанным в `STAGEFLOW_ALLOWED_HOSTS`, что:

- предотвращает случайные обращения к произвольным интернет-адресам;
- уменьшает радиус поражения для ошибочных или пользовательских определений flow;
- делает ожидаемые интеграционные границы явными на уровне конфигурации.

### Редакция чувствительных данных

Чувствительные заголовки маскируются в execution snapshots и логах до их сохранения или отправки. Это защищает типовые секреты, например:

- `Authorization`;
- `Proxy-Authorization`;
- `X-API-Key`;
- `Cookie`;
- `Set-Cookie`.

Executor сохраняет структурную inspectability запроса, не раскрывая при этом очевидные учётные данные.

### Защита от SSRF

Безопасный HTTP executor включает несколько защит, направленных против SSRF:

- allowlist хостов;
- ограничение схем до `http` и `https`;
- запрет URL userinfo;
- запрет fragment;
- подавление redirect;
- проверки хоста/IP на этапе dial;
- ограничения loopback/IP, если они явно не разрешены в конфигурации executor;
- лимиты на размер request и response body;
- таймауты на каждый запрос.

## Наблюдаемость

StageFlow из коробки включает три уровня наблюдаемости.

### Логи

- структурированное JSON-логирование через `zap`;
- стартовую сводку runtime;
- логи завершения запросов с request ID, методом, маршрутом, статусом и длительностью;
- логи ошибок выполнения worker и heartbeat с идентификаторами run/worker.

### Метрики

Примеры экспортируемых метрик:

- `flow_runs_total`;
- `flow_run_duration_seconds`;
- `flow_step_duration_seconds`;
- `flow_step_failures_total`;
- `active_runs`;
- `http_requests_total`;
- `http_request_duration_seconds`;
- `worker_jobs_total`.

### Трассировка

OpenTelemetry spans создаются для:

- входящих HTTP-запросов;
- исходящих HTTP-вызовов;
- выполнения run;
- выполнения шагов flow;
- операций репозитория.

Текущий exporter по умолчанию пишет spans в stdout для локальной отладки.