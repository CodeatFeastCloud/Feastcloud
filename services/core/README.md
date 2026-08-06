<!-- SPDX-License-Identifier: AGPL-3.0-only -->

# FeastCloud Core Service

This directory contains the FeastCloud modular-monolith foundation. It exposes tenant-scoped organization, kitchen execution, Kitchen Graph, recipe, menu, inventory-ledger, food-cost, audit, and edge-inbox APIs. The durable sync adapter uses the MIT-licensed pgx PostgreSQL driver.

Without a database URL, the resource adapter stores data in memory so the domain and HTTP contracts can be demonstrated without infrastructure. When `FEASTCLOUD_DATABASE_URL` is set, the organization hierarchy, orders, normalized lines, kitchen tickets, ticket lines, audit records, sync inbox, and domain-event ledger all use PostgreSQL through tenant-scoped transactions. Without it, sync safely returns a non-terminal retry.

## Run and test

Go 1.22 or later is required.

```sh
go test ./...
go run ./cmd/core -addr :8080
```

The repository root includes an optional native self-hosted PostgreSQL profile. Docker is not required:

```sh
npm run db:install
npm run db:up
npm run db:test
npm run core:postgres
```

The integration test is skipped during an infrastructure-free `go test`. Setting `FEASTCLOUD_TEST_DATABASE_URL` makes it mandatory and verifies the real transaction and RLS boundary through the restricted runtime role.

The server uses explicit HTTP timeouts, graceful shutdown, JSON structured logs, panic recovery, request IDs, security headers, and a 1 MiB limit for ordinary mutation requests. The edge sync endpoint accepts at most 500 operations or 5 MiB.

Public probes and discovery:

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
curl http://localhost:8080/api/v1
```

The committed OpenAPI 3.1 contract is in `api/openapi.yaml`.

## Authentication and device identity

`FEASTCLOUD_AUTH_MODE=demo` remains the loopback development default. `FEASTCLOUD_AUTH_MODE=oidc` is the fail-closed production profile: user requests require an RS256 OIDC bearer token with matching issuer, audience, `tenant_id`, role, outlet scope, and valid lifetime. Core requires PostgreSQL and TLS in this mode. Edge sync requests require a client certificate issued by the configured device CA, and its SHA-256 certificate fingerprint must belong to an active enrolled device in the same tenant and outlet.

Production variables are `FEASTCLOUD_OIDC_ISSUER`, `FEASTCLOUD_OIDC_AUDIENCE`, `FEASTCLOUD_OIDC_PUBLIC_KEY_FILE`, `FEASTCLOUD_CORE_TLS_CERT`, `FEASTCLOUD_CORE_TLS_KEY`, and `FEASTCLOUD_CORE_CLIENT_CA`. Keycloak is the recommended open-source issuer; the core remains standards-based and does not depend on a proprietary identity service.

Managers enroll or revoke certificates through the idempotent, audited `POST /api/v1/devices` and `POST /api/v1/devices/{id}/revoke` mutations. Revocation takes effect on the next sync request. Owner/manager, cashier, chef, and device permissions are enforced server-side, including outlet assignment.

### Demo boundary

Every route below `/api/v1/` requires both demo authentication headers:

- `X-FeastCloud-Tenant-ID`: the authorized tenant scope
- `X-FeastCloud-Actor-ID`: the authenticated actor or service principal

For ordinary mutations, `tenantId` and `actorId` values must match those headers. A `tenantId` query assertion, when supplied, must also match. Cross-tenant entity reads return `404`; explicit cross-tenant assertions and mutations return `403`.

For `POST /api/v1/sync/operations`, the authenticated actor is the outlet edge service principal `edge:<edgeId>`. Each enclosed mutation retains the original cashier, chef, or device actor for audit; its tenant must match the authenticated tenant. The batch `edgeId` therefore identifies the gateway and does not overwrite the originating `deviceId`.

This header adapter exists only to make isolation testable. Never expose demo mode directly to an untrusted network.

## REST v1 resources

| Method and path | Behavior |
|---|---|
| `GET /api/v1/organizations` | List organizations for the authenticated tenant |
| `POST /api/v1/organizations` | Create the tenant organization; organization `id` equals `tenantId` |
| `GET /api/v1/outlets`, `POST /api/v1/outlets` | List or create outlets |
| `GET /api/v1/brands`, `POST /api/v1/brands` | List or create brands |
| `GET /api/v1/stations`, `POST /api/v1/stations` | List or create outlet stations |
| `GET /api/v1/orders`, `POST /api/v1/orders` | List or ingest canonical orders |
| `POST /api/v1/orders/{id}/transitions` | Move an order through its lifecycle using `expectedVersion` |
| `GET /api/v1/kitchen-tickets`, `POST /api/v1/kitchen-tickets` | List or route kitchen tickets |
| `POST /api/v1/kitchen-tickets/{id}/transitions` | Move one station ticket using `expectedVersion` |
| `GET /api/v1/audit-events` | Read append-only mutation audit events |
| `POST /api/v1/devices`, `POST /api/v1/devices/{id}/revoke` | Enroll and revoke outlet device certificates |
| `GET/POST /api/v1/units`, `/ingredients`, `/recipes`, `/menu-items` | Build the versioned Kitchen Graph and menu-to-recipe relationship |
| `POST /api/v1/recipes/{id}/versions` | Publish a new effective-dated recipe version while preserving history |
| `POST /api/v1/inventory-events` | Append receiving, waste, spoilage, transfer, production, staff-meal, or reversal evidence |
| `POST /api/v1/inventory-counts` | Complete an immutable multi-line physical count and atomically post costed variances |
| `GET /api/v1/inventory-summary?outletId=…` | Read ledger-derived stock, theoretical usage, waste, and food-cost values |
| `GET/POST /api/v1/production-batches` | List or plan version-pinned preparation batches |
| `POST /api/v1/production-batches/{id}/transitions` | Start or complete a batch; completion atomically consumes recipe inputs and receives actual yield |
| `GET/POST /api/v1/order-imports` | Import canonical CSV rows and inspect immutable row-level reconciliation results |
| `GET/POST /api/v1/planning-runs` | Generate and inspect observe-only demand, preparation, and stockout recommendations |
| `POST /api/v1/sync/operations` | Atomically commit inbox evidence plus an append-only domain event when PostgreSQL is configured |

Each resource also has `GET /api/v1/{resource}/{id}` except audit events and sync. Lists use `limit` (default 50, maximum 200) and an opaque `cursor`; filters use camelCase names such as `outletId` and `organizationId`. Configuration reads return a strong version `ETag`.

### Canonical mutation envelope

All ordinary POST routes require `Content-Type: application/json` and an `Idempotency-Key` header matching the envelope. Identifiers are generated at the mutation origin; UUIDv7 is recommended for sortable offline identifiers. Public JSON is camelCase and `schemaVersion` is currently `"1.0"`.

```sh
curl -X POST http://localhost:8080/api/v1/organizations \
  -H 'Content-Type: application/json' \
  -H 'X-FeastCloud-Tenant-ID: 018f0000-0000-7000-8000-000000000001' \
  -H 'X-FeastCloud-Actor-ID: bootstrap-manager' \
  -H 'Idempotency-Key: bootstrap-organization-0001' \
  --data '{
    "id": "018f0000-0000-7000-8000-000000000010",
    "tenantId": "018f0000-0000-7000-8000-000000000001",
    "outletId": "018f0000-0000-7000-8000-000000000002",
    "deviceId": "018f0000-0000-7000-8000-000000000003",
    "actorId": "bootstrap-manager",
    "occurredAt": "2026-08-03T10:00:00Z",
    "source": "feastcloud-admin",
    "sourceId": "bootstrap-organization-0001",
    "schemaVersion": "1.0",
    "idempotencyKey": "bootstrap-organization-0001",
    "payload": {
      "id": "018f0000-0000-7000-8000-000000000001",
      "name": "Example Kitchens",
      "legalName": "Example Kitchens Private Limited",
      "defaultLocale": "en-IN",
      "defaultCurrency": "INR"
    }
  }'
```

Successful creates return `201`, `Location`, and `Idempotency-Replayed`. A concurrent or later retry with the same authenticated scope, route, key, and canonical JSON command returns the original business result without creating a second entity or audit event. Reusing the key for a different command returns `409 idempotency_key_reused`. PostgreSQL installations retain the exact operation value, status, and response headers for 90 days across core restarts; the infrastructure-free adapter retains them in memory.

Order and ticket transitions require `expectedVersion`. A stale terminal receives `409 version_conflict`; an impossible lifecycle move receives `422 invalid_transition`. Successful transitions increment the aggregate version and append their audit record in the same transaction.

Errors use `application/problem+json` with a stable `code`, client-localizable `messageKey`, correlation ID, retryability, and optional field violations.

## Edge inbox

`POST /api/v1/sync/operations` accepts the shared edge shape:

```json
{
  "batchId": "batch-42",
  "edgeId": "device-id",
  "outletId": "outlet-id",
  "operations": [
    {
      "operationId": "uuid",
      "aggregateType": "order",
      "aggregateId": "uuid",
      "aggregateVersion": 0,
      "commandType": "order.create",
      "mutation": { "...": "canonical mutation envelope" },
      "recordedAt": "2026-08-03T10:00:01Z"
    }
  ]
}
```

The shared protocol supports `ACCEPTED`, `DUPLICATE`, `REJECTED`, `CONFLICT`, and `RETRY`. With `FEASTCLOUD_DATABASE_URL` configured, core inserts immutable inbox evidence and an append-only `domain_events` row in one PostgreSQL transaction, then marks the inbox accepted before commit. An exact replay returns `DUPLICATE`; reusing an operation ID with different evidence returns `CONFLICT`; unsupported commands are durably `REJECTED`. A database failure returns `RETRY`, so the edge does not discard its outbox entry. Without a database URL, valid operations return `RETRY / sync_inbox_unavailable` by design.

## PostgreSQL foundation

The migrations create tenants, organizations, outlets, brands, stations, orders, normalized order/ticket lines, kitchen tickets, audit events, durable idempotency responses, the sync inbox, and the append-only domain-event ledger. Composite foreign keys prevent cross-tenant and cross-outlet references. Every table has forced row-level security using a transaction-local setting. Audit/domain events reject update and delete operations; sync-inbox identity, command, hash, and mutation evidence cannot be updated or deleted while processing status fields remain updateable.

PostgreSQL order and ticket lists use `(created_at, id)` keyset pagination with opaque, filter-bound cursors. This avoids offset drift, remains valid if earlier rows are removed, and never loads the full shift into application memory.

## Kitchen Graph and inventory accounting

Units store exact rational conversion factors, so grams, kilograms, millilitres, litres, and counts do not accumulate floating conversion drift in PostgreSQL. Ingredients carry allergens and dietary labels. Recipes have immutable effective-dated versions, yield and loss facts, and either ingredient or fixed child-recipe-version components; recursive cycles are rejected.

When an order is created, every recognized menu line captures the recipe version effective at placement time. Completing the order—directly or through an accepted outlet-edge whole-order KDS event—expands that snapshot through sub-recipes and appends one aggregated consumption event per ingredient. Later recipe edits cannot change historical consumption. Inbox evidence, the edge domain event, cloud order projection, inventory consumption, and sync acceptance commit in one transaction. Exact replay creates no additional consumption.

Inventory history is append-only. Receiving and inbound events add stock; consumption, waste, spoilage, transfers out, and staff meals subtract it. Corrections use reversal or count-adjustment entries. Each event retains unit-conversion provenance at the API boundary, actor, device, operation, lot, expiry, reason, currency, and cost. The summary is reproduced entirely from ledger entries.

Physical counts are completed as an atomic multi-line workflow. PostgreSQL serializes each outlet/ingredient balance, captures the expected and physically counted quantities as immutable evidence, and posts only the calculated variance to the ledger. A zero-variance line is still retained as proof that it was counted.

Apply with a migration runner or directly for development:

```sh
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000001_foundation.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000002_domain_events.up.sql
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f migrations/000003_nullable_organization_audit.up.sql
```

Every application transaction must establish its authorized tenant before accessing data:

```sql
BEGIN;
SET LOCAL app.tenant_id = '018f0000-0000-7000-8000-000000000001';
-- tenant-scoped statements
COMMIT;
```

The database owner/migration role must not be used by the runtime. Use a restricted runtime role without `BYPASSRLS`, and keep tenant assignment and business writes in the same transaction.

Run core with the restricted runtime connection:

```sh
FEASTCLOUD_DATABASE_URL="$RUNTIME_DATABASE_URL" go run ./cmd/core -addr :8080
```

When this variable is set, `/readyz` returns `503` until PostgreSQL is reachable and the resource, audit, sync, and domain-event migrations are present. The runtime does not apply migrations itself.

## Package boundaries

- `internal/domain`: canonical Go models and value validation
- `internal/store`: persistence port, durable PostgreSQL repository, and concurrency-safe infrastructure-free adapter
- `internal/idempotency`: concurrent replay coordination with payload-conflict detection
- `internal/api`: REST, pagination, problem details, demo auth, and edge inbox
- `cmd/core`: process lifecycle and HTTP configuration
- `migrations`: durable PostgreSQL schema and tenant isolation

Future modules should depend on the repository/domain ports, not on a concrete adapter. Core operation, tests, and self-hosting do not require proprietary services.

## License

This service is licensed under `AGPL-3.0-only`, matching the repository license. Public interoperability schemas under `packages/contracts` are separately Apache-2.0.
