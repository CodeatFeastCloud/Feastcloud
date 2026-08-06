# ADR 0002: React/TypeScript installable PWA

- Status: Accepted
- Date: 2026-08-03

## Context

POS, KDS, manager, and guest workflows need rapid cross-device delivery, touch support, accessibility, language packs, and operation on commodity Android hardware. Native platform forks would multiply UI and domain behavior before hardware requirements stabilize.

## Decision

Use React and TypeScript with one shared design system and installable PWA application shell. Role and outlet feature profiles expose task-focused surfaces from the same contracts. Service workers cache versioned, signed static assets. Live outlet commands use the local edge API; browser storage may hold drafts and display caches but is not authoritative business storage.

All user-facing text uses ICU keys and BCP 47 locales. The layout supports RTL and WCAG 2.2 AA from foundation work. Capacitor may package the same application for Android-specific integrations; native wrappers do not fork domain logic.

## Consequences

- One client stack supports fast iteration and language/accessibility consistency.
- Browser platform limits require an edge device gateway for printing, scales, cash drawers, and durable operations.
- The UI cannot report a critical order/tender/inventory command as committed until the edge durably acknowledges it.
- Aggressive service-worker update, cache-integrity, and stale-client compatibility tests are required.

## Alternatives rejected

- Separate native Android and web applications: excessive early duplication.
- Electron for all outlet devices: heavier deployment and poor commodity-tablet fit.
- Browser-only cloud storage: cannot meet local latency or 72-hour WAN outage requirements.

## Verification

Run task and accessibility tests for each role at supported form factors, language fallback and RTL tests, stale-shell API compatibility tests, and offline order-to-KDS tests using the installed PWA.

