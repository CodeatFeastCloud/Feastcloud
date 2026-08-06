# System architecture

Status: Accepted Phase 0 baseline

## Goals and constraints

FeastCloud is an offline-capable, multi-tenant Food Operating System. Live order, KDS, printing, inventory, and food-safety work must continue for at least 72 hours without a WAN connection. The same product must support a single outlet and centrally governed chains without creating separate product forks.

The core must be self-hostable using open-source software. AI is advisory and may be unavailable without blocking deterministic operations.

## Runtime topology

```text
POS / KDS / manager PWA / guest web
              |
        outlet HTTPS/LAN
              |
  Go edge agent + SQLite operation log
  local read models + device/print gateway
              |
       mTLS sync over WAN
              |
  Go cloud modular monolith ---- PostgreSQL
        |       |       |
   event bus  objects  Valkey
        |
  analytics / workflows / AI (non-critical path)
```

### Client applications

- React and TypeScript provide a shared design system and installable PWA for POS, KDS, outlet operations, and management.
- All user-facing strings MUST use ICU message keys. Content, formats, and terminology come from independently versioned language and country packs.
- The PWA MUST cache the signed application shell and use the edge API for live outlet work. It MUST show cloud, edge, and synchronization state separately.
- Capacitor packaging MAY provide Android device integration after the browser workflows are stable. Domain logic MUST remain in shared packages, not in native wrappers.
- Guest ordering may use the cloud directly; an outlet-local fallback is optional and must not expose staff APIs.

### Outlet edge

The Go edge agent is the outlet authority for an active shift. It owns:

- Local order acceptance, KDS routing, ticket transitions, printer/device dispatch, and a read cache of active catalog and policy versions.
- A durable SQLite operation log written before acknowledging a local mutation.
- A local projection database that can be rebuilt from the operation log plus the last signed cloud snapshot.
- Cloud synchronization, retry, deduplication, health reporting, and reconciliation queues.

The edge MUST have a stable device identity and cloud-issued certificate. Cloud-to-edge traffic is never required to complete a live local order. Edge databases MUST be encrypted at rest with keys held outside the database file. Payment card data MUST NOT enter the edge log.

### Cloud core

The cloud is a Go modular monolith. It is one deployable process initially, with independently testable domain modules:

| Module | Owns |
|---|---|
| `organization` | Tenant hierarchy, outlets, brands, kitchens, stations, storage locations, feature profiles |
| `identity` | Users, roles, grants, device registrations, sessions, preferences |
| `catalog` | Ingredients, units, allergens, recipes, menus, modifiers, pricing versions |
| `commerce` | Orders, fulfillment, tenders, refunds, taxes, settlements |
| `kitchen` | Tickets, routing, station tasks, preparation batches, quality events |
| `inventory` | Inventory ledger, counts, waste, lots, transfers, cost projections |
| `procurement` | Suppliers, quotations, purchase orders, receipts, invoices |
| `workforce` | Skills, schedules, shifts, attendance, training |
| `guest` | Consent, preferences, reservations, loyalty |
| `finance` | Cash ledger, reconciliation, budgets, operational reporting exports |
| `intelligence` | Forecasts, recommendations, evidence, approvals, outcomes, model registry |
| `integration` | Imports, connectors, webhooks, sync inbox/outbox, partner health |
| `audit` | Security and administrative audit records |

Each module MUST own its PostgreSQL schema and migrations. Modules MUST NOT read or write another module's tables directly. They collaborate through exported Go interfaces, immutable value objects, and domain events. A module extraction is justified only by measured scaling, failure-isolation, security, or independent-release needs and requires an ADR.

NATS JetStream is accessed through an internal event-bus interface; domain packages do not import NATS clients. Durable business truth remains in PostgreSQL. Valkey is cache-only and losing it MUST NOT lose business data. Temporal, ClickHouse, object storage, and AI services are adapters that can be disabled in the single-outlet deployment profile.

## State and event model

FeastCloud is not globally event-sourced.

- Inventory, cash, tender, and financial movements are append-only ledgers. A correction is a compensating event referencing the original event.
- Orders and kitchen tickets retain immutable transition histories while maintaining transactional current-state projections.
- Catalog, policy, and organization configuration use effective-dated versions with optimistic concurrency.
- Every committed domain event is written to a PostgreSQL outbox in the same transaction as its source state. Consumers use durable inbox deduplication.
- Projections are disposable; ledgers and source records are not.

## Tenant and authorization boundary

- The initial SaaS profile uses a shared PostgreSQL cluster and database, with a mandatory `tenant_id` on every tenant-owned row and PostgreSQL row-level security (RLS).
- The authenticated tenant is derived from the session or device certificate, never trusted from a request body. Each transaction sets tenant and actor context with `SET LOCAL`; pooled connections MUST reset context on transaction end.
- Outlet and organization-scope permissions are enforced by both policy evaluation and query scope. RLS is the final tenant boundary, not the complete authorization system.
- Runtime application roles MUST NOT have `BYPASSRLS`, table-owner privileges, or permission to disable triggers. Migration and audited repair identities are separate and unavailable to application processes.
- Background work is tenant-scoped. Cross-tenant analytics receives only explicit, opt-in, de-identified aggregates.
- Enterprise dedicated databases MAY be introduced behind the same repository interfaces; callers cannot depend on physical tenancy layout.

## Authority and consistency

| Data | Authority while online | Authority while disconnected | Merge rule |
|---|---|---|---|
| Organization policies, menu/recipe versions, tax configuration | Cloud | Last signed edge snapshot | Cloud version wins for future work; active tickets retain captured version |
| Active orders and kitchen tickets | Outlet edge | Outlet edge | Append transitions; reject invalid state transitions, never last-write-wins |
| Inventory movements | Originating edge/cloud service | Originating edge | Append and deduplicate; disputed counts create reconciliation work |
| Cash/tender movements | Originating POS/adapter | Originating edge | Append and reconcile; no overwrite |
| User preferences | Cloud | Cached edge copy | Optimistic concurrency; user resolves material conflict |

All timestamps are stored in UTC with the originating IANA timezone retained where operational meaning depends on local time. Ordering and deduplication MUST NOT rely on wall-clock accuracy.

## Deployment profiles

- **Developer:** PWA, core, edge, PostgreSQL, and open-source identity provider via containers; optional adapters disabled.
- **Single outlet:** Docker Compose or K3s, with edge and core potentially on the same host but communicating through production interfaces.
- **Managed cloud:** Kubernetes/Helm, multiple stateless core replicas, managed-by-FeastCloud open-source data services, per-outlet edge agents.
- **Enterprise self-hosted:** Same OCI images and Helm charts; no mandatory callback to FeastCloud-operated services.

## Operational requirements

- P95 local POS command response: under 200 ms. Outlet-network order-to-KDS delivery: under one second.
- Managed cloud target: 99.95% availability, excluding continued local operation during WAN failure.
- OpenTelemetry trace, metric, and structured-log context MUST include correlation ID, deployment, module, and tenant-safe identifiers. Logs MUST NOT contain secrets, card data, voice recordings, or unrestricted personal data.
- Health endpoints distinguish liveness, readiness, dependency degradation, edge connectivity, sync backlog, and last successful reconciliation.
- Backups are encrypted and restoration is tested. Recovery objectives are set per deployment profile before production, not inferred from backup existence.

## AI boundary

Rules, permissions, tax, allergen controls, food-safety thresholds, quantities, and ledger arithmetic remain deterministic. AI services consume versioned, minimized projections and return a recommendation with inputs, model version, confidence, explanation, expiry, and required approval policy. An AI failure never blocks commerce or kitchen execution.

