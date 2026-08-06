# Self-hosted PostgreSQL development profile

Status: executable development and CI profile  
Last updated: 2026-08-03

This profile runs FeastCloud's durable resource and sync boundaries against a loopback-only native PostgreSQL process. Docker is not required. The infrastructure-free demo remains available without PostgreSQL and safely keeps cloud operations pending.

## Start and verify

Requirements: macOS with Homebrew. The project installs the open-source `postgresql@17` formula, but it does not use `brew services` or a global PostgreSQL cluster. On a nonstandard Homebrew prefix, `db:install` also repairs missing keg-only `share`/`lib` links without replacing any existing path.

```sh
npm run db:install
npm run db:up
npm run db:test
FEASTCLOUD_TEST_DATABASE_URL='postgres://feastcloud_runtime:feastcloud_dev_runtime@127.0.0.1:54329/feastcloud?sslmode=disable' npm run smoke:postgres
```

`db:up` initializes `.feastcloud/postgres/data`, starts PostgreSQL directly with `pg_ctl`, applies all core migrations, creates a restricted runtime role, and seeds two UUID-scoped organizations/outlets used only by integration tests. The cluster listens only on loopback port `54329`. `db:test` connects as that runtime role and verifies:

- readiness and required migrations;
- `NOSUPERUSER` and `NOBYPASSRLS` privileges;
- atomic inbox and domain-event acceptance;
- exact duplicate and conflicting-operation behavior;
- durable rejection without a domain event;
- rollback for an unknown outlet;
- one accepted effect under eight concurrent deliveries;
- row-level isolation between two tenants;
- denial of domain-event mutation;
- duplicate recognition after closing and reopening the connection pool;
- atomic hierarchy, order-line, ticket-line, and audit persistence;
- resource duplicate handling and cross-tenant read denial; and
- order reconstruction after closing and reopening the resource pool;
- exact mutation status, value, and header replay after reopening the idempotency pool;
- optimistic order/ticket transitions with stale-version rejection;
- keyset traversal without duplicate rows; and
- causal gap retry, stale-event conflict, adversarial reordering, and clock-skew independence.

Run core against PostgreSQL:

```sh
npm run core:postgres
```

The runtime URL defaults to:

```text
postgres://feastcloud_runtime:feastcloud_dev_runtime@127.0.0.1:54329/feastcloud?sslmode=disable
```

These credentials are public development fixtures. Never use them in a shared or production environment. Production roles and credentials must be provisioned separately through a secret manager.

To connect an outlet edge to the seeded development scope, use:

```sh
FEASTCLOUD_TENANT_ID=11111111-1111-4111-8111-111111111111 \
FEASTCLOUD_OUTLET_ID=33333333-3333-4333-8333-333333333333 \
npm run edge:dev
```

Use a clean browser profile when changing a paired outlet identity; existing offline records deliberately refuse cross-outlet reassignment.

## Lifecycle

```sh
npm run db:status
npm run db:logs
npm run db:down
```

`db:down` performs a fast, orderly PostgreSQL shutdown and retains `.feastcloud/postgres/data`. The repository intentionally provides no reset command because deleting the cluster is destructive. If disposable development data must be removed, stop PostgreSQL first and remove only the explicit `.feastcloud/postgres` directory after confirming that it contains no required records.

The database binds only to `127.0.0.1`. Core readiness fails closed when the configured database is unreachable or a required resource, audit, sync, or event migration is absent. Database unavailability returns a non-terminal sync retry and never causes the outlet edge to drop pending work.

## CI container profile

GitHub Actions uses `postgres:17.10-alpine3.22` as an isolated CI service and applies the same SQL migrations and restricted-role bootstrap. `compose.dev.yaml` remains available as an optional alternative for contributors who already use containers, but no local FeastCloud command depends on it. The native and CI paths run the same Go integration test through `FEASTCLOUD_TEST_DATABASE_URL`.
