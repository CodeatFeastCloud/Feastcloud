# FeastCloud Food & Kitchen OS

FeastCloud is an open-source, offline-first operating system for cloud kitchens, restaurants, chains, central kitchens, and hotel food-and-beverage teams.

This repository currently implements the Phase 0 foundation and the first executable vertical slice: create a multi-brand order, route its items to kitchen stations, move tickets through the KDS, preserve actions during a network interruption, and operate the interface in English, Hindi, or Bengali.

## Repository map

- `apps/web` — installable React/TypeScript operations PWA
- `services/core` — Go modular-monolith API and domain core
- `services/edge` — durable SQLite outlet authority, KDS routing, and cloud outbox
- `scripts/postgres-native.sh` and `deploy/postgres` — native self-hosted PostgreSQL sync profile and restricted development role
- `packages/contracts` — Apache-2.0 public schemas and interoperability contracts
- `docs` — architecture, security, domain, roadmap, and open-source governance

## Quick start

Prerequisites: Node.js 22+ and Go 1.24+.

Install the web dependencies once:

```sh
npm install
```

Start the three local processes in separate terminals:

```sh
npm run core:dev
```

```sh
npm run edge:dev
```

```sh
npm run dev:connected
```

Open the URL printed by Vite. New orders are committed to the local SQLite edge, routed to station tickets, and sent idempotently to core. Stop the core service to exercise WAN-offline behavior; the edge continues accepting orders and preserves its durable outbox. The default infrastructure-free core returns `RETRY` after reconnection. Configure `FEASTCLOUD_DATABASE_URL` and apply all three core migrations to enable durable resources, atomic inbox/domain-event commits, and terminal acknowledgements. Stop the edge as well to exercise browser-offline capture; the PWA creates stable local station tickets and queues their ordered operations, but they become outlet-authoritative only after the edge returns.

`npm run dev` starts a standalone UI demonstration whose local adapter acknowledges its own browser outbox. Use `dev:connected` for the real edge path.

### Durable native PostgreSQL sync profile

Docker is not required. On macOS with Homebrew:

```sh
npm run db:install
npm run db:up
npm run db:test
FEASTCLOUD_TEST_DATABASE_URL='postgres://feastcloud_runtime:feastcloud_dev_runtime@127.0.0.1:54329/feastcloud?sslmode=disable' npm run smoke:postgres
npm run core:postgres
```

This creates a workspace-local cluster under `.feastcloud/postgres`, applies all core migrations, and runs the durable resource and sync integration suites through a restricted `NOSUPERUSER`/`NOBYPASSRLS` login. The PostgreSQL smoke drives an order and KDS transition through a real outlet edge and core, verifies terminal synchronization and local idempotent replay, then restarts the edge and verifies persistence. It does not use `brew services` or create a global database service. PostgreSQL binds only to `127.0.0.1:54329`; development credentials are intentionally public and must never be reused outside this local profile. See [`docs/architecture/postgresql-development.md`](docs/architecture/postgresql-development.md) for the seeded UUID outlet, lifecycle, and safety notes.

## Verify

```sh
make check
```

This validates public contracts and dependency licenses, type-checks and tests the PWA, creates a production build, tests and vets both Go services, and runs a real connected smoke test. The infrastructure-free smoke profile commits an order while core is unavailable, routes two station tickets, verifies replay safety, reconnects to the safe non-terminal core profile without dropping the edge outbox, advances the KDS, restarts the edge, and confirms the projection survived.

Before a release, run `make release-check`. It additionally queries npm and the Go vulnerability database and emits CycloneDX SBOMs for the web graph and both Go modules under `dist/sbom/`. CI retains the same reports as build artifacts.

## What is implemented now

This is the executable Phase 0 foundation, not a claim that the complete four-year product map is already finished. It includes the canonical domain and sync contracts, a PostgreSQL-backed organization/outlet/kitchen resource API, forced-RLS transactions, atomic audit/resource writes, an atomic sync-inbox/domain-event adapter, durable outlet edge, order-to-station KDS slice, complete starter UI packs for English, Hindi, and Bengali, idempotency boundaries, public extension manifests, architecture decisions, threat model, and design-partner measurement kit. Human operational certification of those language packs remains an external pilot gate.

The phase-by-phase implementation gates live in [`docs/roadmap/phase-plan.md`](docs/roadmap/phase-plan.md). Production promotion still requires the external exit gates there: design-partner studies and agreements, live 60-day operation, measured reconciliation and waste baselines, deployment security hardening, and independent tenant-isolation testing.

### Current production gates

- Add load/soak tests, durable idempotency crash-window recovery, transactionally consistent projection feeds, and manager-authorized reconciliation workflows through the non-owner pooled runtime role.
- Add OIDC/OPA authorization, enrolled edge identities, mTLS cloud sync, and a short-lived browser-to-edge pairing/session flow. Demo identity headers and unauthenticated loopback discovery are development-only.
- Exercise station-ticket projection and ticket-specific transitions under concurrent devices, long shifts, and projection pagination; retain **All stations** as an explicit whole-order control with stricter manager policy before production.
- Add encrypted edge storage/key management, signed cloud snapshots and pull sync, backup/restore drills, printer/device adapters, observability, 72-hour chaos/soak tests, and measured latency tests.
- Replace bounded multi-status polling with a transactionally consistent, cursor-paginated projection/change feed, and add an authorized workflow that resolves or retries reconciliation-blocked causal streams.
- Complete native-speaker operational certification and real kitchen-noise speech evaluation; built-in UI translation completeness is not certification.
- Obtain specialist AGPL/MPL and trademark review before the first public distribution.

## Licensing

The product applications and services are licensed under `AGPL-3.0-only`. Public contracts and SDK-facing schemas under `packages/contracts` are licensed under Apache-2.0. Dependencies and AI artifacts must satisfy the policy in [`docs/governance/open-source-policy.md`](docs/governance/open-source-policy.md).

Copyright 2026 FeastCloud contributors.
