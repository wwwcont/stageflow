# StageFlow

StageFlow is an orchestration service for repeatable, observable, and safer execution of HTTP-based workflows. It is designed for teams that need to model multi-step integration scenarios, run them on demand, inspect each step, and do it with production-style guardrails instead of ad-hoc shell scripts or one-off Postman collections.

## The problem StageFlow solves

Teams often need to exercise a sequence of dependent HTTP calls:

- create a resource in one service
- extract an identifier from the response
- pass that identifier into the next request
- assert that downstream systems reached the expected state
- rerun the scenario later with the same or overridden input
- debug failures with enough telemetry and artifacts to understand what happened

In practice, that workflow is usually split across scripts, CI jobs, notebooks, curl snippets, and tribal knowledge. The result is brittle staging validation, poor auditability, inconsistent error handling, and weak observability.

StageFlow centralizes that workflow into a backend service.

## StageFlow idea

A **flow** is a reusable definition of ordered HTTP steps.
A **run** is an execution of that flow with a concrete input payload.
Each **step** can:

- render request templates using run input and previously extracted values
- execute an outbound HTTP call
- extract values from the response
- assert expected invariants before continuing
- persist step-by-step execution state for later inspection

The service exposes an HTTP API to manage flow definitions, launch runs asynchronously, inspect results, and import a single curl command into a request spec draft.

## Key capabilities

- CRUD-style flow definition management with validation before execution
- asynchronous run launch and rerun support
- sequential execution engine with template rendering, extraction, assertions, and retries
- Redis-backed queue/worker processing model
- request allowlist enforcement for outbound HTTP calls
- header redaction in execution snapshots and logs
- SSRF-oriented protections such as host allowlisting, URL validation, and redirect suppression
- Prometheus metrics, structured logs, and OpenTelemetry tracing hooks
- in-memory repositories for local development and deterministic tests
- Postgres repository adapters and SQL migrations prepared for a persistent storage backend

## Architecture

```text
                   +---------------------------+
                   |        API clients        |
                   | curl / CI / tests / UI    |
                   +-------------+-------------+
                                 |
                                 v
+-------------------+   +--------+---------+   +----------------------+
| Prometheus/Grafana|<--|  HTTP API layer  |-->| Flow management usecase|
| metrics dashboards|   | handlers/mw      |   | create/update/validate |
+-------------------+   +--------+---------+   +----------------------+
                                 |
                                 v
                       +---------+----------+
                       | Run coordinator    |
                       | create run + queue |
                       +---------+----------+
                                 |
                                 v
                        +--------+--------+
                        | Redis run queue |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Worker service  |
                        | lease + execute |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Execution engine|
                        | render/extract  |
                        | assert/retry    |
                        +--------+--------+
                                 |
                                 v
                        +--------+--------+
                        | Safe HTTP exec  |
                        | allowlist +     |
                        | redaction       |
                        +-----------------+
```

## Project structure

```text
cmd/stageflow/               # main StageFlow process entrypoint
cmd/stageflow-sandbox/       # lightweight local mock/sandbox HTTP service
internal/app/                # application bootstrap and lifecycle wiring
internal/config/             # env/flag config loading and validation
internal/delivery/http/      # HTTP DTOs, handlers, middleware, server
internal/domain/             # domain models, value objects, validation, errors
internal/execution/          # engine, template rendering, extraction, assertions, HTTP executor
internal/observability/      # logging, tracing, metrics, repository instrumentation
internal/queue/redisqueue/   # Redis-backed queue implementation
internal/redisclient/        # minimal Redis client abstraction
internal/repository/         # repository contracts + in-memory adapters
internal/repository/postgres/# Postgres adapters and integration tests
internal/sandbox/            # sandbox service handlers, models, and in-memory state
internal/usecase/            # flow management, run coordination, curl import
internal/worker/             # background worker and dispatch coordination
migrations/                  # SQL migrations for the Postgres schema
pkg/clock/                   # small reusable dependency-light helpers
third_party/                 # local stubs used to keep the scaffold self-contained
```

## Current runtime shape

Today’s bootstrap wires:

- **in-memory repositories** for flows and runs
- **Redis** for async queueing
- **Prometheus metrics** on `/metrics`
- **OpenTelemetry tracing** with a stdout exporter by default

The repository contains Postgres adapters and migrations, but the default runtime still uses in-memory persistence. This is intentional for local iteration and testability, and is called out explicitly so the project does not overstate production readiness.

## Local development

### Prerequisites

- Go 1.23+
- Docker and Docker Compose

### Start the local stack

```bash
make compose-up
```

This starts:

- StageFlow API on `http://localhost:8080`
- StageFlow built-in UI on `http://localhost:8080/ui`
- StageFlow sandbox service on `http://localhost:8091`
- Redis on `localhost:6379`
- Postgres on `localhost:5432`
- Prometheus on `http://localhost:9090`
- Grafana on `http://localhost:3000` (`admin` / `admin`)

### Run only the app from source

If Redis is available locally:

```bash
make run
```

The built-in server-rendered UI is enabled by default and is served by the same Go process as the API. Open `http://localhost:8080/ui` to browse workspaces, requests, flows, variables, secrets, policies, and run history without a separate frontend build.


### Run the sandbox service

For local flow authoring and e2e validation without touching real integrations, StageFlow now ships with a separate lightweight sandbox service:

```bash
make run-sandbox
```

By default it listens on `http://localhost:8091`. Inside Docker Compose it is available to the StageFlow app at hostname `sandbox:8091`, and the default compose allowlist already includes `sandbox` so flows can safely target it.

The sandbox is intentionally designed for StageFlow-specific scenarios:

- stateful multi-step flows (`users` -> `accounts` -> `orders`)
- deterministic extraction from JSON bodies and response headers
- assertion success and failure cases
- retry testing with a controlled unstable endpoint
- timeout testing with delayed responses
- saved-request execution against predictable mock APIs

#### Sandbox endpoints

| Endpoint | Purpose |
|---|---|
| `POST /api/users` | create a user with stable nested fields for extraction |
| `GET /api/users/{id}` | fetch a user by id |
| `POST /api/accounts` | create an account linked to `user_id` |
| `GET /api/accounts/{id}` | fetch an account by id |
| `POST /api/orders` | create an order linked to `user_id` and `account_id` |
| `GET /api/orders/{id}` | fetch an order by id |
| `POST /api/orders/{id}/complete` | complete an order to drive multi-step state changes |
| `GET /api/unstable` | returns `500` twice, then `200` for retry testing |
| `GET /api/always-500` | deterministic failure endpoint |
| `GET /api/delay/{ms}` | delayed response for timeout handling |
| `GET /api/headers` | emits JSON plus headers like `X-Trace-ID` for header extraction |
| `/api/echo` | echoes method, headers, query, and body summary |
| `GET /api/assert/ok` | returns assertion-friendly success payload |
| `GET /api/assert/wrong-value` | returns a payload with intentionally wrong values |
| `GET /api/assert/missing-field` | returns a payload missing expected fields |
| `POST /api/reset` | clears all in-memory state |
| `POST /api/seed` | loads deterministic demo entities |
| `GET /api/debug/state` | dumps the current in-memory state |
| `GET /api/health` | sandbox liveness endpoint |

#### Sandbox in-memory model

The sandbox keeps everything in memory under a mutex and exposes strongly typed JSON models:

- `users` keyed by `user-{n}`
- `accounts` keyed by `acct-{n}` and linked to a user
- `orders` keyed by `order-{n}` and linked to both user and account
- sequence counters for generated ids
- an `unstable` call counter so retry behaviour is deterministic
- reset/seed metadata for debugging

Every stateful entity is designed to be easy to use from StageFlow extraction rules, with stable fields like `id`, `status`, `created_at`, nested objects, and link metadata.

#### Example sandbox scenarios for StageFlow

1. **Create user -> create account -> create order**  
   Build a flow that posts to `/api/users`, extracts `$.id`, creates an account with `user_id`, extracts `$.id`, then creates an order and asserts `$.status == "pending"`.

2. **Unstable endpoint with retry**  
   Point a saved request or flow step to `http://sandbox:8091/api/unstable` with retry enabled. The first two calls fail with `500`, the third succeeds.

3. **Timeout case**  
   Call `http://sandbox:8091/api/delay/2500` with a lower request timeout in StageFlow to verify timeout handling and run failure logging.

4. **Assertion failure case**  
   Call `http://sandbox:8091/api/assert/wrong-value` and assert that `$.result.value == "expected"` to force a deterministic assertion failure.

5. **Header extraction case**  
   Call `http://sandbox:8091/api/headers`, extract `X-Trace-ID` from response headers, and reuse it in a later step or assertion.

### Built-in UI

StageFlow now includes a lightweight HTML UI designed for internal operator/developer workflows:

- server-rendered with Go `html/template`
- served from the same backend process and port as the API
- no React/Vite/NPM build pipeline
- practical CRUD pages for workspaces, saved requests, flows, steps, variables, secrets, policy, and runs, including archive/restore for workspaces
- run details pages with formatted execution snapshots and auto-refresh for active runs

UI-specific settings:

| Setting | Purpose | Default |
|---|---|---|
| `STAGEFLOW_UI_ENABLED` | enable the built-in UI | `true` |
| `STAGEFLOW_UI_PREFIX` | route prefix for the built-in UI | `/ui` |

### Build and test

```bash
make build
make test
make lint
```

## Configuration

StageFlow is configured by environment variables and command-line flags.

Important settings:

| Setting | Purpose | Default |
|---|---|---|
| `STAGEFLOW_HTTP_ADDR` | API listen address | `:8080` |
| `STAGEFLOW_ALLOWED_HOSTS` | Comma-separated outbound HTTP allowlist | `example.internal,host.docker.internal` |
| `STAGEFLOW_DEFAULT_RUN_QUEUE` | Queue used for async launches | `default` |
| `STAGEFLOW_REDIS_ADDR` | Redis address | `127.0.0.1:6379` |
| `STAGEFLOW_REDIS_PREFIX` | Redis key prefix | `stageflow` |
| `STAGEFLOW_TRACING_ENABLED` | Enable tracing | `true` |
| `STAGEFLOW_TRACING_EXPORTER` | Tracing exporter (`stdout` or `none`) | `stdout` |
| `STAGEFLOW_TRACING_SAMPLE_RATIO` | Trace sampling ratio | `1` |
| `STAGEFLOW_LOG_LEVEL` | Log level | `info` |

Polish notes in the current version:

- invalid environment values now fail fast instead of silently falling back
- allowed hosts are normalized and deduplicated during config loading
- the service logs a concise runtime summary at startup

## Apply migrations

Migrations are included for the Postgres-backed repository implementation.

To apply them against the local compose Postgres instance:

```bash
make migrate-up
```

To roll them back:

```bash
make migrate-down
```

> Note: the default app bootstrap still uses in-memory repositories. The migrations are currently most useful for repository development, integration tests, and the next persistence integration step.

## API overview

### Create a flow

```bash
curl -X POST http://localhost:8080/api/v1/flows \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "order-happy-path",
    "name": "Order happy path",
    "description": "Creates an order and verifies its status",
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

### Import a curl command

```bash
curl -X POST http://localhost:8080/api/v1/import/curl \
  -H 'Content-Type: application/json' \
  -d '{
    "command": "curl -X POST https://api.example.internal/orders -H \"Authorization: Bearer secret\" -H \"Content-Type: application/json\" -d \"{\\\"customer_id\\\":\\\"123\\\"}\""
  }'
```

This endpoint returns a normalized request spec draft that can be copied into a flow step.

### Launch a run

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

### Inspect a run

```bash
curl http://localhost:8080/api/v1/runs/run-000000000000001
curl http://localhost:8080/api/v1/runs/run-000000000000001/steps
```

## Operational endpoints

- `GET /healthz` — lightweight liveness probe
- `GET /readyz` — readiness probe
- `GET /version` — service/environment/version information
- `GET /metrics` — Prometheus metrics

## Security model

StageFlow is intended for controlled internal environments, but the outbound HTTP executor still enforces meaningful protections.

### Allowlist

Outbound requests are only permitted to hosts configured in `STAGEFLOW_ALLOWED_HOSTS`.

Why this matters:

- avoids accidental calls to arbitrary internet endpoints
- reduces blast radius for malformed or user-supplied flow definitions
- makes intended integration boundaries explicit in config

### Redaction

Sensitive headers are redacted from execution snapshots and logs before being persisted or emitted. This protects common secrets such as:

- `Authorization`
- `Proxy-Authorization`
- `X-API-Key`
- `Cookie`
- `Set-Cookie`

The executor keeps the request structurally inspectable without leaking obvious credentials.

### SSRF mitigation

The safe HTTP executor includes several SSRF-oriented controls:

- host allowlisting
- scheme restriction to `http` and `https`
- URL userinfo rejection
- fragment rejection
- redirect suppression
- dial-time host/IP checks
- loopback/IP restrictions unless explicitly allowed in executor config
- request and response body size limits
- per-request timeouts

This is not a full internet-exposed zero-trust sandbox, but it is a serious step above naïve `net/http` execution.

## Observability

StageFlow includes three observability layers out of the box.

### Logs

- structured JSON logging via `zap`
- startup runtime summary
- request completion logs with request ID, method, route, status, and duration
- worker execution and heartbeat failures logged with run/worker identifiers

### Metrics

Examples of exported metrics:

- `flow_runs_total`
- `flow_run_duration_seconds`
- `flow_step_duration_seconds`
- `flow_step_failures_total`
- `active_runs`
- `http_requests_total`
- `http_request_duration_seconds`
- `worker_jobs_total`

### Tracing

OpenTelemetry spans are emitted for:

- inbound HTTP requests
- outbound HTTP execution
- flow run execution
- flow step execution
- repository operations

The current default exporter writes spans to stdout for local debugging.

## What already looks strong

- domain model is explicit and strongly validated instead of pushing everything into untyped maps
- execution concerns are separated into template rendering, extraction, assertions, and HTTP execution
- repository contracts are clear and the in-memory adapters make tests fast and deterministic
- worker queue leasing and recovery logic provide a realistic async execution shape
- the code already includes tracing, metrics, and structured logging rather than treating observability as an afterthought

## Trade-offs in the current version

- runtime persistence is still in-memory even though Postgres adapters already exist
- execution is sequential only; there is no DAG or fan-out/fan-in support yet
- tracing exporter is local-development oriented
- API surface is functional, but authentication/authorization is not implemented
- the built-in UI is intentionally minimal and form-oriented; it is not a SPA or visual flow builder
- there is no flow version promotion workflow beyond increment-on-update semantics

## Roadmap v2

- wire Postgres as the primary runtime repository backend
- add authn/authz and tenant-aware access controls
- expose richer retry/backoff policies and dead-letter handling
- add flow version history, diffing, and promotion workflows
- support step concurrency and dependency graphs where needed
- polish the built-in UI with richer diffing, filtering, and workflow affordances
- support durable artifacts and richer execution snapshots
- export traces to OTLP backends instead of stdout-only local output
- add rate limiting / budgets for outbound integrations

## Senior engineer self-review

### What improved in this polish pass

- tightened config ergonomics so invalid env values fail fast and allowlist hosts are normalized
- clarified naming around the run-oriented application service
- improved runtime lifecycle handling so worker execution uses cancellable contexts and the app shuts down more predictably on component failure
- added operational endpoints expected from a real service (`/healthz`, `/readyz`, `/version`)
- expanded tests into previously uncovered areas such as config loading and operational HTTP endpoints
- replaced the placeholder README with a publication-ready document that explains the service honestly and concretely

### What I would still develop next

If I were taking StageFlow to a true production milestone, my next step would be persistent runtime storage and auth. Those two changes would move the project from a strong orchestration scaffold to something a platform team could confidently expose to broader internal consumers.
