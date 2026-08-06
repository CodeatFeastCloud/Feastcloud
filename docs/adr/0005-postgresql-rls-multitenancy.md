# ADR 0005: PostgreSQL RLS-backed multitenancy

- Status: Accepted
- Date: 2026-08-03

## Context

Small customers require economical shared hosting, while chains require strong isolation and scoped administration. Application filters alone are too easy to omit. A database per small tenant would make migrations, pooling, and operations unnecessarily expensive.

## Decision

Use a shared PostgreSQL database initially. Every tenant-owned table has mandatory `tenant_id` and row-level security. Authentication derives tenant/actor context, and every application transaction installs it with `SET LOCAL`. Runtime roles cannot own tables, use `BYPASSRLS`, disable protections, or run unscoped repair work.

Application policy separately enforces organization-node and outlet capability grants. Background jobs iterate explicit tenant scopes. Storage interfaces hide physical layout so regulated enterprise deployments may later use dedicated databases without changing domain callers.

## Consequences

- Shared operation remains economical with defense in depth against missed filters.
- RLS policy, transaction context, query plans, uniqueness constraints, caches, messages, objects, analytics, and exports all require tenant-aware testing.
- Privileged migrations and repairs need separate, audited, time-bound identities.
- RLS does not replace outlet/role authorization or safe aggregate reporting.

## Alternatives rejected

- Application filters only: insufficient final isolation boundary.
- Schema per tenant: migration and connection complexity grows rapidly.
- Database per tenant from day one: avoidable operational cost for the initial market.

## Verification

CI creates multiple tenants and attempts cross-tenant reads/writes through every repository and a direct runtime SQL role. Stress tests interleave tenants on reused pooled connections. Security tests cover cache keys, object paths, events, exports, and analytics in addition to PostgreSQL.

