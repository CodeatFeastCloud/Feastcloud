# ADR 0001: Go modular monolith for the cloud core

- Status: Accepted
- Date: 2026-08-03

## Context

FeastCloud spans many restaurant domains but begins with one team. Premature services would add deployment, transaction, observability, and failure-mode cost before domain boundaries and scale are proven. The system still needs boundaries that can later be extracted.

## Decision

Build one Go cloud deployable with independently testable domain modules. Each module owns its PostgreSQL schema and migrations, exposes Go interfaces and domain events, and cannot directly access another module's tables. Synchronous in-process calls handle immediate consistency; a transactional outbox and durable inbox handle asynchronous effects. External infrastructure is hidden behind narrow ports.

An extraction requires a new ADR backed by at least one of:

- Measured, independently scaling load that cannot be addressed within the monolith.
- A materially different security or data-residency boundary.
- Required failure isolation with a demonstrated blast-radius problem.
- A genuinely independent release/lifecycle owned by a capable team.

## Consequences

- Local development, transactions, testing, and deployment remain simpler.
- Package and migration-boundary checks are mandatory; otherwise the monolith can become coupled.
- A process failure initially affects all cloud modules, so edge autonomy and graceful adapter degradation are essential.
- Go domain packages cannot import NATS, Temporal, model-server, or vendor SDK clients directly.

## Alternatives rejected

- Microservices from the start: operational cost and distributed consistency exceed Phase 0 benefit.
- A generic ERP fork: its domain and extension boundaries would dictate FeastCloud's kitchen model.
- A single unstructured package/database schema: fast initially but prevents ownership and safe extraction.

## Verification

CI enforces package dependency direction and migration ownership. Architecture tests fail on cross-module SQL/table references. The Phase 0 vertical slice runs in one core process without bypassing module APIs.

