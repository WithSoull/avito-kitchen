# AGENTS.md

## Project context

This repository contains the MVP for **Avito.Kitchen**, a food-ordering
platform. The complete assignment is in
[`backend-trainee-assignment-autumn-2026.md`](backend-trainee-assignment-autumn-2026.md).

The goal is not to over-engineer a production platform. It is to deliver a
running, understandable MVP that demonstrates sound design and has a clear
path to evolve after the MVP.

The repository must ultimately contain:

- the main Avito.Kitchen service;
- one separately runnable example venue service, integrated with the main
  service and started by Docker Compose;
- database migrations and an OpenAPI specification;
- code-generated CJM diagrams for both customer and venue workflows;
- a README covering local launch, business scenarios, architecture (C4 Level 2
  or 3), data model, known MVP limitations, and the rationale for material
  trade-offs.

## Engineering principles

- Prefer simple, explicit code over clever abstractions.
- Keep domain rules independent from HTTP, persistence, Docker, and third-party
  SDKs. Transport and infrastructure must not leak into business logic.
- Design each bounded responsibility so it can be changed or extracted later,
  but do not introduce a microservice, broker, cache, or abstraction unless a
  current scenario justifies it.
- Make ownership, state transitions, validation, failures, and idempotency
  explicit—especially for order creation, availability, and venue integration.
- Treat the API contract and database migrations as versioned product
  interfaces. Update the implementation, OpenAPI document, migrations, tests,
  and README together when a contract changes.
- Preserve backward compatibility where it is cheap. If a breaking MVP change
  is necessary, document it clearly.
- Never add credentials, tokens, local `.env` files, generated secrets, or
  machine-specific paths to version control. Maintain a safe `.env.example`.

## Architecture decisions

The initial architecture is now fixed: two Go binaries in one module, REST/JSON
contracts described with OpenAPI 3.1, PostgreSQL persistence, and separate
Docker Compose containers for Avito.Kitchen and the example venue. The platform
stores the menu read-model and owns orders; the venue remains authoritative for
final price and availability confirmation. Material changes to this baseline
must be discussed and recorded in the README or a decision document first.

Do not introduce a web framework, ORM, message broker, authentication scheme,
or additional deployment unit unless a current scenario justifies it. The
planned reliable integration mechanism is a PostgreSQL transactional outbox
with an HTTP worker; Kafka is intentionally outside the MVP.

Use `/Users/grishinid/home/01_Coding/06_pets/UserServer` as a quality and
organization reference, not as a template to copy blindly. Its useful patterns
include clear separation of handler/service/repository concerns, explicit
dependency wiring, configuration through environment variables, migrations,
Compose-based local development, graceful resource cleanup, and focused unit
tests with mocked boundaries. Adopt a pattern only after checking that it fits
this project's agreed architecture and MVP scope.

## Implementation rules

- Read the assignment and relevant existing code before changing a feature.
- Keep public request/response DTOs separate from domain and persistence
  models; do not expose database records directly through an API.
- Validate input at the API boundary and enforce business invariants in the
  application/domain layer. Repository checks alone are not business
  validation.
- Pass `context.Context` through I/O and request lifecycles. Do not use global
  mutable state for request-specific data.
- Return stable, client-meaningful errors. Do not expose internal database or
  dependency errors in API responses.
- Use database transactions for changes that must succeed or fail together.
- Make external calls resilient: set timeouts, propagate cancellation, and
  handle retries only when the operation is safe to retry.
- Keep configuration typed and centralized. Fail fast with actionable errors
  when required configuration is missing or invalid.
- Add structured logs at operational boundaries without logging secrets or
  unnecessary personal data.
- Prefer small cohesive packages and files. Avoid `utils`/`common` dumping
  grounds and premature generic repositories/services.

## API, data, and integration rules

- Start from a user or venue scenario, then specify the API and data model;
  implementation follows the contract rather than inventing undocumented
  endpoints.
- Model order and availability states deliberately. Every transition needs a
  defined actor, preconditions, result, and failure behaviour.
- Consider concurrent requests when reserving stock or confirming an order.
  Choose and document the consistency strategy rather than relying on timing.
- Give integration messages and callbacks stable identifiers and enough context
  for tracing and deduplication. Define what happens when the venue is slow,
  unavailable, or returns a conflict.
- Use UTC for persisted timestamps and explicit currency/amount conventions;
  do not use floating-point values for money.
- Migrations must be ordered, reproducible on an empty database, and safe to
  run by the Compose environment. Do not edit an already-applied migration;
  add a new one instead.

## Quality gates

For every material change:

1. Format the changed source files with the language's standard formatter.
2. Run the configured linter/static analysis.
3. Add or update tests at the appropriate boundary. At minimum, cover new
   business rules, state transitions, validation, and previously fixed bugs.
4. Run the relevant test suite and report the exact command and outcome.
5. If Compose, migrations, or an inter-service contract changed, verify the
   documented local startup path and the affected end-to-end scenario.
6. Update OpenAPI, diagrams, and README whenever the externally observable
   behaviour changed.

Do not claim a command passed unless it was run successfully. If a check cannot
be run, state why and describe the remaining risk.

## Change hygiene

- Keep commits and pull requests focused; do not mix refactors with unrelated
  functional changes.
- Do not overwrite or discard existing user changes without explicit consent.
- Avoid generated code edits by hand. Change the source schema/template and run
  the documented generator instead.
- Prefer adding a short decision note for non-obvious choices over leaving
  rationale only in code comments.
- Before finishing, inspect the diff for accidental files, incomplete contract
  updates, and inconsistencies with this document.
