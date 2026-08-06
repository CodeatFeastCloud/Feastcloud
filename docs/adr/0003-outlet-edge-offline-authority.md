# ADR 0003: Outlet edge as live-shift offline authority

- Status: Accepted
- Date: 2026-08-03

## Context

Restaurant internet is unreliable, but orders, KDS, printing, stock movement, and shift work cannot stop. A PWA cache alone cannot provide durable shared state or hardware access across outlet devices.

## Decision

Deploy a Go edge agent per outlet. During an active shift it accepts local commands, validates the last signed cloud policy/catalog snapshot, appends each operation and its business effect atomically to encrypted SQLite, maintains local projections, routes KDS work, and drives hardware. It synchronizes to cloud with at-least-once delivery and exactly-once business effect through operation IDs, idempotency, and cloud inboxes.

Cloud remains authority for centrally governed configuration and long-term consolidation. Active tickets retain their captured versions. Conflicting financial/count evidence is reconciled, not overwritten.

## Consequences

- WAN failure does not block the outlet, and local latency is predictable.
- Edge lifecycle, enrollment, certificate rotation, encrypted backup/recovery, schema compatibility, and operator reconciliation become first-class product concerns.
- Temporary divergence is expected and visible; not every conflict can be merged automatically.
- An edge compromise has outlet-scoped impact, limited by device credentials and data minimization.

## Alternatives rejected

- Cloud-only execution: violates availability and latency goals.
- Peer-to-peer browser synchronization: weak durability, leadership, security, and hardware support.
- Full cloud clone at every outlet: excessive footprint and synchronization surface.

## Verification

Demonstrate 72 hours without WAN, process restart during commit/sync, duplicated and reordered batches, clock skew, signed snapshot replacement, device revocation, and recovery with no lost committed local action or duplicate cloud effect.

