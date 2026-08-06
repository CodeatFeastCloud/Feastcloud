# Architecture decision records

ADRs are immutable decision history. Supersede an accepted ADR with a new numbered ADR; do not rewrite its decision to match later implementation.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-go-modular-monolith.md) | Start the cloud core as a Go modular monolith | Accepted |
| [0002](0002-react-pwa-client.md) | Use a shared React/TypeScript installable PWA | Accepted |
| [0003](0003-outlet-edge-offline-authority.md) | Make a Go outlet edge the live-shift offline authority | Accepted |
| [0004](0004-selective-append-only-ledgers.md) | Use append-only ledgers selectively, not global event sourcing | Accepted |
| [0005](0005-postgresql-rls-multitenancy.md) | Use shared PostgreSQL tenancy with mandatory RLS | Accepted |
| [0006](0006-open-source-license-boundaries.md) | Separate AGPL core from Apache-licensed ecosystem contracts | Proposed pending legal ratification |

New ADRs use the next four-digit number and contain: status, context, decision, consequences, alternatives, and verification. Changes that weaken offline durability, tenant isolation, ledger immutability, open-source independence, or deterministic safety require architecture and security approval.

