# FeastCloud engineering documentation

Status: Phase 0 baseline  
Last updated: 2026-08-03

This directory is the implementation source of truth for FeastCloud's product architecture, public contracts, security baseline, and open-source governance. The documents describe the target system and the gates that must be met before Phase 0 is complete.

## Document map

- [System architecture](architecture/system-architecture.md) — runtime topology, module boundaries, data ownership, tenancy, reliability, and deployment profiles.
- [PostgreSQL development profile](architecture/postgresql-development.md) — self-hosted database startup, restricted runtime role, integration proof, and lifecycle.
- [Complete product blueprint](product/product-blueprint.md) — product boundaries, moat, personalization, finished capability map, acceptance criteria, and commercial contract.
- [Canonical domain contracts](domain/canonical-model.md) — organization hierarchy, shared value objects, entity families, ledger invariants, and event envelopes.
- [API and offline synchronization](contracts/api-and-sync.md) — REST, idempotency, webhooks, edge synchronization, conflict resolution, and compatibility rules.
- [Open-source policy](governance/open-source-policy.md) — repository licensing, dependency and model rules, release evidence, and exception handling.
- [Threat model](security/threat-model.md) — assets, trust boundaries, principal threats, required controls, and verification gates.
- [Delivery roadmap](roadmap/phase-plan.md) — Phase 0 work packages and the gated product roadmap through global rollout.
- [Design-partner kit](pilot/README.md) — workflow observation, baseline metrics, and live-data readiness.
- [Architecture decision records](adr/README.md) — accepted architectural decisions and their consequences.

## Normative language

The words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** are normative. A deviation from a MUST requires an accepted ADR. A deviation from a SHOULD requires a documented engineering decision in the relevant pull request.

## Phase 0 completion

Phase 0 is complete only when all of the following are demonstrated:

- A locally created order continues through the outlet edge to a KDS while the WAN is unavailable. Core has an atomic PostgreSQL inbox/domain-event adapter; Phase 0 completion still requires production-like restart, duplication, reordering, clock-skew, and non-owner RLS proof.
- Organization, outlet, user, and device authorization is enforced, including PostgreSQL row-level security tests that attempt cross-tenant access.
- Canonical REST/OpenAPI and edge/Protobuf contracts implement the rules in this documentation.
- The dependency, image, model, SBOM, vulnerability, and license gates pass with no unapproved artifacts.
- Architecture and threat-model reviews are recorded, and pilot baseline measurement and data-handling agreements are approved.
