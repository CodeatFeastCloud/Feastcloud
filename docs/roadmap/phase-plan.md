# Gated product delivery roadmap

Status: Planning baseline  
Assumed kickoff: September 2026

Dates are forecasts. A phase advances only after its exit gate is met in production-like conditions. Incomplete gates are not waived because the next calendar period begins.

## Phase 0 — Product and platform foundation

Target: September–November 2026

### Work package A: product truth and pilot design

- [ ] Observe three design partners representing multi-brand cloud kitchen, QSR, and dine-in workflows.
- [ ] Document station maps, failure/manual processes, menus, recipes/yields, imports, device/network conditions, and role/language needs.
- [ ] Define baseline measurements for waste value, unexplained ingredient variance, reconciliation, order/KDS latency, and operator effort.
- [ ] Sign pilot data-handling, retention, support, and incident agreements.

### Work package B: architecture and contracts

- [ ] Ratify the ADRs in this directory and record any replacement decision before implementation diverges.
- [ ] Establish Go module boundaries, migration ownership, transactional outbox/inbox, and PostgreSQL RLS transaction wrapper.
- [x] Commit canonical OpenAPI 3.1, Protobuf sync, event-schema, and error-code contracts with compatibility checks.
- [x] Define connector, country, language, workflow, and hardware manifests; defer executable third-party plugins until the sandbox is threat-tested.

### Work package C: vertical offline proof

- [ ] Enroll an outlet edge and pair a PWA device.
- [x] Create an order while WAN is unavailable, durably record it in SQLite, route it to station KDS, and complete it.
- [x] Reconnect to the infrastructure-free core, receive an explicit non-terminal retry, and prove the edge retains each operation through duplicate delivery and process restart.
- [x] Wire the PostgreSQL inbox and append-only domain event in one transaction with accepted, duplicate, rejected, conflict, and retry results.
- [x] Prove terminal exactly-once event effect through a non-owner runtime role under duplication, adversarial reordering, process restart, and clock skew. Version gaps retry, stale versions conflict, and device timestamps do not determine causality.
- [ ] Add signed snapshot pull and a 72-hour failure/soak run.
- [ ] Apply a signed cloud menu snapshot without altering active tickets or losing unsynchronized work.

### Work package D: platform safety and operability

- [ ] Implement organization/outlet roles, immutable audit, device identity, encryption, backup/restore, and the Phase 0 threat-model test suite.
- [ ] Add OpenTelemetry, dependency health, edge/sync backlog, correlation, and operator-visible degraded state.
- [x] Enforce dependency/model license policy, lockfiles, vulnerability scanning, and CycloneDX SBOM generation in local release checks and CI.
- [ ] Add secret scanning, signed OCI provenance, and an auditable release-evidence bundle.
- [x] Establish complete English, Hindi, and Bengali UI packs, fallback and placeholder checks, runtime pack installation, and RTL layout direction.
- [ ] Complete native-speaking operational review, kitchen-noise speech evaluation, and certification workflow evidence.

### Phase 0 exit gate

- Architecture and threat-model reviews accepted with no unresolved critical risk.
- Offline order-to-KDS demo survives process/device-network failures and synchronizes without a lost or duplicate committed effect.
- Cross-tenant and cross-outlet authorization tests pass at API and direct-database layers.
- All distributed artifacts have approved licenses, provenance, SBOMs, and no blocking vulnerability.
- Pilot baseline and success-measurement plan are signed by the design partner and product owner.

## Product phases

| Phase | Target | Capabilities added | Exit gate |
|---|---|---|---|
| **1. Kitchen OS Core** | Dec 2026–May 2027 | CSV/manual order intake, multi-brand KDS, recipe graph, prep tasks, inventory ledger, receiving/counts/waste, food-cost variance, dashboards, first three language packs, explainable prep/demand suggestions | One kitchen live for 60 days; at least 95% order reconciliation; no lost committed events; at least 10% waste-value or unexplained-variance improvement |
| **2. Native commerce** | Jun–Nov 2027 | Offline POS, shifts, GST-ready documents, tables/courses/tabs, split/refund, QR/kiosk/web ordering, promotions/loyalty foundation, printer/scanner and payment-terminal adapters | Cloud-kitchen, QSR, and dine-in operation; daily tender reconciliation; 72-hour offline and local performance targets pass |
| **3. Supply chain and multi-outlet** | Dec 2027–Jun 2028 | Supplier/RFQ/PO, OCR, lots/expiry/FEFO, transfers, central production, yield/replenishment, menu/channel sync, marketplace reconciliation, chain control, more certified Indian languages | A 3–10 outlet customer closes a complete stock/purchasing cycle; transfers reconcile; released languages pass native-speaker operational review |
| **4. Workforce, guest, and finance** | Jul 2028–Jan 2029 | Scheduling/attendance/training, food-safety and maintenance, reservations/waitlist/campaigns/loyalty, AP/budgets/P&L/settlements, franchise controls, SSO/audit | A chain runs two accounting periods across commerce, inventory, workforce, and reporting; security and isolation audit passes |
| **5. Hotel F&B and ecosystem** | Feb–Aug 2029 | Room service, minibar, banquets, meal plans, staff dining, folio posting, PMS adapter SDK, plugin registry, hardware certification, non-India country pilots | Hotel runs two outlets plus room service; offline folio charges reconcile; one PMS adapter and one external country pack certified |
| **6. Global platform and guarded autonomy** | Sep 2029–Aug 2030 | RTL/global locales, country packs, marketplace, delivery/fleet, privacy-preserving benchmarks, constrained production/purchasing/labor optimization | Three materially different country packs operate; controlled trials show benefit; every automation is bounded, explainable, overridable, and reversible |

### Phase 1 implementation progress

- [x] Versioned recipe graph, order-time recipe snapshots, recursive theoretical consumption, and ledger-derived food cost.
- [x] Manager receiving, waste, and atomic multi-line physical-count workflows with immutable evidence and costed variances.
- [x] English, Hindi, and Bengali inventory operations UI.
- [x] CSV order intake with file fingerprinting, row validation, immediate KDS routing, and immutable reconciliation results.
- [x] Preparation batches/tasks with station ownership, versioned transitions, actual-yield capture, shelf-life controls, and atomic recipe-to-stock posting.
- [x] Versioned, explainable demand forecasts, preparation suggestions, and recipe-expanded stockout warnings in observe mode.
- [ ] Production-like 60-day pilot and Phase 1 economic/reconciliation exit measurements.

## Cross-phase release rules

- New operational recommendations run in observe/shadow mode before they can suggest changes; suggestion mode is measured before any bounded automation.
- Supplier commitments, payments, allergen/food-safety actions, employee discipline, and material price changes require authorized human approval unless a legally reviewed customer policy explicitly defines a narrower safe action.
- A language is reported independently as UI, operational content, speech input, and speech output. Machine translation is draft content, never certification.
- A country pack requires automated fiscal golden tests plus local professional review.
- Public APIs, data export, and self-hosted operation remain available at every commercial tier.
- Each phase includes migration, rollback, observability, accessibility, threat-model, support-runbook, and license evidence; these are product work, not post-launch cleanup.

## Delivery capacity assumption

Months 0–12 assume approximately ten people spanning food-operations product, architecture, four product/edge engineers, data/AI, design/research, QA/reliability, and implementation. Months 13–24 grow toward eighteen people; months 25–48 grow toward 28–35. With a permanent team of 8–12, reforecast the finished platform to five or more years rather than compressing quality gates.
