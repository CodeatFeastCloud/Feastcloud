# ADR 0004: Selective append-only ledgers

- Status: Accepted
- Date: 2026-08-03

## Context

Inventory, cash, tender, and financial totals must be reproducible and auditable. Editing balances destroys evidence, while event-sourcing every configuration and profile would add complexity without equal benefit.

## Decision

Represent inventory, cash, tender, and financial movements as append-only ledger events. Runtime roles cannot update/delete them. Corrections append a compensating reversal and optional replacement. Balances and costing are rebuildable projections with checkpoints and reconciliation.

Orders and kitchen tickets keep immutable transition histories plus current-state projections. Catalog and organization configuration use effective-dated records and optimistic concurrency. Therefore FeastCloud is explicitly not globally event-sourced.

## Consequences

- Historical balances and variance can be reproduced and audited.
- Correction UX must explain reversals instead of pretending the original action never happened.
- Projection versioning, rebuild tooling, and invariant tests are mandatory.
- Some personal data must be stored by reference or crypto-erased to reconcile privacy deletion with retained financial facts.

## Alternatives rejected

- Mutable quantity/balance columns as truth: cannot reliably explain historical variance.
- Global event sourcing: unnecessary replay/schema burden for configuration and preferences.
- Database audit logs alone: infrastructure evidence is not a domain ledger.

## Verification

Rebuild projections from ledger events and compare them to live balances in CI and scheduled production checks. Privilege tests prove runtime identities cannot mutate history. Duplicate and reversal property tests preserve conservation invariants.

