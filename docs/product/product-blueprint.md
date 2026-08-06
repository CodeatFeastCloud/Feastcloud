# FeastCloud complete product blueprint

Status: Target product contract; implementation is gated by the delivery roadmap  
Scope: Food operations for cloud kitchens, restaurants, chains, central kitchens, franchises, and hotel F&B

## Product promise and boundary

FeastCloud is one progressively disclosed Food and Kitchen Operating System. A single outlet receives a simple workflow template; a chain or hotel activates deeper hierarchy, approvals, controls, and integrations on the same underlying contracts. Native POS and ordering are part of the finished system, while kitchen execution, food cost, operational reliability, and learning remain its center.

Hotel rooms, front desk, general housekeeping, banking, merchant acquiring, and full general-ledger accounting are outside the product. Hotel F&B posts to an existing PMS; financial operations export to accounting and payment systems through replaceable open-source adapters.

## Defensible moat

- **Kitchen Graph:** versioned relationships among ingredients, suppliers, recipes, yields, allergens, equipment, processes, menu items, brands, prices, stations, and demand.
- **Operational digital twin:** an outlet model continuously reconciled from stock, capacity, staff skills, preparation time, demand, waste, equipment, and vendor outcomes.
- **Personal operating system:** role-, language-, outlet-, skill-, accessibility-, and device-specific workflows for operators and consented guest preferences.
- **Multilingual operations:** independently installable language packs, with certification stated separately for UI, operational content, speech input, and speech output.
- **Offline authority:** local POS, KDS, printing, stock movement, and shift work continue without WAN connectivity and reconcile through immutable operations.
- **Open ecosystem:** public pack, connector, hardware, workflow, and permission-scoped WebAssembly plugin contracts create portable community distribution.
- **Measured learning loop:** every recommendation records evidence, approval, decision, model version, and outcome so tenant-specific operating knowledge compounds.
- **Trust and portability:** deterministic safety rules, explainable assistance, human approvals, audit trails, self-hosting, open code, and complete tenant export.

The moat is accumulated operational context and measured customer outcomes, not source-code secrecy or forced payment lock-in.

## Personalization contract

- A person chooses language, text size, accessibility settings, station, shortcuts, alerts, and training level.
- An outlet controls terminology, order flow, recipes, stations, approvals, dashboards, and standard operating procedures.
- An organization centrally governs policies and menus while granting explicit, auditable local variation.
- A guest may opt into preferences, dietary requirements, allergens, loyalty, and communication choices; every use remains purpose- and consent-bound.
- Tenant data is isolated. Cross-customer benchmarks are opt-in, de-identified, aggregated, and subject to re-identification risk review.
- Allergy, food-safety, fiscal, payment, and employment decisions use deterministic policy. Generative AI cannot silently override them.

## Finished-product capability map

| Domain | Target capabilities |
|---|---|
| Organization and platform | Organizations, legal entities, properties/clusters, outlets, brands, revenue centres, kitchens, stations, storage, franchises, workflows, approvals, feature profiles, audit, documents, imports, APIs, webhooks, plugin registry, and country/language packs |
| POS and commerce | Dine-in, takeaway, delivery, QSR, café, bar, drive-through, tables/seats/courses/tabs, split bills, tips, service charges, discount/refund controls, cash shifts, fiscal documents, QR, kiosk, web/call-centre ordering, catering, subscriptions, gift cards, and payment-terminal adapters |
| Omnichannel ordering | Unified intake, channel menu and availability synchronization, throttling, acceptance, dispatch, driver assignment, reconciliation, degraded state, retry, and manual recovery |
| Kitchen execution | Station KDS, routing, fire/hold, coursing, timers, prioritization, expo, assembly, packing, quality checks, capacity, late alerts, instructions, and traceability |
| Recipes and production | Versioned recipes/sub-recipes, units/conversions, yield, portions, substitutes, allergens, nutrition, SOPs, batch production, thawing/fermentation, shelf life, central production, production orders, and yield variance |
| Inventory and procurement | Perpetual event ledger, counts, lots, expiry/FEFO, waste/spoilage/staff meals, transfers, pars/forecasting, suppliers, RFQ/PO, receiving, invoice OCR, price and landed cost, scorecards, and supplier portal |
| Food safety and assets | Temperature logs, cleaning/opening/closing checks, HACCP-style controls, incidents, recall traceability, equipment, maintenance, calibration, and downtime |
| Workforce | Onboarding, roles, skills/certification, attendance, scheduling, leave/swaps, tasks, multilingual training/SOPs, tips/incentives, labor forecasts, payroll inputs, and certified country calculations |
| Guest and growth | Consent CRM, history/preferences, loyalty/membership, promotions/coupons, feedback, reservations/waitlists, campaigns/segments, reputation workflows, and self-service |
| Finance and compliance | Cash/tender reconciliation, marketplace settlements, payment matching, AP workflow, tax/fiscal records, budgets, operational P&L, contribution margin, royalties, and accounting exports |
| Chains and franchises | Central menus and contracts, controlled overrides, transfer pricing, outlet/regional scorecards and permissions, compliance, consolidated reports, benchmarking, approvals, and multi-currency consolidation |
| Hotel F&B | Restaurants, bars, room service, minibars, banquets/event orders, meal plans, staff dining, central stores, guest preference handoff, and safe room-folio posting through PMS adapters |
| Analytics and AI | Live dashboards, forecasts and plans, purchase/stockout/waste/theft/labor/menu/supplier/equipment recommendations, conversational analytics, experimentation, and guarded automation |

## Intelligence progression

1. **Observe:** report sales, waste, delays, cost, and anomalies from reproducible facts.
2. **Predict:** forecast demand, prep, stockouts, labor, and equipment risk with versioned inputs.
3. **Recommend:** draft purchases, prep, transfers, schedules, and availability changes with evidence and expiry.
4. **Optimize:** compare scenarios against margin, service, waste, capacity, and labor constraints.
5. **Automate safely:** execute only reversible, explicitly permitted actions within value and confidence limits.

Supplier commitments, payments, discipline, allergens, food safety, and material price changes require authorized approval unless the customer installs a legally reviewed deterministic policy. AI unavailability never blocks ordering, payment, KDS, printing, receiving, counting, or safety work.

## Language progression

- Unicode, locale-aware formats, translated business content, and RTL-safe layouts are architectural requirements from the foundation.
- English, Hindi, and Bengali are the first complete UI packs; each still requires native-speaking kitchen validation before operational certification.
- Machine translation may draft content but cannot mark a pack reviewed or certified.
- Packs update independently of application releases and report UI, operations, speech input, and speech output support separately.
- Touch workflows remain available for every voice workflow; speech is evaluated in real kitchen noise before release.

## Product-level acceptance criteria

- POS and KDS remain operational for at least 72 hours without cloud connectivity.
- P95 local POS commands complete under 200 ms and outlet-network order-to-KDS delivery under one second.
- Managed cloud targets 99.95% availability without counting continued edge operation as cloud availability.
- Chaos tests lose no committed order, tender, inventory, cash, or kitchen event.
- Inventory balance and historical food cost are reproducible from immutable ledger events.
- Tenant, role, outlet, and device authorization tests run on every release; PostgreSQL RLS is tested using a non-owner runtime role.
- No application interface has hard-coded user-facing language; management and guest applications target WCAG 2.2 AA.
- Every fiscal pack passes golden tests and professional local review; payment adapters prevent PAN/CVV storage.
- Recommendations are versioned, reproducible, explainable, approval-linked, and measured against outcomes.
- Every distributed artifact is self-hostable, license-approved, vulnerability-scanned, checksummed, and represented in release SBOM/provenance evidence.

## Commercial contract

- The community distribution contains the same operational code and remains usable through self-hosting.
- Revenue comes from managed hosting, implementation, migration, support, training, certified integrations/hardware, backups, and enterprise SLAs.
- Pricing may scale by outlet, order volume, storage, and support, but does not require a percentage of restaurant revenue or one payment processor.
- Public APIs and complete tenant export are permanent product requirements, not enterprise-only escape hatches.

## Delivery and current state

The gated implementation sequence is maintained in [the delivery roadmap](../roadmap/phase-plan.md). The repository currently ships the executable Phase 0 order-to-KDS foundation described in the root README. Unimplemented target capabilities in this document are commitments to product scope, not claims of current availability.
